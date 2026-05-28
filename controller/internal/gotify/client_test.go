package gotify_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/erlenfrosch/claude-k8s-agent/controller/internal/gotify"
)

func TestSend_success(t *testing.T) {
	var gotTitle, gotMsg, gotToken string
	var gotPriority int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.URL.Query().Get("token")
		var m struct {
			Title    string `json:"title"`
			Message  string `json:"message"`
			Priority int    `json:"priority"`
		}
		json.NewDecoder(r.Body).Decode(&m) //nolint:errcheck
		gotTitle = m.Title
		gotMsg = m.Message
		gotPriority = m.Priority
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := gotify.NewClient(srv.URL, "test-token")
	err := client.Send("Agent laeuft: Issue #42", "Session: https://claude.ai/test", "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotToken != "test-token" {
		t.Errorf("token: got %q, want %q", gotToken, "test-token")
	}
	if gotTitle != "Agent laeuft: Issue #42" {
		t.Errorf("title: got %q", gotTitle)
	}
	if gotMsg != "Session: https://claude.ai/test" {
		t.Errorf("message: got %q", gotMsg)
	}
	if gotPriority != 5 {
		t.Errorf("priority: got %d, want 5", gotPriority)
	}
}

func TestSend_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := gotify.NewClient(srv.URL, "token")
	if err := client.Send("test", "body", "default"); err == nil {
		t.Error("expected error on 500 response, got nil")
	}
}

func TestSend_priorityMapping(t *testing.T) {
	cases := []struct {
		input    string
		expected int
	}{
		{"default", 5},
		{"high", 7},
		{"urgent", 9},
		{"", 5},
	}

	for _, tc := range cases {
		tc := tc
		var gotPriority int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var m struct{ Priority int `json:"priority"` }
			json.NewDecoder(r.Body).Decode(&m) //nolint:errcheck
			gotPriority = m.Priority
			w.WriteHeader(http.StatusOK)
		}))
		client := gotify.NewClient(srv.URL, "token")
		if err := client.Send("t", "b", tc.input); err != nil {
			t.Errorf("priority %q: unexpected error: %v", tc.input, err)
		}
		if gotPriority != tc.expected {
			t.Errorf("priority %q: got %d, want %d", tc.input, gotPriority, tc.expected)
		}
		srv.Close()
	}
}
