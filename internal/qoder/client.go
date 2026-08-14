package qoder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/protocol"
)

type QueueRetryPolicy struct {
	MaxRetries int
	MaxWait    time.Duration
}

type QueueInfo struct {
	Status            int
	Code              string
	ModelKey          string
	QueueCount        int
	QueueType         string
	RetryAfterSeconds int
	WaitTime          int
	ServiceAvailable  bool
	Message           string
}

type Client struct {
	HTTP          *http.Client
	Cred          credential.Credential
	QueueRetry    QueueRetryPolicy
	UsageObserver func(protocol.Usage)
	queueWait     func(context.Context, time.Duration) error
}

func NewClient(httpClient *http.Client, cred credential.Credential) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		HTTP:       httpClient,
		Cred:       cred,
		QueueRetry: QueueRetryPolicy{MaxRetries: 20, MaxWait: 10 * time.Minute},
		queueWait:  waitContext,
	}
}

func stableHash(parts ...any) string {
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (c *Client) Chat(ctx context.Context, req protocol.Request) (*http.Response, error) {
	return c.ChatWithQueue(ctx, req, nil)
}

// ChatWithQueue preserves one Qoder session and one final effort value across
// queue retries. A trustworthy client session key is reused across turns; when
// no such key is available, the public API request gets an isolated session.
func (c *Client) ChatWithQueue(ctx context.Context, req protocol.Request, onQueue func(QueueInfo) error) (*http.Response, error) {
	policy := c.QueueRetry
	if policy.MaxRetries < 0 {
		policy.MaxRetries = 0
	}
	if policy.MaxWait <= 0 {
		policy.MaxWait = 10 * time.Minute
	}
	waitFn := c.queueWait
	if waitFn == nil {
		waitFn = waitContext
	}
	sessionID, err := sessionIDForRequest(req)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	for attempt := 0; ; attempt++ {
		resp, err := c.doChatAttempt(ctx, req, sessionID, attempt)
		if err != nil {
			return nil, err
		}
		info, queued, err := inspectQueueResponse(resp)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		if !queued {
			return resp, nil
		}
		resp.Body.Close()
		if info.RetryAfterSeconds <= 0 {
			info.RetryAfterSeconds = 30
		}
		slog.Info("qoder queued",
			"model", req.PublicModel,
			"upstream_model", req.ModelID,
			"reasoning_effort", effectiveReasoningLabel(req.ReasoningEffort),
			"attempt", attempt+1,
			"queue_type", info.QueueType,
			"queue_count", info.QueueCount,
			"retry_after_seconds", info.RetryAfterSeconds,
			"wait_time", info.WaitTime,
			"service_available", info.ServiceAvailable,
		)
		if onQueue != nil {
			if err := onQueue(info); err != nil {
				return nil, err
			}
		}
		if attempt >= policy.MaxRetries {
			return nil, fmt.Errorf("Qoder queue retry limit reached after %d retries: %s", policy.MaxRetries, info.Message)
		}
		delay := time.Duration(info.RetryAfterSeconds) * time.Second
		if time.Since(started)+delay > policy.MaxWait {
			return nil, fmt.Errorf("Qoder queue wait limit reached after %s: %s", time.Since(started).Round(time.Second), info.Message)
		}
		if err := waitFn(ctx, delay); err != nil {
			return nil, err
		}
	}
}

func waitContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// doChatAttempt emits the final normalized effort and task identity beside both
// upstream request and response events. attempt is zero-based; attempts after
// the first are explicitly marked is_retry for Qoder.
func (c *Client) doChatAttempt(ctx context.Context, req protocol.Request, sessionID string, attempt int) (*http.Response, error) {
	modelConfig := cloneMap(req.ModelConfig)
	if len(modelConfig) == 0 {
		modelConfig = map[string]any{
			"key":               req.ModelID,
			"display_name":      req.ModelID,
			"source":            "system",
			"is_reasoning":      strings.Contains(req.ModelID, "think") || strings.Contains(req.ModelID, "reason"),
			"is_vl":             false,
			"max_input_tokens":  131072,
			"max_output_tokens": 32768,
		}
	}
	maxOutput := intField(modelConfig, "max_output_tokens")
	if maxOutput <= 0 {
		maxOutput = 32768
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 || maxTokens > maxOutput {
		maxTokens = maxOutput
	}
	tools := req.Tools
	if tools == nil {
		tools = []any{}
	}
	requestSetID := requestSetIDForRequest(req, sessionID)
	chatRecordID := stableHash("qoder-chat-record", sessionID, req.ModelID, req.Messages, tools, maxTokens, req.ReasoningEffort)
	requestID, _ := randomUUID()
	businessID, _ := randomUUID()
	isRetry := attempt > 0
	clientSessionBound := strings.TrimSpace(req.ClientSessionKey) != ""
	parameters := map[string]any{"max_tokens": maxTokens}
	if req.ReasoningEffort != "" {
		parameters["reasoningEffort"] = req.ReasoningEffort
	}
	isReasoning := boolField(modelConfig, "is_reasoning")
	if req.ReasoningEffort == "none" {
		isReasoning = false
	} else if req.ReasoningEffort != "" {
		isReasoning = true
	}
	body := map[string]any{
		"request_id":       requestID,
		"request_set_id":   requestSetID,
		"chat_record_id":   chatRecordID,
		"session_id":       sessionID,
		"stream":           true,
		"chat_task":        "FREE_INPUT",
		"is_reply":         true,
		"is_retry":         isRetry,
		"source":           1,
		"version":          "3",
		"session_type":     "qodercli",
		"agent_id":         DefaultAgent,
		"task_id":          DefaultTask,
		"code_language":    "",
		"chat_prompt":      "",
		"image_urls":       nil,
		"aliyun_user_type": "",
		"system":           req.System,
		"messages":         req.Messages,
		"tools":            tools,
		"parameters":       parameters,
		"chat_context": map[string]any{
			"chatPrompt": "", "imageUrls": nil,
			"extra": map[string]any{
				"context":         []any{},
				"modelConfig":     map[string]any{"key": req.ModelID, "is_reasoning": isReasoning},
				"originalContent": req.LastUserText,
			},
			"features": []any{}, "text": req.LastUserText,
		},
		"model_config": modelConfig,
		"business": map[string]any{
			"product": "cli", "version": ClientVersion, "type": "agent", "stage": "start",
			"id": businessID, "name": truncateRunes(req.LastUserText, 30), "begin_at": time.Now().UnixMilli(),
		},
	}
	// Intentionally do not forward temperature/top_p/stop here. reasoningEffort
	// is the only additional request parameter because Qoder's current CLI/SDK
	// exposes it as a first-class per-request model parameter.
	plainBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	// Qoder's agent endpoint supports an encoded request mode used by Qoder CLI
	// implementations such as 9Router and CLIProxyAPIPlus. Tool schemas can
	// contain shell/code/security-like strings that trigger the upstream WAF;
	// encode the complete body and sign the encoded bytes so tools reach the
	// agent endpoint intact.
	encodedBody := qoderEncodeBody(plainBody)
	url := BaseURL + ChatEncodedPath
	headers, err := BuildHeaders(encodedBody, url, c.Cred)
	if err != nil {
		return nil, err
	}
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Accept-Encoding", "identity")
	headers.Set("X-Model-Key", req.ModelID)
	source := firstString(modelConfig, "source")
	if source == "" {
		source = "system"
	}
	headers.Set("X-Model-Source", source)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encodedBody))
	httpReq.Header = headers
	// net/http otherwise injects User-Agent: Go-http-client/1.1. CFlareAIProxy's
	// Worker fetch does not explicitly send a Qoder client user-agent, so suppress
	// Go's transport fingerprint here for closer protocol parity.
	httpReq.Header["User-Agent"] = []string{""}
	started := time.Now()
	slog.Debug("qoder request",
		"operation", "chat",
		"model", req.PublicModel,
		"upstream_model", req.ModelID,
		"attempt", attempt+1,
		"is_retry", isRetry,
		"client_session_bound", clientSessionBound,
		"session_id", sessionID,
		"request_set_id", requestSetID,
		"chat_record_id", chatRecordID,
		"url", url,
		"body_bytes", len(plainBody),
		"encoded_body_bytes", len(encodedBody),
		"encoded", true,
		"messages", len(req.Messages),
		"roles", messageRoles(req.Messages),
		"last_user_bytes", len(req.LastUserText),
		"tools", len(tools),
		"max_tokens", maxTokens,
		"reasoning_effort", effectiveReasoningLabel(req.ReasoningEffort),
		"supported_reasoning_efforts", reasoningEfforts(modelConfig),
		"reasoning_disabled_supported", reasoningDisabled(modelConfig),
		"model_config_keys", sortedKeys(modelConfig),
		"model_config_key", firstString(modelConfig, "key", "model"),
		"model_source", source,
	)
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		slog.Error("qoder request failed", "operation", "chat", "model", req.PublicModel, "upstream_model", req.ModelID, "attempt", attempt+1, "is_retry", isRetry, "client_session_bound", clientSessionBound, "session_id", sessionID, "request_set_id", requestSetID, "chat_record_id", chatRecordID, "reasoning_effort", effectiveReasoningLabel(req.ReasoningEffort), "duration_ms", time.Since(started).Milliseconds(), "error", err)
		return nil, err
	}
	slog.Info("qoder response",
		"operation", "chat",
		"model", req.PublicModel,
		"upstream_model", req.ModelID,
		"attempt", attempt+1,
		"is_retry", isRetry,
		"client_session_bound", clientSessionBound,
		"session_id", sessionID,
		"request_set_id", requestSetID,
		"chat_record_id", chatRecordID,
		"reasoning_effort", effectiveReasoningLabel(req.ReasoningEffort),
		"status", resp.StatusCode,
		"duration_ms", time.Since(started).Milliseconds(),
		"content_type", resp.Header.Get("Content-Type"),
		"content_length", resp.ContentLength,
		"transfer_encoding", strings.Join(resp.TransferEncoding, ","),
	)
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		message := strings.TrimSpace(string(data))
		slog.Error("qoder upstream error", "operation", "chat", "model", req.PublicModel, "upstream_model", req.ModelID, "attempt", attempt+1, "is_retry", isRetry, "client_session_bound", clientSessionBound, "session_id", sessionID, "request_set_id", requestSetID, "chat_record_id", chatRecordID, "reasoning_effort", effectiveReasoningLabel(req.ReasoningEffort), "status", resp.StatusCode, "body", truncateRunes(message, 1000))
		return nil, fmt.Errorf("qoder chat returned HTTP %d: %s", resp.StatusCode, message)
	}
	if c.UsageObserver != nil {
		resp.Body = observeUsageBody(resp.Body, c.UsageObserver)
	}
	return resp, nil
}

// effectiveReasoningLabel makes the upstream automatic behavior explicit instead of leaving logs blank.
// effectiveReasoningLabel 将上游自动行为明确标为 auto，避免日志留空造成歧义。
func effectiveReasoningLabel(effort string) string {
	if strings.TrimSpace(effort) == "" {
		return "auto"
	}
	return effort
}

func messageRoles(messages []map[string]any) []string {
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		role, _ := message["role"].(string)
		roles = append(roles, role)
	}
	return roles
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
