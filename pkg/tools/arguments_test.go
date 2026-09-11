package tools

import "testing"

func TestParseArgumentsEmpty(t *testing.T) {
	v := ParseArguments("")
	if m, ok := v.(map[string]any); !ok || len(m) != 0 {
		t.Fatalf("empty must map to {}, got %#v", v)
	}
}

func TestParseArgumentsJSON(t *testing.T) {
	v := ParseArguments(`{"a":1}`)
	m, ok := v.(map[string]any)
	if !ok || m["a"] != float64(1) {
		t.Fatalf("got %#v", v)
	}
}

func TestParseArgumentsInvalidKept(t *testing.T) {
	v := ParseArguments(`{oops`)
	if s, ok := v.(string); !ok || s != "{oops" {
		t.Fatalf("invalid must be kept as string, got %#v", v)
	}
}
