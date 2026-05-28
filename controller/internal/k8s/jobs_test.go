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
		// Job ohne app=claude-agent, aber Completed (Active=0): testet, dass inaktive Jobs
		// nicht gezählt werden. Label-Selector-Filterung kann mit fake.SimpleClientset nicht
		// getestet werden (ignoriert LabelSelector); dies bleibt einem Integrations-Test vorbehalten.
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name: "other-completed-job", Namespace: "claude-agent",
				Labels: map[string]string{"app": "other"},
			},
			Status: batchv1.JobStatus{Active: 0, Succeeded: 1},
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
		GotifyURL: "http://gotify.gotify.svc.cluster.local",
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
