package qoder

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQoderEncodeBodyObfuscatesJSON(t *testing.T) {
	body := []byte(`{"stream":true,"messages":[{"role":"user","content":"inspect project"}],"tools":[{"type":"function","function":{"name":"Read","parameters":{"type":"object"}}}]}`)
	encoded := qoderEncodeBody(body)
	if len(encoded) == 0 {
		t.Fatal("qoderEncodeBody returned empty body")
	}
	if len(encoded) == len(body) && string(encoded) == string(body) {
		t.Fatal("encoded body equals plaintext")
	}
	var decoded any
	if json.Unmarshal(encoded, &decoded) == nil {
		t.Fatalf("encoded body must not remain valid JSON: %q", encoded)
	}
	for _, plain := range []string{`"tools"`, `"function"`, `"Read"`} {
		if strings.Contains(string(encoded), plain) {
			t.Fatalf("encoded body still contains plaintext fragment %q", plain)
		}
	}
}

func TestQoderEncodeBodyDeterministic(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	first := qoderEncodeBody(body)
	second := qoderEncodeBody(body)
	if string(first) != string(second) {
		t.Fatalf("encoding must be deterministic: %q != %q", first, second)
	}
}
