package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	apiKey string
	from   string
	http   *http.Client
}

func NewClient(apiKey, from string) *Client {
	return &Client{apiKey: apiKey, from: from, http: &http.Client{Timeout: 10 * time.Second}}
}

// Enabled is nil-safe, so handlers can call it without checking for nil.
func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != "" && c.from != ""
}

func (c *Client) Send(to, subject, htmlBody string) error {
	if !c.Enabled() {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{
		"from": c.from, "to": []string{to}, "subject": subject, "html": htmlBody,
	})
	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resend: %s: %s", resp.Status, body)
	}
	return nil
}