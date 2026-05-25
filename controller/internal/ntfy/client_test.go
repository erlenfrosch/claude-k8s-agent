package ntfy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/erlenfrosch/claude-k8s-agent/controller/internal/ntfy"
)

func TestSend_success(t *testing.T) {
	var gotTitle, gotPriority, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.Header.Get("Title")
		gotPriority = r.Header.Get("Priority")
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := ntfy.NewClient(srv.URL, "test-topic", "")
	err := client.Send(ntfy.Message{Title: "Agent laeuft: Issue #42", Body: "Session: https://claude.ai/test", Priority: "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTitle != "Agent laeuft: Issue #42" {
		t.Errorf("unexpected title: %s", gotTitle)
	}
	if gotPriority != "default" {
		t.Errorf("unexpected priority: %s", gotPriority)
	}
	if gotBody != "Session: https://claude.ai/test" {
		t.Errorf("unexpected body: %s", gotBody)
	}
}

func TestSend_withAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := ntfy.NewClient(srv.URL, "test-topic", "mytoken")
	err := client.Send(ntfy.Message{Title: "test", Body: "body", Priority: "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer mytoken" {
		t.Errorf("expected Bearer auth, got: %s", gotAuth)
	}
}
