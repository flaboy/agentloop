package core

import "testing"

func TestAPISurface_Compile(t *testing.T) {
	type demoState struct {
		ID string
	}

	var resolver PolicyResolver[demoState]
	if resolver != nil {
		t.Fatalf("expected nil resolver in compile contract check")
	}

	var tool Tool[demoState]
	if tool != nil {
		t.Fatalf("expected nil tool in compile contract check")
	}
}
