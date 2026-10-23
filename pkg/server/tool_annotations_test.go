package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSampleToolAnnotations(t *testing.T) {
	t.Setenv("MCP_API_KEY", "test-key")
	req := httptest.NewRequest(http.MethodPost, "/mcp?key=test-key", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Result struct {
			Tools []struct {
				Name        string                     `json:"name"`
				Annotations map[string]json.RawMessage `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode tools/list response: %v", err)
	}
	if len(response.Result.Tools) != 2 {
		t.Fatalf("expected two example tools, got %d", len(response.Result.Tools))
	}
	for _, tool := range response.Result.Tools {
		if string(tool.Annotations["readOnlyHint"]) != "true" {
			t.Errorf("%s: expected readOnlyHint=true, got %s", tool.Name, tool.Annotations["readOnlyHint"])
		}
		if _, present := tool.Annotations["openWorldHint"]; present {
			t.Errorf("%s: openWorldHint must be omitted", tool.Name)
		}
	}
}
