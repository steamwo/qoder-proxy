package openai

import (
	"log/slog"
	"sort"
	"strings"
	"unicode"
)

const (
	autoToolSearchThreshold  = 96
	autoToolSearchCoreLimit  = 24
	autoToolSearchCoreFloor  = 8
	proxyToolSearchResultMax = 12
)

type responsesFunctionCandidate struct {
	ordinal              int
	core                 bool
	tool                 map[string]any
	namespace            string
	namespaceDescription string
}

// autoVirtualizeResponsesTools projects a very large Responses registry into a
// small model-visible core plus deferred long-tail tools. When the client has a
// native tool_search entry point it is preserved. Otherwise a synthetic search
// entry point is added for the proxy-managed discovery loop in responses.go.
//
// Codex can group hundreds of functions under only a few namespace containers,
// so activation is based on recursively expanded function leaves rather than
// len(initial). The original downstream request is never mutated.
func autoVirtualizeResponsesTools(initial []map[string]any) []map[string]any {
	return virtualizeResponsesTools(initial, true)
}

func virtualizeResponsesTools(initial []map[string]any, emitLog bool) []map[string]any {
	candidates := responsesFunctionCandidates(initial)
	if len(candidates) < autoToolSearchThreshold {
		return initial
	}

	hasToolSearch := responsesToolsHaveSearch(initial)
	selected := selectResponsesCoreFunctions(candidates)

	capacity := len(initial)
	if !hasToolSearch {
		capacity++
	}
	projected := make([]map[string]any, 0, capacity)
	if !hasToolSearch {
		projected = append(projected, syntheticResponsesToolSearch())
	}

	ordinal := 0
	deferred := 0
	for _, tool := range initial {
		clone, n := projectResponsesTool(tool, selected, &ordinal)
		deferred += n
		projected = append(projected, clone)
	}

	if emitLog {
		slog.Info("openai responses tool virtualization",
			"top_level_tools_received", len(initial),
			"function_tools_received", len(candidates),
			"core_tools_visible", len(selected),
			"tools_deferred", deferred,
			"tool_search_present", hasToolSearch,
			"tool_search_proxy_managed", !hasToolSearch,
			"core_tool_names", responsesCandidateNames(candidates, selected, true, autoToolSearchCoreLimit),
			"deferred_tool_names_sample", responsesCandidateNames(candidates, selected, false, 8),
		)
	}
	return projected
}

func responsesNeedsProxyToolSearch(initial []map[string]any) bool {
	return len(responsesFunctionCandidates(initial)) >= autoToolSearchThreshold && !responsesToolsHaveSearch(initial)
}

func responsesFunctionCandidates(tools []map[string]any) []responsesFunctionCandidate {
	out := make([]responsesFunctionCandidate, 0)
	ordinal := 0
	var visit func([]map[string]any, string, string)
	visit = func(items []map[string]any, namespace, namespaceDescription string) {
		for _, tool := range items {
			switch asString(tool["type"]) {
			case "function":
				out = append(out, responsesFunctionCandidate{
					ordinal:              ordinal,
					core:                 responsesCoreTool(tool),
					tool:                 tool,
					namespace:            namespace,
					namespaceDescription: namespaceDescription,
				})
				ordinal++
			case "namespace":
				ns := strings.TrimSpace(asString(tool["name"]))
				if ns == "" {
					ns = namespace
				}
				desc := strings.TrimSpace(asString(tool["description"]))
				if desc == "" {
					desc = namespaceDescription
				}
				visit(mapsFromAny(tool["tools"]), ns, desc)
			}
		}
	}
	visit(tools, "", "")
	return out
}

func selectResponsesCoreFunctions(candidates []responsesFunctionCandidate) map[int]bool {
	selected := make(map[int]bool)
	for _, candidate := range candidates {
		if len(selected) >= autoToolSearchCoreLimit {
			break
		}
		if candidate.core {
			selected[candidate.ordinal] = true
		}
	}
	if len(selected) < autoToolSearchCoreFloor {
		for _, candidate := range candidates {
			if len(selected) >= autoToolSearchCoreFloor {
				break
			}
			selected[candidate.ordinal] = true
		}
	}
	return selected
}

func projectResponsesTool(tool map[string]any, selected map[int]bool, ordinal *int) (map[string]any, int) {
	clone := cloneResponsesTool(tool)
	switch asString(tool["type"]) {
	case "tool_search":
		delete(clone, "defer_loading")
		return clone, 0

	case "namespace":
		delete(clone, "defer_loading")
		children := mapsFromAny(tool["tools"])
		projectedChildren := make([]map[string]any, 0, len(children))
		deferred := 0
		for _, child := range children {
			projected, n := projectResponsesTool(child, selected, ordinal)
			deferred += n
			projectedChildren = append(projectedChildren, projected)
		}
		clone["tools"] = projectedChildren
		return clone, deferred

	case "function":
		current := *ordinal
		(*ordinal)++
		if selected[current] {
			delete(clone, "defer_loading")
			return clone, 0
		}
		clone["defer_loading"] = true
		return clone, 1
	}
	return clone, 0
}

func syntheticResponsesToolSearch() map[string]any {
	return map[string]any{
		"type":      "tool_search",
		"execution": "proxy",
		"description": "Search the proxy-managed deferred tool registry for capabilities that are not currently visible. " +
			"Use this before concluding that a tool is unavailable.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Capability, action, integration, or tool to find."},
			},
			"required":             []any{"query"},
			"additionalProperties": false,
		},
	}
}

func responsesToolsHaveSearch(tools []map[string]any) bool {
	for _, tool := range tools {
		switch asString(tool["type"]) {
		case "tool_search":
			return true
		case "namespace":
			if responsesToolsHaveSearch(mapsFromAny(tool["tools"])) {
				return true
			}
		}
	}
	return false
}

func responsesCoreTool(tool map[string]any) bool {
	if asString(tool["type"]) != "function" {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(asString(tool["name"])))
	if name == "" {
		return false
	}
	for _, token := range []string{
		"read", "write", "edit", "patch", "file", "grep", "glob",
		"shell", "bash", "exec", "command", "terminal", "git", "plan",
	} {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

func searchDeferredResponsesTools(initial []map[string]any, query string, limit int) []responsesFunctionCandidate {
	if limit <= 0 {
		limit = proxyToolSearchResultMax
	}
	candidates := responsesFunctionCandidates(initial)
	selected := selectResponsesCoreFunctions(candidates)
	terms := responsesSearchTerms(query)

	type scoredCandidate struct {
		candidate responsesFunctionCandidate
		score     int
	}
	deferred := make([]responsesFunctionCandidate, 0, len(candidates))
	scored := make([]scoredCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if selected[candidate.ordinal] {
			continue
		}
		deferred = append(deferred, candidate)
		if score := scoreResponsesToolCandidate(candidate, query, terms); score > 0 {
			scored = append(scored, scoredCandidate{candidate: candidate, score: score})
		}
	}

	// Generic discovery requests ("all available tools") and lexical misses get
	// a deterministic catalog slice instead of an empty result. This prevents the
	// model from incorrectly concluding that the deferred registry is empty.
	if len(scored) == 0 {
		if len(deferred) > limit {
			deferred = deferred[:limit]
		}
		return deferred
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].candidate.ordinal < scored[j].candidate.ordinal
		}
		return scored[i].score > scored[j].score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]responsesFunctionCandidate, len(scored))
	for i, candidate := range scored {
		out[i] = candidate.candidate
	}
	return out
}

func responsesSearchTerms(query string) []string {
	stop := map[string]bool{
		"all": true, "available": true, "tool": true, "tools": true,
		"find": true, "search": true, "please": true, "the": true,
		"a": true, "an": true, "to": true, "for": true, "of": true,
		"use": true, "with": true,
	}
	raw := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool)
	for _, term := range raw {
		if len(term) < 2 || stop[term] || seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
	}
	return out
}

func scoreResponsesToolCandidate(candidate responsesFunctionCandidate, query string, terms []string) int {
	name := strings.ToLower(asString(candidate.tool["name"]))
	namespace := strings.ToLower(candidate.namespace)
	description := strings.ToLower(asString(candidate.tool["description"]))
	query = strings.ToLower(strings.TrimSpace(query))

	score := 0
	if query != "" {
		if strings.Contains(name, query) {
			score += 120
		}
		if namespace != "" && strings.Contains(namespace, query) {
			score += 70
		}
		if strings.Contains(description, query) {
			score += 25
		}
	}
	allMatched := len(terms) > 0
	for _, term := range terms {
		matched := false
		if strings.Contains(name, term) {
			score += 40
			matched = true
		}
		if namespace != "" && strings.Contains(namespace, term) {
			score += 25
			matched = true
		}
		if strings.Contains(description, term) {
			score += 8
			matched = true
		}
		if !matched {
			allMatched = false
		}
	}
	if allMatched {
		score += 40
	}
	return score
}

func responsesSearchResultTools(matches []responsesFunctionCandidate) []map[string]any {
	out := make([]map[string]any, 0, len(matches))
	namespaceIndexes := make(map[string]int)
	for _, candidate := range matches {
		clone := cloneResponsesTool(candidate.tool)
		delete(clone, "defer_loading")
		if candidate.namespace == "" {
			out = append(out, clone)
			continue
		}
		key := candidate.namespace + "\x00" + candidate.namespaceDescription
		index, ok := namespaceIndexes[key]
		if !ok {
			namespace := map[string]any{
				"type":  "namespace",
				"name":  candidate.namespace,
				"tools": []map[string]any{},
			}
			if candidate.namespaceDescription != "" {
				namespace["description"] = candidate.namespaceDescription
			}
			out = append(out, namespace)
			index = len(out) - 1
			namespaceIndexes[key] = index
		}
		children := mapsFromAny(out[index]["tools"])
		children = append(children, clone)
		out[index]["tools"] = children
	}
	return out
}

func responsesCandidateNames(candidates []responsesFunctionCandidate, selected map[int]bool, wantSelected bool, limit int) []string {
	out := make([]string, 0)
	for _, candidate := range candidates {
		if selected[candidate.ordinal] != wantSelected {
			continue
		}
		out = append(out, responsesCandidateDisplayName(candidate))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func responsesCandidateDisplayName(candidate responsesFunctionCandidate) string {
	name := strings.TrimSpace(asString(candidate.tool["name"]))
	if candidate.namespace != "" {
		return candidate.namespace + "__" + name
	}
	return name
}

func cloneResponsesTool(tool map[string]any) map[string]any {
	clone := make(map[string]any, len(tool)+1)
	for key, value := range tool {
		clone[key] = value
	}
	return clone
}
