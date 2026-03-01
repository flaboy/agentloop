package responses

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flaboy/agentloop/core"
)

func TestClient_CreateResponse_ParsesFinalText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer srv.Close()

	c := NewClient(OpenAIConfig{BaseURL: srv.URL, Model: "gpt-5", APIKey: "k"}, srv.Client())
	res, err := c.CreateResponse(context.Background(), core.CreateResponseRequest{Input: "hi"})
	if err != nil {
		t.Fatalf("create response failed: %v", err)
	}
	if res.FinalText != "ok" {
		t.Fatalf("unexpected final text: %q", res.FinalText)
	}
}
