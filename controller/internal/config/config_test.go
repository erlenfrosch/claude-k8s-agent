package config_test

import (
	"testing"
	"github.com/erlenfrosch/claude-k8s-agent/controller/internal/config"
)

func TestLoad_defaults(t *testing.T) {
	t.Setenv("NTFY_URL", "http://ntfy.test")
	t.Setenv("NTFY_TOPIC", "test")
	t.Setenv("AGENT_IMAGE", "img:latest")
	t.Setenv("GITHUB_TOKEN", "tok")
	t.Setenv("TARGET_REPO", "owner/repo")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxPods != 4 {
		t.Errorf("expected MaxPods=4, got %d", cfg.MaxPods)
	}
	if cfg.LabelFilter != "agent-backlog" {
		t.Errorf("wrong label: %s", cfg.LabelFilter)
	}
	if cfg.Namespace != "claude-agent" {
		t.Errorf("wrong namespace: %s", cfg.Namespace)
	}
}

func TestLoad_customValues(t *testing.T) {
	t.Setenv("MAX_PODS", "2")
	t.Setenv("NTFY_URL", "http://ntfy.test")
	t.Setenv("NTFY_TOPIC", "alerts")
	t.Setenv("AGENT_IMAGE", "img:v1")
	t.Setenv("GITHUB_TOKEN", "tok")
	t.Setenv("TARGET_REPO", "o/r")
	t.Setenv("LABEL_FILTER", "ready")
	t.Setenv("NAMESPACE", "agents")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxPods != 2 {
		t.Errorf("expected 2, got %d", cfg.MaxPods)
	}
	if cfg.Namespace != "agents" {
		t.Errorf("expected agents, got %s", cfg.Namespace)
	}
}

func TestLoad_invalidMaxPods(t *testing.T) {
	t.Setenv("MAX_PODS", "notanumber")
	t.Setenv("NTFY_URL", "http://x")
	t.Setenv("NTFY_TOPIC", "t")
	t.Setenv("AGENT_IMAGE", "i")
	t.Setenv("GITHUB_TOKEN", "t")
	t.Setenv("TARGET_REPO", "o/r")

	_, err := config.Load()
	if err == nil {
		t.Error("expected error for invalid MAX_PODS")
	}
}
