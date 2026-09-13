package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateJITRunner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/bonyai/demo/actions/runners/generate-jitconfig" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatal("missing installation token")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"encoded_jit_config":"config","runner":{"name":"tyto-1"}}`))
	}))
	defer server.Close()
	result, err := (GitHubJITClient{BaseURL: server.URL}).CreateJITRunner(context.Background(), "token", "bonyai", "demo", "tyto-1", []string{"tyto"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Encoded != "config" || result.Name != "tyto-1" {
		t.Fatalf("unexpected runner: %#v", result)
	}
}
