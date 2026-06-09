package config_test

import (
	"testing"
	"github.com/erlenfrosch/claude-k8s-agent/controller/internal/config"
)

func TestLoad_defaults(t *testing.T) {
	t.Setenv("GOTIFY_URL", "http://gotify.test")
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
	if cfg.LabelFilter != "" {
		t.Errorf("expected empty label filter by default, got: %s", cfg.LabelFilter)
	}
	if cfg.Namespace != "claude-agent" {
		t.Errorf("wrong namespace: %s", cfg.Namespace)
	}
}

func TestLoad_customValues(t *testing.T) {
	t.Setenv("MAX_PODS", "2")
	t.Setenv("GOTIFY_URL", "http://gotify.test")
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
	t.Setenv("GOTIFY_URL", "http://gotify.test")
	t.Setenv("AGENT_IMAGE", "i")
	t.Setenv("GITHUB_TOKEN", "t")
	t.Setenv("TARGET_REPO", "o/r")

	_, err := config.Load()
	if err == nil {
		t.Error("expected error for invalid MAX_PODS")
	}
}

func TestLoad_autorunEnabledDefault(t *testing.T) {
	t.Setenv("GOTIFY_URL", "http://gotify.test")
	t.Setenv("AGENT_IMAGE", "img:latest")
	t.Setenv("GITHUB_TOKEN", "tok")
	t.Setenv("TARGET_REPO", "owner/repo")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AutorunEnabled {
		t.Errorf("expected AutorunEnabled=false by default, got %v", cfg.AutorunEnabled)
	}
}

func TestLoad_autorunEnabledTrue(t *testing.T) {
	t.Setenv("GOTIFY_URL", "http://gotify.test")
	t.Setenv("AGENT_IMAGE", "img:latest")
	t.Setenv("GITHUB_TOKEN", "tok")
	t.Setenv("TARGET_REPO", "owner/repo")
	t.Setenv("AUTORUN_ENABLED", "true")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.AutorunEnabled {
		t.Errorf("expected AutorunEnabled=true, got %v", cfg.AutorunEnabled)
	}
}
