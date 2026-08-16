package openai

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

type ChatRequest struct {
	Model               string           `json:"model"`
	Messages            []map[string]any `json:"messages"`
	Stream              bool             `json:"stream"`
	StreamOptions       map[string]any   `json:"stream_options,omitempty"`
	Tools               []any            `json:"tools,omitempty"`
	ToolChoice          any              `json:"tool_choice,omitempty"`
	Temperature         *float64         `json:"temperature,omitempty"`
	TopP                *float64         `json:"top_p,omitempty"`
	MaxTokens           int              `json:"max_tokens,omitempty"`
	MaxCompletionTokens int              `json:"max_completion_tokens,omitempty"`
	Stop                any              `json:"stop,omitempty"`
	ReasoningEffort     string           `json:"reasoning_effort,omitempty"`
}

type ResponsesRequest struct {
	Model           string           `json:"model"`
	Input           json.RawMessage  `json:"input"`
	Instructions    json.RawMessage  `json:"instructions,omitempty"`
	Stream          bool             `json:"stream"`
	Tools           []map[string]any `json:"tools,omitempty"`
	ToolChoice      any              `json:"tool_choice,omitempty"`
	Temperature     *float64         `json:"temperature,omitempty"`
	TopP            *float64         `json:"top_p,omitempty"`
	MaxOutputTokens int              `json:"max_output_tokens,omitempty"`
	Reasoning       map[string]any   `json:"reasoning,omitempty"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
}

func NormalizeChat(req ChatRequest, model qoder.Model) (protocol.Request, error) {
	messages, system, lastUser, imageURLs := normalizeMessagesWithImages(req.Messages)
	reasoningEffort, err := model.NormalizeReasoningEffort(req.ReasoningEffort)
	if err != nil {
		return protocol.Request{}, err
	}
	max := req.MaxCompletionTokens
	if max <= 0 {
		max = req.MaxTokens
	}
	return protocol.Request{
		PublicModel: req.Model, ModelID: model.UpstreamID, ModelConfig: model.Raw,
		ReasoningEffort: reasoningEffort,
		System:          system, Messages: messages, ImageURLs: imageURLs, Tools: req.Tools, MaxTokens: max,
		Temperature: req.Temperature, TopP: req.TopP, Stop: req.Stop, LastUserText: lastUser,
	}, nil
}

func NormalizeResponses(req ResponsesRequest, model qoder.Model) (protocol.Request, error) {
	reasoningEffort, err := model.NormalizeReasoningEffort(asString(req.Reasoning["effort"]))
	if err != nil {
		return protocol.Request{}, err
	}

	var inputString string
	var items []map[string]any
	if len(req.Input) == 0 || string(req.Input) == "null" {
		return protocol.Request{}, fmt.Errorf("input is required")
	}
	if err := json.Unmarshal(req.Input, &inputString); err != nil {
		if err := json.Unmarshal(req.Input, &items); err != nil {
			return protocol.Request{}, fmt.Errorf("unsupported responses input: %w", err)
		}
	}

	var discoveredTools []map[string]any
	for _, item := range items {
		switch asString(item["type"]) {
		case "tool_search_output", "additional_tools":
			discoveredTools = append(discoveredTools, mapsFromAny(item["tools"])...)
		}
	}
	allTools := make([]map[string]any, 0, len(req.Tools)+len(discoveredTools))
	allTools = append(allTools, req.Tools...)
	allTools = append(allTools, discoveredTools...)
	tools, routes := normalizeResponsesTools(allTools)

	var rawMessages []map[string]any
	var systemParts []string
	if inputString != "" || (len(items) == 0 && len(req.Input) > 0 && string(req.Input) == `""`) {
		rawMessages = append(rawMessages, map[string]any{"role": "user", "content": inputString})
	} else {
		for _, item := range items {
			typeName := asString(item["type"])
			role := asString(item["role"])
			switch typeName {
			case "function_call":
				name := asString(item["name"])
				namespace := asString(item["namespace"])
				qoderName := responseToolAlias(routes, "function", namespace, name)
				rawMessages = append(rawMessages, canonicalToolCallMessage(firstString(item, "call_id", "id"), qoderName, item["arguments"]))
				continue
			case "function_call_output":
				rawMessages = append(rawMessages, map[string]any{"role": "tool", "tool_call_id": asString(item["call_id"]), "content": valueText(item["output"])})
				continue
			case "tool_search_call":
				qoderName := responseToolAlias(routes, "tool_search", "", "tool_search")
				rawMessages = append(rawMessages, canonicalToolCallMessage(firstString(item, "call_id", "id"), qoderName, item["arguments"]))
				continue
			case "tool_search_output":
				rawMessages = append(rawMessages, map[string]any{"role": "tool", "tool_call_id": asString(item["call_id"]), "content": valueText(item["tools"])})
				continue
			case "additional_tools":
				continue
			}
			if role == "" && typeName == "message" {
				role = "user"
			}
			if role == "" {
				continue
			}
			rawMessages = append(rawMessages, map[string]any{"role": role, "content": item["content"]})
		}
	}

	if len(req.Instructions) > 0 && string(req.Instructions) != "null" {
		var s string
		if json.Unmarshal(req.Instructions, &s) == nil && s != "" {
			systemParts = append(systemParts, s)
		}
		var arr []any
		if json.Unmarshal(req.Instructions, &arr) == nil {
			if t := contentText(arr); t != "" {
				systemParts = append(systemParts, t)
			}
		}
	}
	messages, system, lastUser, imageURLs := normalizeMessagesWithImages(rawMessages)
	if len(systemParts) > 0 {
		system = strings.Join(append(systemParts, system), "\n\n")
		system = strings.TrimSpace(system)
	}

	return protocol.Request{
		PublicModel: req.Model, ModelID: model.UpstreamID, ModelConfig: model.Raw, ReasoningEffort: reasoningEffort, System: system, Messages: messages, ImageURLs: imageURLs, Tools: tools, ToolRoutes: routes,
		MaxTokens: req.MaxOutputTokens, Temperature: req.Temperature, TopP: req.TopP, LastUserText: lastUser,
	}, nil
}

func canonicalToolCallMessage(callID, name string, arguments any) map[string]any {
	if callID == "" {
		callID = "call_unknown"
	}
	return map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{
		"id": callID, "type": "function", "function": map[string]any{"name": name, "arguments": argumentsText(arguments)},
	}}}
}

func argumentsText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func normalizeResponsesTools(input []map[string]any) ([]any, map[string]protocol.ToolRoute) {
	out := make([]any, 0, len(input))
	routes := make(map[string]protocol.ToolRoute)
	usedAliases := make(map[string]string)
	seenTargets := make(map[string]bool)

	// Reserve tool_search first so a client function coincidentally named
	// tool_search cannot steal the discoverability entry point used by Codex.
	for _, tool := range input {
		if asString(tool["type"]) == "tool_search" {
			appendResponsesTool(&out, routes, usedAliases, seenTargets, tool, "", "")
		}
	}
	for _, tool := range input {
		if asString(tool["type"]) == "tool_search" {
			continue
		}
		appendResponsesTool(&out, routes, usedAliases, seenTargets, tool, "", "")
	}
	return out, routes
}

func appendResponsesTool(out *[]any, routes map[string]protocol.ToolRoute, usedAliases map[string]string, seenTargets map[string]bool, tool map[string]any, namespace, namespaceDescription string) {
	typ := asString(tool["type"])
	switch typ {
	case "namespace":
		ns := strings.TrimSpace(asString(tool["name"]))
		if ns == "" {
			return
		}
		desc := strings.TrimSpace(asString(tool["description"]))
		for _, child := range mapsFromAny(tool["tools"]) {
			appendResponsesTool(out, routes, usedAliases, seenTargets, child, ns, desc)
		}
	case "tool_search":
		key := "tool_search\x00client"
		if seenTargets[key] {
			return
		}
		seenTargets[key] = true
		alias := reserveToolAlias("tool_search", key, usedAliases)
		params := tool["parameters"]
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []any{"query"}, "additionalProperties": false}
		}
		fn := map[string]any{"name": alias, "parameters": params}
		if desc := strings.TrimSpace(asString(tool["description"])); desc != "" {
			fn["description"] = desc
		}
		*out = append(*out, map[string]any{"type": "function", "function": fn})
		routes[alias] = protocol.ToolRoute{Kind: "tool_search", Name: "tool_search"}
	case "function":
		name := strings.TrimSpace(asString(tool["name"]))
		if name == "" {
			return
		}
		key := "function\x00" + namespace + "\x00" + name
		if seenTargets[key] {
			return
		}
		seenTargets[key] = true
		base := name
		if namespace != "" {
			base = namespace + "__" + name
		}
		alias := reserveToolAlias(base, key, usedAliases)
		params := tool["parameters"]
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		fn := map[string]any{"name": alias, "parameters": params}
		desc := strings.TrimSpace(asString(tool["description"]))
		if namespaceDescription != "" {
			if desc == "" {
				desc = namespaceDescription
			} else {
				desc = namespaceDescription + "\n\n" + desc
			}
		}
		if desc != "" {
			fn["description"] = desc
		}
		if strict, ok := tool["strict"].(bool); ok {
			fn["strict"] = strict
		}
		*out = append(*out, map[string]any{"type": "function", "function": fn})
		routes[alias] = protocol.ToolRoute{Kind: "function", Name: name, Namespace: namespace}
	}
}

func responseToolAlias(routes map[string]protocol.ToolRoute, kind, namespace, name string) string {
	for alias, route := range routes {
		if route.Kind == kind && route.Namespace == namespace && route.Name == name {
			return alias
		}
	}
	if namespace != "" {
		return sanitizeToolAlias(namespace + "__" + name)
	}
	return name
}

func reserveToolAlias(base, target string, used map[string]string) string {
	base = sanitizeToolAlias(base)
	if base == "" {
		base = "tool"
	}
	if len(base) > 64 {
		base = shortenToolAlias(base, target)
	}
	if previous, exists := used[base]; !exists || previous == target {
		used[base] = target
		return base
	}
	sum := sha256.Sum256([]byte(target))
	suffix := fmt.Sprintf("__%x", sum[:5])
	maxPrefix := 64 - len(suffix)
	if maxPrefix < 1 {
		maxPrefix = 1
	}
	if len(base) > maxPrefix {
		base = base[:maxPrefix]
	}
	alias := base + suffix
	used[alias] = target
	return alias
}

func shortenToolAlias(base, target string) string {
	sum := sha256.Sum256([]byte(target))
	suffix := fmt.Sprintf("__%x", sum[:5])
	maxPrefix := 64 - len(suffix)
	if len(base) > maxPrefix {
		base = base[:maxPrefix]
	}
	return base + suffix
}

func sanitizeToolAlias(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func mapsFromAny(v any) []map[string]any {
	switch items := v.(type) {
	case []map[string]any:
		return items
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, raw := range items {
			if m, ok := raw.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func normalizeMessages(input []map[string]any) ([]map[string]any, string, string) {
	out, system, lastUser, _ := normalizeMessagesWithImages(input)
	return out, system, lastUser
}

func normalizeMessagesWithImages(input []map[string]any) ([]map[string]any, string, string, []string) {
	out := make([]map[string]any, 0, len(input))
	var systemParts []string
	imageURLs := make([]string, 0)
	lastUser := ""
	for _, raw := range input {
		role := asString(raw["role"])
		if role == "" {
			role = "user"
		}
		text, images := contentTextAndImages(raw["content"])
		if role == "system" || role == "developer" {
			if text != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}
		if role == "user" {
			imageURLs = append(imageURLs, images...)
			if text != "" {
				lastUser = text
			}
		}
		m := make(map[string]any, len(raw)+2)
		for k, v := range raw {
			m[k] = v
		}
		m["role"] = role
		m["content"] = text
		out = append(out, m)
	}
	return out, strings.Join(systemParts, "\n\n"), lastUser, imageURLs
}

func contentText(v any) string {
	text, _ := contentTextAndImages(v)
	return text
}

func contentTextAndImages(v any) (string, []string) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return valueText(v), nil
	}
	var parts []string
	images := make([]string, 0)
	for _, raw := range arr {
		switch p := raw.(type) {
		case string:
			if p != "" {
				parts = append(parts, p)
			}
		case map[string]any:
			if imageURL := imageURLFromPart(p); imageURL != "" {
				images = append(images, imageURL)
				continue
			}
			if s := firstString(p, "text", "content"); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, "\n"), images
}

func imageURLFromPart(part map[string]any) string {
	typ := strings.ToLower(strings.TrimSpace(asString(part["type"])))
	if typ != "image_url" && typ != "input_image" && typ != "image" {
		return ""
	}
	if value := part["image_url"]; value != nil {
		switch image := value.(type) {
		case string:
			return strings.TrimSpace(image)
		case map[string]any:
			return strings.TrimSpace(asString(image["url"]))
		}
	}
	return strings.TrimSpace(asString(part["url"]))
}

func valueText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func asString(v any) string { s, _ := v.(string); return s }
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := asString(m[k]); s != "" {
			return s
		}
	}
	return ""
}
