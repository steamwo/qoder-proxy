package anthropic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

var workingDirectoryPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?im)^[ \t]*(?:working directory|current working directory|cwd|workspace(?: directory)?|project directory)[ \t]*:[ \t]*(.+?)[ \t]*$`),
	regexp.MustCompile(`(?is)<(?:cwd|working_directory|workspace|project_directory)>\s*([^<\r\n]+?)\s*</(?:cwd|working_directory|workspace|project_directory)>`),
	regexp.MustCompile(`(?i)"cwd"\s*:\s*"([^"\r\n]+)"`),
}

// handleMessagesNative is the production Anthropic path. It preserves the
// Anthropic API surface for Claude Code while canonicalizing tool history to
// OpenAI tool_calls/role=tool messages before Qoder's agent endpoint.
func handleMessagesNative(w http.ResponseWriter, r *http.Request, backend Backend) {
	var req MessageRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMessageRequestBytes))
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "messages is required")
		return
	}
	if req.MaxTokens <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "max_tokens must be greater than 0")
		return
	}

	model, err := backend.ResolveModel(r.Context(), req.Model)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found_error", err.Error())
		return
	}
	preq, err := normalizeNativeMessages(req, model)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	if req.Stream {
		streamMessages(w, r, backend, preq, req)
		return
	}

	resp, err := chatWithQueue(r.Context(), backend, preq, nil)
	if err != nil {
		writeBackendError(w, err, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	a, err := collectEvents(resp)
	if err != nil {
		writeError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, messageObject(req, a))
}

func normalizeNativeMessages(req MessageRequest, model qoder.Model) (protocol.Request, error) {
	requestedEffort := stringValue(req.OutputConfig["effort"])
	thinkingType := strings.ToLower(strings.TrimSpace(stringValue(req.Thinking["type"])))
	if thinkingType == "disabled" && requestedEffort != "" {
		return protocol.Request{}, fmt.Errorf("output_config.effort cannot be combined with thinking.type=disabled when proxying to Qoder")
	}
	if requestedEffort == "" && thinkingType == "disabled" {
		requestedEffort = "none"
	}
	reasoningEffort, err := model.NormalizeReasoningEffort(requestedEffort)
	if err != nil {
		return protocol.Request{}, err
	}

	system, err := textContent(req.System, false)
	if err != nil {
		return protocol.Request{}, fmt.Errorf("system: %w", err)
	}
	workingDirectory, workspaceSource := extractWorkingDirectory(system, req.Tools)
	if workingDirectory != "" {
		system = appendWorkspaceConstraint(system, workingDirectory)
	}

	messages := make([]map[string]any, 0, len(req.Messages))
	lastUser := ""
	for i, raw := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(stringValue(raw["role"])))
		if role != "user" && role != "assistant" {
			return protocol.Request{}, fmt.Errorf("messages[%d].role must be user or assistant", i)
		}
		converted, userText, err := convertMessage(role, raw["content"])
		if err != nil {
			return protocol.Request{}, fmt.Errorf("messages[%d]: %w", i, err)
		}
		messages = append(messages, converted...)
		if userText != "" {
			lastUser = userText
		}
	}

	tools := make([]any, 0, len(req.Tools))
	for i, tool := range req.Tools {
		name := stringValue(tool["name"])
		if name == "" {
			return protocol.Request{}, fmt.Errorf("tools[%d].name is required", i)
		}
		schema := tool["input_schema"]
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		fn := map[string]any{"name": name, "parameters": schema}
		if desc := stringValue(tool["description"]); desc != "" {
			fn["description"] = desc
		}
		tools = append(tools, map[string]any{"type": "function", "function": fn})
	}

	var stop any
	if len(req.StopSequences) == 1 {
		stop = req.StopSequences[0]
	} else if len(req.StopSequences) > 1 {
		vals := make([]any, len(req.StopSequences))
		for i, s := range req.StopSequences {
			vals[i] = s
		}
		stop = vals
	}

	slog.Info("anthropic context",
		"model", req.Model,
		"system_bytes", len(system),
		"messages", len(messages),
		"tools", len(tools),
		"workspace_present", workingDirectory != "",
		"workspace_source", workspaceSource,
		"workspace_bytes", len(workingDirectory),
		"openai_tool_history", true,
	)

	return protocol.Request{
		PublicModel:              req.Model,
		ModelID:                  model.UpstreamID,
		ModelConfig:              model.Raw,
		ReasoningEffort:          reasoningEffort,
		System:                   system,
		Messages:                 messages,
		Tools:                    tools,
		MaxTokens:                req.MaxTokens,
		Temperature:              req.Temperature,
		TopP:                     req.TopP,
		Stop:                     stop,
		LastUserText:             lastUser,
		ClaudeToolCompatibility: true,
	}, nil
}

func convertNativeMessage(role string, content any) (map[string]any, string, error) {
	if s, ok := content.(string); ok {
		return map[string]any{"role": role, "content": s}, ternary(role == "user", s, ""), nil
	}
	blocks, ok := content.([]any)
	if !ok {
		return nil, "", fmt.Errorf("content must be a string or content-block array")
	}

	out := make([]any, 0, len(blocks))
	userText := make([]string, 0)
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ := stringValue(block["type"])
		if role == "assistant" {
			switch typ {
			case "text":
				if text := stringValue(block["text"]); text != "" {
					out = append(out, map[string]any{"type": "text", "text": text})
				}
			case "tool_use":
				id := stringValue(block["id"])
				if id == "" {
					id = newID("toolu_")
				}
				name := stringValue(block["name"])
				if name == "" {
					return nil, "", fmt.Errorf("tool_use.name is required")
				}
				input := block["input"]
				if input == nil {
					input = map[string]any{}
				}
				out = append(out, map[string]any{"type": "tool_use", "id": id, "name": name, "input": input})
			case "thinking", "redacted_thinking", "":
				// Do not forward hidden thinking history to another provider.
			default:
				return nil, "", fmt.Errorf("unsupported assistant content block type %q", typ)
			}
			continue
		}

		switch typ {
		case "text":
			if text := stringValue(block["text"]); text != "" {
				out = append(out, map[string]any{"type": "text", "text": text})
				userText = append(userText, text)
			}
		case "tool_result":
			toolID := stringValue(block["tool_use_id"])
			if toolID == "" {
				return nil, "", fmt.Errorf("tool_result.tool_use_id is required")
			}
			result := map[string]any{"type": "tool_result", "tool_use_id": toolID}
			if block["content"] == nil {
				result["content"] = ""
			} else {
				result["content"] = block["content"]
			}
			if isError, ok := block["is_error"].(bool); ok {
				result["is_error"] = isError
			}
			out = append(out, result)
		case "image":
			if source := block["source"]; source != nil {
				out = append(out, map[string]any{"type": "image", "source": source})
			}
		case "":
		default:
			return nil, "", fmt.Errorf("unsupported user content block type %q", typ)
		}
	}
	return map[string]any{"role": role, "content": out}, strings.Join(userText, "\n"), nil
}

func extractWorkingDirectory(system string, tools []map[string]any) (string, string) {
	if cwd := extractWorkingDirectoryFromText(system); cwd != "" {
		return cwd, "system"
	}
	for _, tool := range tools {
		description := stringValue(tool["description"])
		if description == "" {
			continue
		}
		if cwd := extractWorkingDirectoryFromText(description); cwd != "" {
			name := stringValue(tool["name"])
			if name == "" {
				name = "unknown"
			}
			return cwd, "tool:" + name
		}
	}
	return "", ""
}

func extractWorkingDirectoryFromText(text string) string {
	for _, pattern := range workingDirectoryPatterns {
		match := pattern.FindStringSubmatch(text)
		if len(match) < 2 {
			continue
		}
		if cwd := cleanWorkingDirectory(match[1]); cwd != "" {
			return cwd
		}
	}
	return ""
}

func cleanWorkingDirectory(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'`")
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 1024 {
		return ""
	}
	lower := strings.ToLower(value)
	if lower == "unknown" || lower == "none" || lower == "n/a" || lower == "null" {
		return ""
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `\\`) {
		return value
	}
	if len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return value
	}
	return ""
}

func appendWorkspaceConstraint(system, cwd string) string {
	if cwd == "" || strings.Contains(system, "[qoder-proxy workspace]") {
		return system
	}
	block := "[qoder-proxy workspace]\nCurrent working directory: " + cwd + "\nTreat this directory as the active project root for project-relative Read, Edit, Write, Glob, Grep, and Bash operations. Do not inspect parent, home, filesystem-root, or unrelated project directories unless the user explicitly asks or the task clearly requires it."
	if strings.TrimSpace(system) == "" {
		return block
	}
	return system + "\n\n" + block
}
