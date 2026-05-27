package controller

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/erlenfrosch/claude-k8s-agent/controller/internal/config"
)

// Issue ist die domänenspezifische Darstellung eines GitHub Issues.
type Issue struct {
	Number int
	Title  string
	Body   string
}

// JobParams wird an den K8s-Client übergeben.
type JobParams struct {
	IssueNumber string
	IssueTitle  string
	TargetRepo  string
	AgentImage  string
	SecretName  string
	NtfyURL     string
	NtfyTopic   string
	Profile     string
	Flavors     string
}

// GitHubClient definiert die vom Controller benötigten GitHub-Operationen.
type GitHubClient interface {
	ListBacklogIssues(ctx context.Context, repo, label string) ([]Issue, error)
	SetRunningLabel(ctx context.Context, repo string, number int, add, remove string) error
	ResetToBacklog(ctx context.Context, repo string, number int, backlog, running string) error
}

// K8sClient definiert die benötigten Kubernetes-Operationen.
type K8sClient interface {
	CountRunningJobs(ctx context.Context) (int, error)
	JobExists(ctx context.Context, name string) (bool, error)
	CreateAgentJob(ctx context.Context, p JobParams) error
}

// NtfyClient definiert die Benachrichtigungs-Operationen.
type NtfyClient interface {
	Send(title, body, priority string) error
}

// Controller orchestriert die Reconcile-Schleife.
type Controller struct {
	cfg  *config.Config
	gh   GitHubClient
	k8s  K8sClient
	ntfy NtfyClient
}

// New erstellt einen neuen Controller.
func New(cfg *config.Config, gh GitHubClient, k8s K8sClient, ntfy NtfyClient) *Controller {
	return &Controller{cfg: cfg, gh: gh, k8s: k8s, ntfy: ntfy}
}

// Reconcile führt einen Reconcile-Durchlauf durch.
func (c *Controller) Reconcile(ctx context.Context) error {
	issues, err := c.gh.ListBacklogIssues(ctx, c.cfg.TargetRepo, c.cfg.LabelFilter)
	if err != nil {
		return fmt.Errorf("Issues laden: %w", err)
	}
	if len(issues) == 0 {
		slog.Info("Keine offenen Backlog-Issues")
		return nil
	}

	running, err := c.k8s.CountRunningJobs(ctx)
	if err != nil {
		return fmt.Errorf("laufende Jobs zählen: %w", err)
	}
	slog.Info("Reconcile", "issues", len(issues), "running", running, "max", c.cfg.MaxPods)

	for _, issue := range issues {
		if running >= c.cfg.MaxPods {
			slog.Info("Max-Pods erreicht", "max", c.cfg.MaxPods)
			break
		}
		jobName := fmt.Sprintf("claude-agent-issue-%d", issue.Number)
		exists, err := c.k8s.JobExists(ctx, jobName)
		if err != nil {
			slog.Warn("Job-Prüfung fehlgeschlagen", "job", jobName, "error", err)
			continue
		}
		if exists {
			slog.Info("Job existiert bereits", "job", jobName)
			continue
		}
		if err := c.k8s.CreateAgentJob(ctx, JobParams{
			IssueNumber: fmt.Sprintf("%d", issue.Number),
			IssueTitle:  issue.Title,
			TargetRepo:  c.cfg.TargetRepo,
			AgentImage:  c.cfg.AgentImage,
			SecretName:  c.cfg.SecretName,
			NtfyURL:     c.cfg.NtfyURL,
			NtfyTopic:   c.cfg.NtfyTopic,
			Profile:     c.cfg.DefaultProfile,
			Flavors:     c.cfg.DefaultFlavors,
		}); err != nil {
			slog.Error("Job erstellen fehlgeschlagen", "issue", issue.Number, "error", err)
			_ = c.ntfy.Send(fmt.Sprintf("Fehler beim Spawnen: Issue #%d", issue.Number), err.Error(), "urgent")
			continue
		}
		if err := c.gh.SetRunningLabel(ctx, c.cfg.TargetRepo, issue.Number, "agent-running", c.cfg.LabelFilter); err != nil {
			slog.Warn("Label setzen fehlgeschlagen", "issue", issue.Number, "error", err)
		}
		slog.Info("Job erstellt", "job", jobName)
		running++
	}
	return nil
}
