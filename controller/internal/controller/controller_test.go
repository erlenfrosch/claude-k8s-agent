package controller_test

import (
	"context"
	"errors"
	"testing"

	"github.com/erlenfrosch/claude-k8s-agent/controller/internal/config"
	"github.com/erlenfrosch/claude-k8s-agent/controller/internal/controller"
)

type mockGitHub struct {
	issues      []controller.Issue
	labelCalls  int
	shouldError bool
}

func (m *mockGitHub) ListBacklogIssues(_ context.Context, _, _ string) ([]controller.Issue, error) {
	if m.shouldError {
		return nil, errors.New("github error")
	}
	return m.issues, nil
}
func (m *mockGitHub) SetRunningLabel(_ context.Context, _ string, _ int, _, _ string) error {
	m.labelCalls++
	return nil
}
func (m *mockGitHub) ResetToBacklog(_ context.Context, _ string, _ int, _, _ string) error {
	return nil
}

type mockK8s struct {
	runningCount int
	jobExists    bool
	created      []string
	shouldError  bool
}

func (m *mockK8s) CountRunningJobs(_ context.Context) (int, error) { return m.runningCount, nil }
func (m *mockK8s) JobExists(_ context.Context, _ string) (bool, error) {
	return m.jobExists, nil
}
func (m *mockK8s) CreateAgentJob(_ context.Context, p controller.JobParams) error {
	if m.shouldError {
		return errors.New("k8s error")
	}
	m.created = append(m.created, p.IssueNumber)
	return nil
}

type mockNtfy struct{ sent []string }

func (m *mockNtfy) Send(title, _, _ string) error {
	m.sent = append(m.sent, title)
	return nil
}

func TestReconcile_spawnsJobsUpToMax(t *testing.T) {
	cfg := &config.Config{MaxPods: 3, LabelFilter: "agent-backlog", TargetRepo: "owner/repo"}
	gh := &mockGitHub{issues: []controller.Issue{
		{Number: 1, Title: "I1"}, {Number: 2, Title: "I2"},
		{Number: 3, Title: "I3"}, {Number: 4, Title: "I4"},
	}}
	k8s := &mockK8s{runningCount: 1} // 1 läuft bereits => max 2 neue
	c := controller.New(cfg, gh, k8s, &mockNtfy{})

	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(k8s.created) != 2 {
		t.Errorf("expected 2 new jobs, got %d", len(k8s.created))
	}
}

func TestReconcile_skipsWhenMaxReached(t *testing.T) {
	cfg := &config.Config{MaxPods: 2, LabelFilter: "agent-backlog", TargetRepo: "owner/repo"}
	gh := &mockGitHub{issues: []controller.Issue{{Number: 1, Title: "I1"}}}
	k8s := &mockK8s{runningCount: 2}
	c := controller.New(cfg, gh, k8s, &mockNtfy{})

	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(k8s.created) != 0 {
		t.Errorf("expected 0 new jobs, got %d", len(k8s.created))
	}
}

func TestReconcile_skipsExistingJob(t *testing.T) {
	cfg := &config.Config{MaxPods: 4, LabelFilter: "agent-backlog", TargetRepo: "owner/repo"}
	gh := &mockGitHub{issues: []controller.Issue{{Number: 42, Title: "Already running"}}}
	k8s := &mockK8s{runningCount: 0, jobExists: true}
	c := controller.New(cfg, gh, k8s, &mockNtfy{})

	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(k8s.created) != 0 {
		t.Errorf("expected 0 jobs (already exists), got %d", len(k8s.created))
	}
}

func TestReconcile_noIssues(t *testing.T) {
	cfg := &config.Config{MaxPods: 4, LabelFilter: "agent-backlog", TargetRepo: "owner/repo"}
	c := controller.New(cfg, &mockGitHub{}, &mockK8s{}, &mockNtfy{})

	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
