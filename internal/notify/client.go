package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Client struct {
	apiKey     string
	senderEmail string
	senderName  string
	http        *http.Client
}

func NewClientFromEnv() *Client {
	return &Client{
		apiKey:      strings.TrimSpace(os.Getenv("BREVO_API_KEY")),
		senderEmail: strings.TrimSpace(os.Getenv("BREVO_SENDER_EMAIL")),
		senderName:  firstEnv("BREVO_SENDER_NAME", "SMTP_FROM_NAME", "Mentorship Platform"),
		http:        &http.Client{Timeout: 10 * time.Second},
	}
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != "" && c.senderEmail != ""
}

func (c *Client) Send(to, subject, htmlBody string) error {
	if !c.Enabled() {
		return nil
	}

	payload := map[string]interface{}{
		"sender": map[string]string{
			"name":  c.senderName,
			"email": c.senderEmail,
		},
		"to": []map[string]string{
			{"email": to},
		},
		"subject":     subject,
		"htmlContent": htmlBody,
	}

	bodyBytes, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://api.brevo.com/v3/smtp/email", bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}

	req.Header.Set("api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		log.Printf("brevo api request failed to %s: %v", to, err)
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
	if resp.StatusCode >= 300 {
		err := fmt.Errorf("brevo api returned %d: %s", resp.StatusCode, string(respBody))
		log.Printf("email to %s failed: %v", to, err)
		return err
	}

	return nil
}