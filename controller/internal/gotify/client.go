package gotify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// message ist das JSON-Payload für die Gotify-API.
type message struct {
	Title    string `json:"title"`
	Message  string `json:"message"`
	Priority int    `json:"priority"`
}

// Client sendet Push-Benachrichtigungen an einen Gotify-Server.
type Client struct {
	baseURL    string
	appToken   string
	httpClient *http.Client
}

// NewClient erstellt einen Gotify-Client.
func NewClient(baseURL, appToken string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		appToken:   appToken,
		httpClient: &http.Client{},
	}
}

// priorityToInt mappt ntfy-artige Prioritäts-Strings auf Gotify-Integer-Prioritäten.
func priorityToInt(p string) int {
	switch p {
	case "urgent":
		return 9
	case "high":
		return 7
	default: // "default", ""
		return 5
	}
}

// Send sendet eine Benachrichtigung an den Gotify-Server.
// Implementiert das NtfyClient-Interface des Controllers.
func (c *Client) Send(title, body, priority string) error {
	payload, err := json.Marshal(message{
		Title:    title,
		Message:  body,
		Priority: priorityToInt(priority),
	})
	if err != nil {
		return fmt.Errorf("gotify payload marshallen: %w", err)
	}
	url := fmt.Sprintf("%s/message?token=%s", c.baseURL, c.appToken)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("request erstellen: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gotify request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gotify antwortete mit %d", resp.StatusCode)
	}
	return nil
}
