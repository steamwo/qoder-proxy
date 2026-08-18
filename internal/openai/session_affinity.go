package openai

import "strings"

// responsesPromptCacheSessionKey turns the downstream Responses prompt cache
// key into a stable proxy-side conversation identity. The value is never sent
// to Qoder verbatim: qoder.sessionIDForRequest hashes ClientSessionKey together
// with the upstream model. This lets a restarted proxy reconstruct the same
// Qoder session/cache affinity without persisting provider cache contents.
func responsesPromptCacheSessionKey(promptCacheKey string) string {
	key := strings.TrimSpace(promptCacheKey)
	if key == "" {
		return ""
	}
	return "openai-responses/prompt-cache/" + key
}
