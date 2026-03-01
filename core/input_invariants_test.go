package core

import "testing"

func TestValidateResponseInputInvariants_RejectsOutputWithoutCallID(t *testing.T) {
	err := ValidateResponseInputInvariants(NewResponseInputItems([]ResponseInputItem{
		{Type: "function_call_output", Output: "{}"},
	}))
	if err == nil {
		t.Fatal("expected invariant error")
	}
}

func TestValidateResponseInputInvariants_RejectsSystemNotFirst(t *testing.T) {
	err := ValidateResponseInputInvariants(NewResponseInputItems([]ResponseInputItem{
		{Type: "message", Role: "user"},
		{Type: "message", Role: "system"},
	}))
	if err == nil {
		t.Fatal("expected system-order invariant error")
	}
}
