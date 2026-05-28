package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	MaxPods        int
	LabelFilter    string
	DefaultProfile string
	DefaultFlavors string
	GotifyURL      string
	GotifyToken    string
	AgentImage     string
	GithubToken    string
	TargetRepo     string
	Namespace      string
	SecretName     string
}

func Load() (*Config, error) {
	maxPods, err := strconv.Atoi(env("MAX_PODS", "4"))
	if err != nil {
		return nil, fmt.Errorf("ungültiger Wert für MAX_PODS: %w", err)
	}
	return &Config{
		MaxPods:        maxPods,
		LabelFilter:    env("LABEL_FILTER", "agent-backlog"),
		DefaultProfile: env("DEFAULT_PROFILE", "backend"),
		DefaultFlavors: env("DEFAULT_FLAVORS", "github,gitops"),
		GotifyURL:      mustEnv("GOTIFY_URL"),
		GotifyToken:    os.Getenv("GOTIFY_TOKEN"),
		AgentImage:     mustEnv("AGENT_IMAGE"),
		GithubToken:    mustEnv("GITHUB_TOKEN"),
		TargetRepo:     mustEnv("TARGET_REPO"),
		Namespace:      env("NAMESPACE", "claude-agent"),
		SecretName:     env("SECRET_NAME", "claude-agent-secrets"),
	}, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("Pflicht-Env nicht gesetzt: %s", key))
	}
	return v
}
