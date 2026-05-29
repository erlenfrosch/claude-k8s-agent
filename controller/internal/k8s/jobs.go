package k8s

import (
	"context"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// JobParams enthält alle Parameter zum Erstellen eines Agent-Jobs.
type JobParams struct {
	IssueNumber string
	IssueTitle  string
	TargetRepo  string
	AgentImage  string
	SecretName  string
	GotifyURL   string
	Profile     string
	Flavors     string
}

// Client kapselt K8s-Operationen für den Issue Controller.
type Client struct {
	cs        kubernetes.Interface
	namespace string
}

// NewInClusterClient erstellt einen K8s-Client mit In-Cluster-Konfiguration.
func NewInClusterClient(namespace string) (*Client, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config laden: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("K8s-Client erstellen: %w", err)
	}
	return &Client{cs: cs, namespace: namespace}, nil
}

// NewClient erstellt einen Client mit übergebenem Interface (für Tests).
func NewClient(cs kubernetes.Interface, namespace string) *Client {
	return &Client{cs: cs, namespace: namespace}
}

// CountRunningJobs zählt aktiv laufende Agent-Jobs.
func (c *Client) CountRunningJobs(ctx context.Context) (int, error) {
	jobs, err := c.cs.BatchV1().Jobs(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=claude-agent",
	})
	if err != nil {
		return 0, fmt.Errorf("Jobs listen: %w", err)
	}
	count := 0
	for _, j := range jobs.Items {
		if j.Status.Active > 0 {
			count++
		}
	}
	return count, nil
}

// JobExists prüft ob ein Job mit dem Namen bereits existiert.
func (c *Client) JobExists(ctx context.Context, name string) (bool, error) {
	_, err := c.cs.BatchV1().Jobs(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("Job %s prüfen: %w", name, err)
	}
	return true, nil
}

// CreateAgentJob erstellt einen neuen K8s Job für ein GitHub Issue.
func (c *Client) CreateAgentJob(ctx context.Context, p JobParams) error {
	jobName := fmt.Sprintf("claude-agent-issue-%s", p.IssueNumber)
	ttl := int32(3600)
	uid := int64(0)
	priv := true // Privileged ermöglicht Docker-Socket-Zugriff für Claude Code im Container

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: jobName, Namespace: c.namespace,
			Labels: map[string]string{"app": "claude-agent", "issue": p.IssueNumber},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "claude-agent"}},
				Spec: corev1.PodSpec{
					RestartPolicy:   corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{RunAsUser: &uid},
					InitContainers: []corev1.Container{{
						Name:    "setup",
						Image:   p.AgentImage,
						Command: []string{"/bin/bash", "-c", buildSetupScript(p)},
						Env:     []corev1.EnvVar{secretEnv("GITHUB_TOKEN", p.SecretName, "GITHUB_TOKEN")},
						VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: "/workspace"}},
					}},
					Containers: []corev1.Container{{
						Name:    "claude-agent",
						Image:   p.AgentImage,
						Command: []string{"/usr/local/bin/entrypoint.sh"},
						SecurityContext: &corev1.SecurityContext{Privileged: &priv},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("2Gi"),
								corev1.ResourceCPU:    resource.MustParse("500m"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("4Gi"),
								corev1.ResourceCPU:    resource.MustParse("2000m"),
							},
						},
						Env: []corev1.EnvVar{
							// ANTHROPIC_API_KEY und CLAUDE_CREDENTIALS sind beide optional —
							// es genügt eines der beiden zur Authentifizierung:
							//   ANTHROPIC_API_KEY  → Pay-per-use API
							//   CLAUDE_CREDENTIALS → OAuth-JSON aus Claude.ai Pro/Max-Abo
							secretEnvOptional("ANTHROPIC_API_KEY", p.SecretName, "ANTHROPIC_API_KEY"),
							secretEnvOptional("CLAUDE_CREDENTIALS", p.SecretName, "CLAUDE_CREDENTIALS"),
							secretEnv("GITHUB_TOKEN", p.SecretName, "GITHUB_TOKEN"),
							secretEnvOptional("GOTIFY_TOKEN", p.SecretName, "GOTIFY_TOKEN"),
							{Name: "GOTIFY_URL", Value: p.GotifyURL},
							{Name: "ISSUE_NUMBER", Value: p.IssueNumber},
							{Name: "ISSUE_TITLE", Value: p.IssueTitle},
							{Name: "TARGET_REPO", Value: p.TargetRepo},
						},
						VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: "/workspace"}},
					}},
					Volumes: []corev1.Volume{{
						Name: "workspace",
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					}},
				},
			},
		},
	}
	_, err := c.cs.BatchV1().Jobs(c.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("Job %s erstellen: %w", jobName, err)
	}
	return nil
}

func buildSetupScript(p JobParams) string {
	lines := []string{
		"set -euo pipefail",
		fmt.Sprintf(`git clone "https://${GITHUB_TOKEN}@github.com/%s.git" /workspace`, p.TargetRepo),
		"cd /workspace",
		fmt.Sprintf(`PROFILE="%s"`, p.Profile),
		fmt.Sprintf(`FLAVORS="%s"`, p.Flavors),
		`if [ -f .claude-agent.yaml ]; then`,
		`  YQ_PROFILE=$(grep -A1 'profile:' .claude-agent.yaml | tail -1 | tr -d ' :' | grep -v '^$' || true)`,
		`  if [ -n "$YQ_PROFILE" ]; then PROFILE="$YQ_PROFILE"; fi`,
		`  # Flavors als kommagetrennte Liste aus dem YAML-Array lesen`,
		`  YQ_FLAVORS=$(sed -n '/flavors:/,/^  [a-z]/p' .claude-agent.yaml | grep '^\s*- ' | sed 's/^\s*- //' | tr '\n' ',' | sed 's/,$//' || true)`,
		`  if [ -n "$YQ_FLAVORS" ]; then FLAVORS="$YQ_FLAVORS"; fi`,
		`fi`,
		// forgecrate init kann scheitern wenn ein Plugin im Marketplace fehlt (z.B. "superpowers").
		// CLAUDE.md, settings.json und Hooks werden trotzdem korrekt angelegt.
		`forgecrate init --profile "$PROFILE" --flavors "$FLAVORS" || echo "WARNUNG: forgecrate init unvollstaendig (Plugin fehlt im Marketplace)"`,
		// Titel als Variable setzen, um printf-Format-Injection durch Sonderzeichen (%) zu vermeiden
		fmt.Sprintf(`ISSUE_TITLE=%q`, p.IssueTitle),
		fmt.Sprintf(`printf "GitHub Issue #%s: %%s\n\nArbeite dieses Issue vollstaendig ab.\nErstelle Branch agent/issue-%s, implementiere die Loesung und oeffne einen PR.\n" "$ISSUE_TITLE" > /workspace/.agent-prompt`,
			p.IssueNumber, p.IssueNumber),
	}
	return strings.Join(lines, "\n")
}

func secretEnv(name, secretName, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  key,
			},
		},
	}
}

// secretEnvOptional liefert eine Env-Var aus einem Secret, die nicht zwingend vorhanden sein muss.
// Fehlt der Key im Secret, wird die Env-Var leer gesetzt statt den Pod scheitern zu lassen.
func secretEnvOptional(name, secretName, key string) corev1.EnvVar {
	optional := true
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  key,
				Optional:             &optional,
			},
		},
	}
}
