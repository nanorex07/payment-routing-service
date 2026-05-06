package httpadapter

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIsValidJSON(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(openAPISpec), &spec); err != nil {
		t.Fatal(err)
	}
	if spec["openapi"] != "3.0.3" {
		t.Fatalf("got openapi=%v want 3.0.3", spec["openapi"])
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths missing")
	}
	for _, path := range []string{"/healthz", "/transactions/initiate", "/transactions/callback"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("path %s missing", path)
		}
	}
}
