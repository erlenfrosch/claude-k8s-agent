# Autonomous Claude Code K8s Agent — Implementierungsplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Autonomen Claude Code Agenten in k3s deployen, der GitHub Issues mit Label `agent-backlog` selbstständig abarbeitet, PRs öffnet und Push-Benachrichtigungen via ntfy.sh sendet.

**Architecture:** Go-Binary (issue-controller) läuft als CronJob alle 10 min. Vergleicht offene GitHub Issues mit laufenden K8s Jobs, spawnt neue Jobs bis max-pods. Job-Pod: Init-Container (git clone + forgecrate init) + Main-Container (claude --share --dangerously-skip-permissions). entrypoint.sh extrahiert Share-URL, sendet ntfy-Notifications, überwacht Idle-Zustand per Watchdog. Alle Manifeste via ArgoCD aus diesem Repo deployed.

**Tech Stack:** Go 1.22, github.com/google/go-github/v61, k8s.io/client-go v0.29, Ubuntu 24.04, forgecrate (apt jmt-labs), @anthropic-ai/claude-code (npm), ntfy.sh, k3s, ArgoCD, Bitnami SealedSecrets

---

## Dateistruktur

```
claude-k8s-agent/
├── controller/
│   ├── cmd/main.go                       # Entrypoint: Config laden, einmal Reconcile
│   ├── internal/config/config.go         # Config-Struct + Laden aus Env-Variablen
│   ├── internal/config/config_test.go
│   ├── internal/github/client.go         # GitHub API: Issues listen, Labels setzen
│   ├── internal/github/client_test.go
│   ├── internal/k8s/jobs.go              # K8s: Jobs listen/zählen/erstellen
│   ├── internal/k8s/jobs_test.go
│   ├── internal/ntfy/client.go           # ntfy HTTP-Client
│   ├── internal/ntfy/client_test.go
│   ├── internal/controller/controller.go # Reconcile-Logik
│   ├── internal/controller/controller_test.go
│   ├── go.mod
│   └── go.sum
├── agent/
│   ├── Dockerfile                        # Multi-Stage: Go-Build + ubuntu:24.04 Runtime
│   └── entrypoint.sh                     # Share-URL, Watchdog, ntfy-Hooks
├── .github/workflows/
│   └── build-push.yml                    # CI: Image bei Push auf main bauen + pushen
├── deploy/
│   ├── namespace.yaml
│   ├── rbac.yaml
│   ├── configmap.yaml
│   ├── sealed-secret.example.yaml        # Vorlage — NICHT mit echten Werten committen
│   ├── controller/cronjob.yaml
│   ├── ntfy/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── ingress.yaml
│   │   └── pvc.yaml
│   └── argocd/application.yaml
├── .claude-agent.yaml.example
└── .gitignore
```

---

## Phase 1: Controller (Go Binary)

### Task 1: Go-Modul + Config

**Files:**
- Create: `controller/go.mod`
- Create: `controller/internal/config/config.go`
- Create: `controller/internal/config/config_test.go`

- [ ] **Step 1.1: Modul + Dependencies**

```bash
mkdir -p controller && cd controller
go mod init github.com/erlenfrosch/claude-k8s-agent/controller
go get github.com/google/go-github/v61@latest
go get golang.org/x/oauth2@latest
go get k8s.io/client-go@v0.29.14
go get k8s.io/api@v0.29.14
go get k8s.io/apimachinery@v0.29.14
go mod tidy
```

- [ ] **Step 1.2: Failing test schreiben** — `controller/internal/config/config_test.go`

```go
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
```

- [ ] **Step 1.3: Test ausführen — muss fehlschlagen**

```bash
cd controller && go test ./internal/config/... 2>&1 | head -3
```
Expected: compile error (package nicht vorhanden)

- [ ] **Step 1.4: config.go implementieren** — `controller/internal/config/config.go`

```go
package config

import (
    "fmt"
    "os"
    "strconv"
)

// Config enthält alle Laufzeit-Einstellungen des Issue Controllers.
// Alle Werte kommen aus Umgebungsvariablen (ConfigMap + Secret).
type Config struct {
    MaxPods        int
    LabelFilter    string
    DefaultProfile string
    DefaultFlavors string
    NtfyURL        string
    NtfyTopic      string
    NtfyAuthToken  string
    AgentImage     string
    GithubToken    string
    TargetRepo     string // Format: "owner/repo"
    Namespace      string
    SecretName     string
}

// Load liest die Konfiguration aus Umgebungsvariablen.
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
        NtfyURL:        mustEnv("NTFY_URL"),
        NtfyTopic:      mustEnv("NTFY_TOPIC"),
        NtfyAuthToken:  os.Getenv("NTFY_AUTH_TOKEN"),
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
```

- [ ] **Step 1.5: Tests ausführen — müssen bestehen**

```bash
cd controller && go test ./internal/config/... -v
```
Expected: PASS (3 Tests)

- [ ] **Step 1.6: Commit**

```bash
git add controller/ && git commit -m "feat(controller): add Go module and config loading"
```

---

### Task 2: GitHub Client

**Files:**
- Create: `controller/internal/github/client.go`
- Create: `controller/internal/github/client_test.go`

- [ ] **Step 2.1: Failing test schreiben** — `controller/internal/github/client_test.go`

```go
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
```

- [ ] **Step 2.2: Test ausführen — muss fehlschlagen**

```bash
cd controller && go test ./internal/github/... 2>&1 | head -3
```
Expected: compile error

- [ ] **Step 2.3: client.go implementieren** — `controller/internal/github/client.go`

```go
package github

import (
    "context"
    "fmt"
    "strings"

    gogithub "github.com/google/go-github/v61/github"
    "golang.org/x/oauth2"
)

// Issue repräsentiert ein GitHub Issue mit den controller-relevanten Feldern.
type Issue struct {
    Number int
    Title  string
    Body   string
}

// Client wraps den go-github Client.
type Client struct {
    gh *gogithub.Client
}

// NewClient erstellt einen authentifizierten GitHub-Client.
func NewClient(token string) *Client {
    ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
    tc := oauth2.NewClient(context.Background(), ts)
    return &Client{gh: gogithub.NewClient(tc)}
}

// NewClientWithBaseURL erstellt einen Client mit angepasster Base-URL (für Tests).
func NewClientWithBaseURL(token, baseURL string) *Client {
    ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
    tc := oauth2.NewClient(context.Background(), ts)
    c, _ := gogithub.NewClient(tc).WithAuthToken(token)
    c, _ = c.WithEnterpriseURLs(baseURL, baseURL)
    return &Client{gh: c}
}

// ListBacklogIssues gibt alle offenen Issues mit dem angegebenen Label zurück (älteste zuerst).
func (c *Client) ListBacklogIssues(ctx context.Context, repo, label string) ([]Issue, error) {
    owner, name, err := splitRepo(repo)
    if err != nil {
        return nil, err
    }
    issues, _, err := c.gh.Issues.ListByRepo(ctx, owner, name, &gogithub.IssueListByRepoOptions{
        Labels: []string{label}, State: "open", Direction: "asc", Sort: "created",
    })
    if err != nil {
        return nil, fmt.Errorf("GitHub Issues listen: %w", err)
    }
    result := make([]Issue, 0, len(issues))
    for _, i := range issues {
        if i.PullRequestLinks != nil {
            continue // PRs ignorieren
        }
        result = append(result, Issue{Number: i.GetNumber(), Title: i.GetTitle(), Body: i.GetBody()})
    }
    return result, nil
}

// SetRunningLabel fügt addLabel hinzu und entfernt removeLabel.
func (c *Client) SetRunningLabel(ctx context.Context, repo string, number int, addLabel, removeLabel string) error {
    owner, name, err := splitRepo(repo)
    if err != nil {
        return err
    }
    if _, _, err := c.gh.Issues.AddLabelsToIssue(ctx, owner, name, number, []string{addLabel}); err != nil {
        return fmt.Errorf("Label hinzufügen: %w", err)
    }
    if _, err := c.gh.Issues.RemoveLabelForIssue(ctx, owner, name, number, removeLabel); err != nil {
        return fmt.Errorf("Label entfernen: %w", err)
    }
    return nil
}

// ResetToBacklog setzt ein Issue von agent-running zurück auf agent-backlog.
func (c *Client) ResetToBacklog(ctx context.Context, repo string, number int, backlogLabel, runningLabel string) error {
    return c.SetRunningLabel(ctx, repo, number, backlogLabel, runningLabel)
}

func splitRepo(repo string) (owner, name string, err error) {
    parts := strings.SplitN(repo, "/", 2)
    if len(parts) != 2 {
        return "", "", fmt.Errorf("ungültiges Repo-Format %q, erwartet owner/repo", repo)
    }
    return parts[0], parts[1], nil
}
```

- [ ] **Step 2.4: Tests ausführen — müssen bestehen**

```bash
cd controller && go test ./internal/github/... -v
```
Expected: PASS (2 Tests)

- [ ] **Step 2.5: Commit**

```bash
git add controller/internal/github/ && git commit -m "feat(controller): add GitHub client with label management"
```

---

### Task 3: Kubernetes Jobs Client

**Files:**
- Create: `controller/internal/k8s/jobs.go`
- Create: `controller/internal/k8s/jobs_test.go`

- [ ] **Step 3.1: Failing test schreiben** — `controller/internal/k8s/jobs_test.go`

```go
package k8s_test

import (
    "context"
    "testing"

    batchv1 "k8s.io/api/batch/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes/fake"

    k8sclient "github.com/erlenfrosch/claude-k8s-agent/controller/internal/k8s"
)

func TestCountRunningJobs_empty(t *testing.T) {
    client := k8sclient.NewClient(fake.NewSimpleClientset(), "claude-agent")
    count, err := client.CountRunningJobs(context.Background())
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if count != 0 {
        t.Errorf("expected 0, got %d", count)
    }
}

func TestCountRunningJobs_withActive(t *testing.T) {
    active := int32(1)
    fakeCS := fake.NewSimpleClientset(
        &batchv1.Job{
            ObjectMeta: metav1.ObjectMeta{
                Name: "claude-agent-issue-1", Namespace: "claude-agent",
                Labels: map[string]string{"app": "claude-agent"},
            },
            Status: batchv1.JobStatus{Active: active},
        },
        &batchv1.Job{
            ObjectMeta: metav1.ObjectMeta{
                Name: "claude-agent-issue-2", Namespace: "claude-agent",
                Labels: map[string]string{"app": "claude-agent"},
            },
            Status: batchv1.JobStatus{Active: active},
        },
    )
    client := k8sclient.NewClient(fakeCS, "claude-agent")
    count, err := client.CountRunningJobs(context.Background())
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if count != 2 {
        t.Errorf("expected 2, got %d", count)
    }
}

func TestJobExists(t *testing.T) {
    fakeCS := fake.NewSimpleClientset(&batchv1.Job{
        ObjectMeta: metav1.ObjectMeta{Name: "claude-agent-issue-42", Namespace: "claude-agent"},
    })
    client := k8sclient.NewClient(fakeCS, "claude-agent")
    ctx := context.Background()

    exists, err := client.JobExists(ctx, "claude-agent-issue-42")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if !exists {
        t.Error("expected job to exist")
    }

    exists, err = client.JobExists(ctx, "claude-agent-issue-99")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if exists {
        t.Error("expected job to not exist")
    }
}

func TestCreateAgentJob(t *testing.T) {
    fakeCS := fake.NewSimpleClientset()
    client := k8sclient.NewClient(fakeCS, "claude-agent")

    err := client.CreateAgentJob(context.Background(), k8sclient.JobParams{
        IssueNumber: "42", IssueTitle: "Fix auth bug",
        TargetRepo: "owner/repo", AgentImage: "ghcr.io/test/agent:latest",
        SecretName: "claude-agent-secrets",
        NtfyURL: "http://ntfy.test", NtfyTopic: "claude-agent",
        Profile: "backend", Flavors: "github,gitops",
    })
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    job, err := fakeCS.BatchV1().Jobs("claude-agent").Get(
        context.Background(), "claude-agent-issue-42", metav1.GetOptions{},
    )
    if err != nil {
        t.Fatalf("job not found: %v", err)
    }
    if job.Labels["app"] != "claude-agent" {
        t.Errorf("expected app=claude-agent, got %s", job.Labels["app"])
    }
    if len(job.Spec.Template.Spec.InitContainers) != 1 {
        t.Errorf("expected 1 init container, got %d", len(job.Spec.Template.Spec.InitContainers))
    }
    if len(job.Spec.Template.Spec.Containers) != 1 {
        t.Errorf("expected 1 container, got %d", len(job.Spec.Template.Spec.Containers))
    }
}
```

- [ ] **Step 3.2: Test ausführen — muss fehlschlagen**

```bash
cd controller && go test ./internal/k8s/... 2>&1 | head -3
```
Expected: compile error

- [ ] **Step 3.3: jobs.go implementieren** — `controller/internal/k8s/jobs.go`

```go
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
    NtfyURL     string
    NtfyTopic   string
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
    priv := true

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
                        Name:  "setup",
                        Image: p.AgentImage,
                        Command: []string{"/bin/bash", "-c", buildSetupScript(p)},
                        Env: []corev1.EnvVar{secretEnv("GITHUB_TOKEN", p.SecretName, "GITHUB_TOKEN")},
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
                            secretEnv("ANTHROPIC_API_KEY", p.SecretName, "ANTHROPIC_API_KEY"),
                            secretEnv("GITHUB_TOKEN", p.SecretName, "GITHUB_TOKEN"),
                            secretEnv("NTFY_AUTH_TOKEN", p.SecretName, "NTFY_AUTH_TOKEN"),
                            {Name: "NTFY_URL", Value: p.NtfyURL},
                            {Name: "NTFY_TOPIC", Value: p.NtfyTopic},
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
        fmt.Sprintf(`git clone "https://$(GITHUB_TOKEN)@github.com/%s.git" /workspace`, p.TargetRepo),
        "cd /workspace",
        fmt.Sprintf(`PROFILE="%s"`, p.Profile),
        fmt.Sprintf(`FLAVORS="%s"`, p.Flavors),
        `if [ -f .claude-agent.yaml ]; then`,
        `  YQ_PROFILE=$(grep -A1 'profile:' .claude-agent.yaml | tail -1 | tr -d ' :' | grep -v '^$' || true)`,
        `  if [ -n "$YQ_PROFILE" ]; then PROFILE="$YQ_PROFILE"; fi`,
        `fi`,
        `forgecrate init --profile "$PROFILE" --flavors "$FLAVORS"`,
        fmt.Sprintf(`printf "GitHub Issue #%s: %s\n\nArbeite dieses Issue vollstaendig ab.\nErstelle Branch agent/issue-%s, implementiere die Loesung und oeffne einen PR.\n" > /workspace/.agent-prompt`,
            p.IssueNumber, p.IssueTitle, p.IssueNumber),
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
```

- [ ] **Step 3.4: Tests ausführen — müssen bestehen**

```bash
cd controller && go test ./internal/k8s/... -v
```
Expected: PASS (4 Tests)

- [ ] **Step 3.5: Commit**

```bash
git add controller/internal/k8s/ && git commit -m "feat(controller): add K8s jobs client"
```

---

### Task 4: ntfy Client

**Files:**
- Create: `controller/internal/ntfy/client.go`
- Create: `controller/internal/ntfy/client_test.go`

- [ ] **Step 4.1: Failing test schreiben** — `controller/internal/ntfy/client_test.go`

```go
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
```

- [ ] **Step 4.2: Test ausführen — muss fehlschlagen**

```bash
cd controller && go test ./internal/ntfy/... 2>&1 | head -3
```
Expected: compile error

- [ ] **Step 4.3: client.go implementieren** — `controller/internal/ntfy/client.go`

```go
package ntfy

import (
    "fmt"
    "net/http"
    "strings"
)

// Message repräsentiert eine ntfy-Benachrichtigung.
type Message struct {
    Title    string
    Body     string
    Priority string // "default", "high", "urgent"
}

// Client sendet Push-Benachrichtigungen an einen ntfy-Server.
type Client struct {
    baseURL   string
    topic     string
    authToken string
    http      *http.Client
}

// NewClient erstellt einen ntfy-Client.
func NewClient(baseURL, topic, authToken string) *Client {
    return &Client{
        baseURL:   strings.TrimRight(baseURL, "/"),
        topic:     topic,
        authToken: authToken,
        http:      &http.Client{},
    }
}

// Send sendet eine Nachricht an das konfigurierte ntfy-Topic.
func (c *Client) Send(msg Message) error {
    url := fmt.Sprintf("%s/%s", c.baseURL, c.topic)
    req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(msg.Body))
    if err != nil {
        return fmt.Errorf("request erstellen: %w", err)
    }
    req.Header.Set("Title", msg.Title)
    req.Header.Set("Priority", msg.Priority)
    req.Header.Set("Content-Type", "text/plain; charset=utf-8")
    if c.authToken != "" {
        req.Header.Set("Authorization", "Bearer "+c.authToken)
    }
    resp, err := c.http.Do(req)
    if err != nil {
        return fmt.Errorf("ntfy request: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode >= 400 {
        return fmt.Errorf("ntfy antwortete mit %d", resp.StatusCode)
    }
    return nil
}
```

- [ ] **Step 4.4: Tests ausführen — müssen bestehen**

```bash
cd controller && go test ./internal/ntfy/... -v
```
Expected: PASS (2 Tests)

- [ ] **Step 4.5: Commit**

```bash
git add controller/internal/ntfy/ && git commit -m "feat(controller): add ntfy notification client"
```

---

### Task 5: Controller Reconcile-Logik

**Files:**
- Create: `controller/internal/controller/controller.go`
- Create: `controller/internal/controller/controller_test.go`

- [ ] **Step 5.1: Failing test schreiben** — `controller/internal/controller/controller_test.go`

```go
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
```

- [ ] **Step 5.2: Test ausführen — muss fehlschlagen**

```bash
cd controller && go test ./internal/controller/... 2>&1 | head -3
```
Expected: compile error

- [ ] **Step 5.3: controller.go implementieren** — `controller/internal/controller/controller.go`

```go
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
```

- [ ] **Step 5.4: Tests ausführen — müssen bestehen**

```bash
cd controller && go test ./internal/controller/... -v
```
Expected: PASS (4 Tests)

- [ ] **Step 5.5: Commit**

```bash
git add controller/internal/controller/ && git commit -m "feat(controller): add reconcile logic"
```

---

### Task 6: Main Entrypoint

**Files:**
- Create: `controller/cmd/main.go`

- [ ] **Step 6.1: main.go schreiben** — `controller/cmd/main.go`

```go
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
```

- [ ] **Step 6.2: Build prüfen**

```bash
cd controller && go build ./...
```
Expected: kein Fehler

- [ ] **Step 6.3: Alle Tests ausführen**

```bash
cd controller && go test ./... -v
```
Expected: alle Tests PASS

- [ ] **Step 6.4: Commit**

```bash
git add controller/cmd/ && git commit -m "feat(controller): add main entrypoint and adapters"
```

---

## Phase 2: Agent Container

### Task 7: Dockerfile (Multi-Stage)

**Files:**
- Create: `agent/Dockerfile`

- [ ] **Step 7.1: Dockerfile schreiben** — `agent/Dockerfile`

```dockerfile
# Stage 1: Go Controller Binary bauen
FROM golang:1.22-bookworm AS controller-build

WORKDIR /src
COPY controller/ .
RUN go build -o /issue-controller ./cmd/main.go

# Stage 2: Agent Runtime
FROM ubuntu:24.04

LABEL org.opencontainers.image.source="https://github.com/erlenfrosch/claude-k8s-agent"
LABEL org.opencontainers.image.description="Autonomer Claude Code Agent fuer Kubernetes"

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    git curl ca-certificates gnupg jq \
    && rm -rf /var/lib/apt/lists/*

# Node.js 20
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y nodejs \
    && rm -rf /var/lib/apt/lists/*

# forgecrate via apt
RUN curl -fsSL https://jmt-labs.github.io/apt/KEY.gpg \
    | gpg --dearmor -o /etc/apt/keyrings/jmt-labs.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/jmt-labs.gpg] https://jmt-labs.github.io/apt stable main" \
    > /etc/apt/sources.list.d/jmt-labs.list \
    && apt-get update \
    && apt-get install -y forgecrate \
    && rm -rf /var/lib/apt/lists/*

# Claude Code
RUN npm install -g @anthropic-ai/claude-code

# Controller Binary aus Stage 1
COPY --from=controller-build /issue-controller /usr/local/bin/issue-controller

# entrypoint
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/issue-controller

WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
```

- [ ] **Step 7.2: Build-Kontext prüfen — Dockerfile braucht den controller/-Ordner**

Der Buildkontext muss das Repo-Root sein (nicht agent/):

```bash
docker build -f agent/Dockerfile -t claude-agent-test:local .
```
Expected: Build erfolgreich

- [ ] **Step 7.3: Binaries im Image prüfen**

```bash
docker run --rm claude-agent-test:local forgecrate --version
docker run --rm claude-agent-test:local /usr/local/bin/issue-controller --help 2>&1 | head -3 || true
docker run --rm claude-agent-test:local claude --version
```
Expected: alle drei Binaries antworten

- [ ] **Step 7.4: Commit**

```bash
git add agent/Dockerfile && git commit -m "feat(agent): add multi-stage Dockerfile"
```

---

### Task 8: entrypoint.sh mit Watchdog

**Files:**
- Create: `agent/entrypoint.sh`

- [ ] **Step 8.1: entrypoint.sh schreiben** — `agent/entrypoint.sh`

```bash
#!/bin/bash
set -euo pipefail

CLAUDE_LOG=/tmp/claude.log
WORKSPACE=/workspace
WATCHDOG_INTERVAL=60     # Sekunden zwischen Watchdog-Prüfungen
WATCHDOG_IDLE_ROUNDS=10  # Runden ohne Output = ~10 Min -> "needs input"
NOTIFIED_IDLE=false
SHARE_URL=""

ntfy_send() {
  local title="$1" body="$2" priority="${3:-default}"
  local auth_args=()
  if [ -n "${NTFY_AUTH_TOKEN:-}" ]; then
    auth_args=(-H "Authorization: Bearer ${NTFY_AUTH_TOKEN}")
  fi
  curl -s -d "$body" \
    -H "Title: $title" \
    -H "Priority: $priority" \
    "${auth_args[@]}" \
    "${NTFY_URL}/${NTFY_TOPIC}" || {
    echo "WARNUNG: ntfy-Notification fehlgeschlagen" >&2
    true
  }
}

extract_share_url() {
  grep -o 'https://claude\.ai/[^ "]*' "$CLAUDE_LOG" 2>/dev/null \
    | grep -v 'accounts' | head -1 || true
}

cd "$WORKSPACE"

echo "=== Agent startet: Issue #${ISSUE_NUMBER}: ${ISSUE_TITLE} ==="
echo "=== Repo: ${TARGET_REPO} ==="

# Claude Code starten
claude \
  --dangerously-skip-permissions \
  --share \
  --message "$(cat .agent-prompt)" \
  2>&1 | tee "$CLAUDE_LOG" &

CLAUDE_PID=$!

# Share-URL abwarten (max 60 Sekunden)
for i in $(seq 1 30); do
  SHARE_URL=$(extract_share_url)
  if [ -n "$SHARE_URL" ]; then
    echo "Share-URL: $SHARE_URL"
    break
  fi
  sleep 2
done

# Start-Notification
ISSUE_HEADER=$(head -1 .agent-prompt 2>/dev/null || echo "Issue #${ISSUE_NUMBER}")
if [ -n "$SHARE_URL" ]; then
  ntfy_send "Agent laeuft: Issue #${ISSUE_NUMBER}" \
    "${ISSUE_HEADER}
Session: ${SHARE_URL}" "default"
else
  ntfy_send "Agent laeuft: Issue #${ISSUE_NUMBER}" \
    "${ISSUE_HEADER}
(Kein Share-Link verfuegbar)" "default"
fi

# Watchdog
LAST_SIZE=0
IDLE_ROUNDS=0

while kill -0 $CLAUDE_PID 2>/dev/null; do
  sleep "$WATCHDOG_INTERVAL"
  CURRENT_SIZE=$(wc -c < "$CLAUDE_LOG" 2>/dev/null || echo 0)

  if [ "$CURRENT_SIZE" -eq "$LAST_SIZE" ]; then
    IDLE_ROUNDS=$((IDLE_ROUNDS + 1))
    if [ "$IDLE_ROUNDS" -ge "$WATCHDOG_IDLE_ROUNDS" ] && [ "$NOTIFIED_IDLE" = "false" ]; then
      echo "Watchdog: Agent idle seit ${WATCHDOG_IDLE_ROUNDS} Runden"
      ntfy_send "Eingabe noetig: Issue #${ISSUE_NUMBER}" \
        "Agent wartet auf Eingabe.
Session: ${SHARE_URL:-kein Link}" "high"
      NOTIFIED_IDLE=true
    fi
  else
    IDLE_ROUNDS=0
    NOTIFIED_IDLE=false
    LAST_SIZE=$CURRENT_SIZE
  fi
done

wait $CLAUDE_PID
EXIT_CODE=$?

if [ $EXIT_CODE -eq 0 ]; then
  PR_URL=$(grep -o 'https://github\.com/[^ "]*pull[^ "]*' "$CLAUDE_LOG" | tail -1 || true)
  BODY="${PR_URL:-Issue #${ISSUE_NUMBER} abgeschlossen (kein PR-Link im Log)}"
  ntfy_send "Abgeschlossen: Issue #${ISSUE_NUMBER}" "$BODY" "default"
  echo "=== Agent erfolgreich ==="
else
  LAST_LINES=$(tail -15 "$CLAUDE_LOG" 2>/dev/null || echo "Kein Log")
  ntfy_send "Fehler: Issue #${ISSUE_NUMBER}" \
    "Exit-Code: ${EXIT_CODE}
${LAST_LINES}" "urgent"
  echo "=== Agent fehlgeschlagen: Exit ${EXIT_CODE} ===" >&2
  exit $EXIT_CODE
fi
```

- [ ] **Step 8.2: Syntax prüfen**

```bash
bash -n agent/entrypoint.sh && echo "Syntax OK"
```
Expected: `Syntax OK`

- [ ] **Step 8.3: Image neu bauen**

```bash
docker build -f agent/Dockerfile -t claude-agent-test:local .
```
Expected: erfolgreich

- [ ] **Step 8.4: Commit**

```bash
git add agent/entrypoint.sh && git commit -m "feat(agent): add entrypoint with watchdog and ntfy notifications"
```

---

### Task 9: GitHub Actions CI

**Files:**
- Create: `.github/workflows/build-push.yml`

- [ ] **Step 9.1: Workflow schreiben** — `.github/workflows/build-push.yml`

```yaml
name: Build and Push Agent Image

on:
  push:
    branches: [main]
    paths:
      - "agent/**"
      - "controller/**"
  workflow_dispatch:

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository_owner }}/claude-agent

jobs:
  build-push:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Log in to GitHub Container Registry
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          tags: |
            type=sha,prefix=sha-
            type=raw,value=latest,enable={{is_default_branch}}

      - name: Build and push
        uses: docker/build-push-action@v5
        with:
          context: .
          file: ./agent/Dockerfile
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

- [ ] **Step 9.2: YAML validieren**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/build-push.yml'))" && echo "YAML valid"
```
Expected: `YAML valid`

- [ ] **Step 9.3: Commit und Push — CI startet**

```bash
git add .github/workflows/build-push.yml && git commit -m "ci: add GitHub Actions workflow to build and push agent image"
git push origin main
```
Expected: CI-Run auf GitHub → `ghcr.io/erlenfrosch/claude-agent:latest` wird gepusht.
Prüfen: `https://github.com/erlenfrosch/claude-k8s-agent/actions`

---

## Phase 3: Kubernetes Manifeste

### Task 10: Namespace, RBAC, ConfigMap

**Files:**
- Create: `deploy/namespace.yaml`
- Create: `deploy/rbac.yaml`
- Create: `deploy/configmap.yaml`

- [ ] **Step 10.1: namespace.yaml**

```yaml
# deploy/namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: claude-agent
  labels:
    app.kubernetes.io/managed-by: argocd
```

- [ ] **Step 10.2: rbac.yaml**

```yaml
# deploy/rbac.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: issue-controller
  namespace: claude-agent
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: issue-controller
  namespace: claude-agent
rules:
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["create", "get", "list", "watch", "delete"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: issue-controller
  namespace: claude-agent
subjects:
  - kind: ServiceAccount
    name: issue-controller
    namespace: claude-agent
roleRef:
  kind: Role
  name: issue-controller
  apiGroup: rbac.authorization.k8s.io
```

- [ ] **Step 10.3: configmap.yaml**

`DEINE_DOMAIN` ersetzen durch die tatsächliche Domain.

```yaml
# deploy/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: claude-agent-config
  namespace: claude-agent
data:
  MAX_PODS: "4"
  LABEL_FILTER: "agent-backlog"
  DEFAULT_PROFILE: "backend"
  DEFAULT_FLAVORS: "github,gitops"
  NTFY_URL: "https://ntfy.DEINE_DOMAIN"
  NTFY_TOPIC: "claude-agent"
  AGENT_IMAGE: "ghcr.io/erlenfrosch/claude-agent:latest"
  NAMESPACE: "claude-agent"
  SECRET_NAME: "claude-agent-secrets"
```

- [ ] **Step 10.4: Commit**

```bash
git add deploy/namespace.yaml deploy/rbac.yaml deploy/configmap.yaml
git commit -m "feat(deploy): add namespace, rbac, and configmap manifests"
```

---

### Task 11: ntfy.sh Manifeste

**Files:**
- Create: `deploy/ntfy/pvc.yaml`
- Create: `deploy/ntfy/deployment.yaml`
- Create: `deploy/ntfy/service.yaml`
- Create: `deploy/ntfy/ingress.yaml`

- [ ] **Step 11.1: pvc.yaml**

```yaml
# deploy/ntfy/pvc.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ntfy-cache
  namespace: claude-agent
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Gi
```

- [ ] **Step 11.2: deployment.yaml**

`DEINE_DOMAIN` ersetzen.

```yaml
# deploy/ntfy/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ntfy
  namespace: claude-agent
  labels:
    app: ntfy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ntfy
  template:
    metadata:
      labels:
        app: ntfy
    spec:
      containers:
        - name: ntfy
          image: binwiederhier/ntfy:latest
          args: ["serve"]
          ports:
            - containerPort: 80
          env:
            - name: NTFY_BASE_URL
              value: "https://ntfy.DEINE_DOMAIN"
            - name: NTFY_CACHE_FILE
              value: "/var/cache/ntfy/cache.db"
            - name: NTFY_AUTH_DEFAULT_ACCESS
              value: "deny-all"
            - name: NTFY_BEHIND_PROXY
              value: "true"
          volumeMounts:
            - name: cache
              mountPath: /var/cache/ntfy
          resources:
            requests:
              memory: "64Mi"
              cpu: "50m"
            limits:
              memory: "256Mi"
              cpu: "200m"
      volumes:
        - name: cache
          persistentVolumeClaim:
            claimName: ntfy-cache
```

- [ ] **Step 11.3: service.yaml**

```yaml
# deploy/ntfy/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: ntfy
  namespace: claude-agent
spec:
  selector:
    app: ntfy
  ports:
    - port: 80
      targetPort: 80
```

- [ ] **Step 11.4: ingress.yaml**

`DEINE_DOMAIN` ersetzen. `cert-manager.io/cluster-issuer` an bestehende Cluster-Issuer anpassen (z.B. `letsencrypt-prod`).

```yaml
# deploy/ntfy/ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ntfy
  namespace: claude-agent
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
    nginx.ingress.kubernetes.io/proxy-body-size: "0"
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - ntfy.DEINE_DOMAIN
      secretName: ntfy-tls
  rules:
    - host: ntfy.DEINE_DOMAIN
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: ntfy
                port:
                  number: 80
```

- [ ] **Step 11.5: Commit**

```bash
git add deploy/ntfy/ && git commit -m "feat(deploy): add ntfy.sh Kubernetes manifests"
```

---

### Task 12: CronJob Manifest + SealedSecret-Vorlage

**Files:**
- Create: `deploy/controller/cronjob.yaml`
- Create: `deploy/sealed-secret.example.yaml`
- Create: `.gitignore`

- [ ] **Step 12.1: cronjob.yaml schreiben**

```yaml
# deploy/controller/cronjob.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: issue-controller
  namespace: claude-agent
spec:
  schedule: "*/10 * * * *"
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 5
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: issue-controller
          restartPolicy: OnFailure
          containers:
            - name: controller
              image: ghcr.io/erlenfrosch/claude-agent:latest
              command: ["/usr/local/bin/issue-controller"]
              envFrom:
                - configMapRef:
                    name: claude-agent-config
              env:
                - name: GITHUB_TOKEN
                  valueFrom:
                    secretKeyRef:
                      name: claude-agent-secrets
                      key: GITHUB_TOKEN
                - name: TARGET_REPO
                  valueFrom:
                    configMapKeyRef:
                      name: claude-agent-config
                      key: TARGET_REPO
              resources:
                requests:
                  memory: "64Mi"
                  cpu: "50m"
                limits:
                  memory: "256Mi"
                  cpu: "500m"
```

- [ ] **Step 12.2: sealed-secret.example.yaml schreiben**

```yaml
# deploy/sealed-secret.example.yaml
# VORLAGE — NICHT mit echten Werten committen!
#
# Anleitung:
# 1. /tmp/secret.yaml anlegen (NICHT committen):
#    ---
#    apiVersion: v1
#    kind: Secret
#    metadata:
#      name: claude-agent-secrets
#      namespace: claude-agent
#    stringData:
#      ANTHROPIC_API_KEY: "sk-ant-..."
#      GITHUB_TOKEN: "ghp_..."     # PAT Scopes: repo + issues
#      NTFY_AUTH_TOKEN: "..."
#
# 2. Verschluesseln:
#    kubeseal --namespace claude-agent --name claude-agent-secrets \
#      < /tmp/secret.yaml > deploy/sealed-secret.yaml
#
# 3. /tmp/secret.yaml loeschen: rm /tmp/secret.yaml
# 4. deploy/sealed-secret.yaml committen (nur verschluesselte Werte)
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: claude-agent-secrets
  namespace: claude-agent
spec:
  encryptedData:
    ANTHROPIC_API_KEY: REPLACE_WITH_KUBESEAL_OUTPUT
    GITHUB_TOKEN: REPLACE_WITH_KUBESEAL_OUTPUT
    NTFY_AUTH_TOKEN: REPLACE_WITH_KUBESEAL_OUTPUT
```

- [ ] **Step 12.3: .gitignore anlegen**

```gitignore
# .gitignore

# Echte Secrets niemals committen
deploy/sealed-secret.yaml
/tmp/secret.yaml

# Go build artifacts
controller/bin/

# OS / Editor
.DS_Store
.idea/
.vscode/
*.swp
```

- [ ] **Step 12.4: Commit**

```bash
git add deploy/controller/ deploy/sealed-secret.example.yaml .gitignore
git commit -m "feat(deploy): add CronJob, sealed-secret template, and .gitignore"
```

---

## Phase 4: GitOps & Abschluss

### Task 13: ArgoCD Application + Per-Repo Vorlage

**Files:**
- Create: `deploy/argocd/application.yaml`
- Create: `.claude-agent.yaml.example`
- Modify: `README.md`

- [ ] **Step 13.1: application.yaml schreiben**

Das Manifest deployt alles aus `deploy/` dieses Repos direkt ins Homelab.

```yaml
# deploy/argocd/application.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: claude-agent
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  source:
    repoURL: https://github.com/erlenfrosch/claude-k8s-agent
    targetRevision: main
    path: deploy
  destination:
    server: https://kubernetes.default.svc
    namespace: claude-agent
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true
```

- [ ] **Step 13.2: .claude-agent.yaml.example schreiben**

```yaml
# .claude-agent.yaml
# Diese Datei ins Root-Verzeichnis jedes Ziel-Repos legen.
# Steuert forgecrate-Profil und Flavors fuer den autonomen Agenten.
# Fehlt die Datei: ConfigMap-Defaults gelten (profile: backend, flavors: github,gitops).

forgecrate:
  # Verfuegbare Profile: backend | frontend | fullstack
  profile: backend

  # Verfuegbare Flavors: tdd | strict-review | github | gitops | getbetter | minimal | no-research
  flavors:
    - github
    - gitops
    # Weitere nach Bedarf:
    # - tdd
    # - strict-review
```

- [ ] **Step 13.3: README.md aktualisieren**

README.md ersetzen mit:

```markdown
# claude-k8s-agent

Autonomer Claude Code Agent fuer Kubernetes (k3s / Homelab).

Arbeitet GitHub Issues selbststaendig ab, oeffnet Pull Requests und sendet
Push-Benachrichtigungen via ntfy.sh. Jederzeit per Claude.ai App (Mobile /
Browser) in die laufende Session eingreifbar.

## Wie es funktioniert

1. CronJob prueft alle 10 Minuten Issues mit Label `agent-backlog`
2. Pro Issue wird ein K8s Job gespawnt (max. konfigurierbar, Standard: 4)
3. Job: `git clone` + `forgecrate init` + `claude --share --dangerously-skip-permissions`
4. Agent arbeitet autonom: Tests, Implementierung, PR oeffnen
5. Push-Notification via ntfy.sh mit Claude.ai Session-URL
6. Tap auf Notification -> Claude.ai App -> Session interaktiv fortfuehren

## Schnellstart

**Voraussetzungen:** k3s + ArgoCD + SealedSecrets, GitHub PAT (repo + issues),
Anthropic API Key, Domain mit TLS.

### 1. Secrets verschluesseln und committen

```bash
# /tmp/secret.yaml anlegen (NICHT committen):
cat > /tmp/secret.yaml << EOF
apiVersion: v1
kind: Secret
metadata:
  name: claude-agent-secrets
  namespace: claude-agent
stringData:
  ANTHROPIC_API_KEY: "sk-ant-..."
  GITHUB_TOKEN: "ghp_..."
  NTFY_AUTH_TOKEN: "dein-ntfy-token"
EOF

kubeseal --namespace claude-agent --name claude-agent-secrets \
  < /tmp/secret.yaml > deploy/sealed-secret.yaml
rm /tmp/secret.yaml
git add deploy/sealed-secret.yaml && git commit -m "feat: add sealed secrets"
git push origin main
```

### 2. Konfiguration anpassen

- `deploy/configmap.yaml`: `NTFY_URL` und `TARGET_REPO` setzen
- `deploy/ntfy/deployment.yaml` + `ingress.yaml`: `DEINE_DOMAIN` ersetzen

### 3. ArgoCD Application deployen

```bash
kubectl apply -f deploy/argocd/application.yaml
```

ArgoCD synct automatisch alle Manifeste aus `deploy/`.

### 4. Erstes Issue anlegen

Im Ziel-Repo ein GitHub Issue anlegen und Label `agent-backlog` setzen.
Innerhalb von 10 Minuten startet der Agent automatisch.

## Per-Repo Konfiguration

`.claude-agent.yaml` im Root des Ziel-Repos anlegen:

```yaml
forgecrate:
  profile: fullstack
  flavors:
    - tdd
    - github
    - gitops
```

## Dokumentation

- [Design-Spec](docs/superpowers/specs/2026-05-24-autonomous-claude-k8s-design.md)
- [Implementierungsplan](docs/superpowers/plans/2026-05-24-autonomous-claude-k8s-agent.md)
```

- [ ] **Step 13.4: Alle Tests ein letztes Mal**

```bash
cd controller && go test ./... -v
```
Expected: alle Tests PASS

- [ ] **Step 13.5: Finaler Commit und Push**

```bash
git add deploy/argocd/ .claude-agent.yaml.example README.md
git commit -m "feat: finalize ArgoCD manifest, per-repo config example, and README"
git push origin main
```

---

## Checkliste vor dem ersten Deploy

- [ ] `deploy/configmap.yaml`: `NTFY_URL` und `TARGET_REPO` angepasst
- [ ] `deploy/ntfy/deployment.yaml` + `ingress.yaml`: Domain angepasst
- [ ] `deploy/sealed-secret.yaml` via kubeseal erstellt und committed
- [ ] GitHub PAT Scopes: `repo` + `issues`
- [ ] CI-Run erfolgreich: Image liegt auf `ghcr.io/erlenfrosch/claude-agent:latest`
- [ ] ArgoCD Application ist gruen (alle Ressourcen Healthy/Synced)
- [ ] ntfy-App auf Handy installiert, Topic `claude-agent` mit Auth-Token abonniert
- [ ] Test-Issue mit Label `agent-backlog` im Ziel-Repo erstellt
- [ ] Nach max. 10 Min: Push-Notification auf Handy mit Session-URL
