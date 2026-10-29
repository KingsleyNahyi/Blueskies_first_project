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
	apiKey      string
	senderEmail string
	senderName  string
	http        *http.Client
}

func NewClientFromEnv() *Client {
	c := &Client{
		apiKey:      strings.TrimSpace(os.Getenv("BREVO_API_KEY")),
		senderEmail: strings.TrimSpace(os.Getenv("BREVO_SENDER_EMAIL")),
		senderName:  firstEnv("BREVO_SENDER_NAME", "SMTP_FROM_NAME"),
		http:        &http.Client{Timeout: 10 * time.Second},
	}

	// Proper fallback: used only when no sender name env var is set.
	if c.senderName == "" {
		c.senderName = "Mentorship Platform"
	}

	// One-time startup log so you can see in Render whether config was picked up.
	log.Printf("notify client init: apiKey set=%t, senderEmail set=%t, senderName=%q, enabled=%t",
		c.apiKey != "", c.senderEmail != "", c.senderName, c.Enabled())

	return c
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
		if c == nil {
			log.Printf("email to %s skipped: notify client is nil", to)
		} else {
			log.Printf("email to %s skipped: notify client disabled (apiKey set: %t, senderEmail set: %t)",
				to, c.apiKey != "", c.senderEmail != "")
		}
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

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("email to %s failed: could not encode payload: %v", to, err)
		return err
	}

	req, err := http.NewRequest("POST", "https://api.brevo.com/v3/smtp/email", bytes.NewReader(bodyBytes))
	if err != nil {
		log.Printf("email to %s failed: could not build request: %v", to, err)
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

	log.Printf("email to %s accepted by brevo (status %d): %s", to, resp.StatusCode, string(respBody))
	return nil
}