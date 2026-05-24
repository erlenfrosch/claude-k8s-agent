package github

import (
	"context"
	"fmt"
	"net/url"
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
	return &Client{gh: gogithub.NewClient(tc).WithAuthToken(token)}
}

// NewClientWithBaseURL erstellt einen Client mit angepasster Base-URL (für Tests).
func NewClientWithBaseURL(token, baseURL string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	c := gogithub.NewClient(tc).WithAuthToken(token)
	parsed, err := url.Parse(baseURL)
	if err != nil {
		panic(fmt.Sprintf("ungültige Base-URL %q: %v", baseURL, err))
	}
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	c.BaseURL = parsed
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
