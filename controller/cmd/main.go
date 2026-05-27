package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/erlenfrosch/claude-k8s-agent/controller/internal/config"
	ctrl "github.com/erlenfrosch/claude-k8s-agent/controller/internal/controller"
	ghclient "github.com/erlenfrosch/claude-k8s-agent/controller/internal/github"
	k8sclient "github.com/erlenfrosch/claude-k8s-agent/controller/internal/k8s"
	ntfyclient "github.com/erlenfrosch/claude-k8s-agent/controller/internal/ntfy"
)

// Adapter-Typen passen die konkreten Clients an die Controller-Interfaces an.

type ghAdapter struct{ c *ghclient.Client }

func (a *ghAdapter) ListBacklogIssues(ctx context.Context, repo, label string) ([]ctrl.Issue, error) {
	issues, err := a.c.ListBacklogIssues(ctx, repo, label)
	if err != nil {
		return nil, err
	}
	result := make([]ctrl.Issue, len(issues))
	for i, issue := range issues {
		result[i] = ctrl.Issue{Number: issue.Number, Title: issue.Title, Body: issue.Body}
	}
	return result, nil
}
func (a *ghAdapter) SetRunningLabel(ctx context.Context, repo string, n int, add, rem string) error {
	return a.c.SetRunningLabel(ctx, repo, n, add, rem)
}
func (a *ghAdapter) ResetToBacklog(ctx context.Context, repo string, n int, b, r string) error {
	return a.c.ResetToBacklog(ctx, repo, n, b, r)
}

type k8sAdapter struct{ c *k8sclient.Client }

func (a *k8sAdapter) CountRunningJobs(ctx context.Context) (int, error) {
	return a.c.CountRunningJobs(ctx)
}
func (a *k8sAdapter) JobExists(ctx context.Context, name string) (bool, error) {
	return a.c.JobExists(ctx, name)
}
func (a *k8sAdapter) CreateAgentJob(ctx context.Context, p ctrl.JobParams) error {
	return a.c.CreateAgentJob(ctx, k8sclient.JobParams{
		IssueNumber: p.IssueNumber, IssueTitle: p.IssueTitle,
		TargetRepo: p.TargetRepo, AgentImage: p.AgentImage,
		SecretName: p.SecretName, NtfyURL: p.NtfyURL, NtfyTopic: p.NtfyTopic,
		Profile: p.Profile, Flavors: p.Flavors,
	})
}

type ntfyAdapter struct{ c *ntfyclient.Client }

func (a *ntfyAdapter) Send(title, body, priority string) error {
	return a.c.Send(ntfyclient.Message{Title: title, Body: body, Priority: priority})
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Config laden fehlgeschlagen", "error", err)
		os.Exit(1)
	}

	k8sClient, err := k8sclient.NewInClusterClient(cfg.Namespace)
	if err != nil {
		slog.Error("K8s-Client fehlgeschlagen", "error", err)
		os.Exit(1)
	}

	c := ctrl.New(cfg,
		&ghAdapter{c: ghclient.NewClient(cfg.GithubToken)},
		&k8sAdapter{c: k8sClient},
		&ntfyAdapter{c: ntfyclient.NewClient(cfg.NtfyURL, cfg.NtfyTopic, cfg.NtfyAuthToken)},
	)

	if err := c.Reconcile(context.Background()); err != nil {
		slog.Error("Reconcile fehlgeschlagen", "error", err)
		os.Exit(1)
	}
	slog.Info("Reconcile abgeschlossen")
}
