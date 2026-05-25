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
