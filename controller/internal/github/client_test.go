package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gh "github.com/erlenfrosch/claude-k8s-agent/controller/internal/github"
)

func TestListBacklogIssues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/issues" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("labels") != "agent-backlog" {
			t.Errorf("expected label filter, got: %s", r.URL.Query().Get("labels"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"number": 42, "title": "Fix auth bug", "body": "Auth fails"},
			{"number": 43, "title": "Dark mode", "body": "Add dark mode"},
		})
	}))
	defer srv.Close()

	client := gh.NewClientWithBaseURL("fake-token", srv.URL+"/")
	issues, err := client.ListBacklogIssues(context.Background(), "owner/repo", "agent-backlog")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
	if issues[0].Number != 42 {
		t.Errorf("expected issue 42, got %d", issues[0].Number)
	}
	if issues[0].Title != "Fix auth bug" {
		t.Errorf("unexpected title: %s", issues[0].Title)
	}
}

func TestSetRunningLabel(t *testing.T) {
	addCalled, removeCalled := false, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			addCalled = true
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]map[string]string{{"name": "agent-running"}})
			return
		}
		if r.Method == http.MethodDelete {
			removeCalled = true
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]map[string]string{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := gh.NewClientWithBaseURL("fake-token", srv.URL+"/")
	err := client.SetRunningLabel(context.Background(), "owner/repo", 42, "agent-running", "agent-backlog")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !addCalled || !removeCalled {
		t.Errorf("addCalled=%v removeCalled=%v", addCalled, removeCalled)
	}
}
