package core

import "testing"

func TestValidateResponseInputInvariants_MissingCallID(t *testing.T) {
	err := ValidateResponseInputInvariants([]map[string]any{{
		"type": "function_call_output",
	}})
	if err == nil {
		t.Fatal("expected invariant error")
	}
}

func TestValidateResponseInputInvariants_SystemNotFirst(t *testing.T) {
	err := ValidateResponseInputInvariants([]map[string]any{
		{"type": "message", "role": "user"},
		{"type": "message", "role": "system"},
	})
	if err == nil {
		t.Fatal("expected invariant error")
	}
}
