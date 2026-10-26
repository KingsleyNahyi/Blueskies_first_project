package notify

import (
	"fmt"
	"net/smtp"
)

type Client struct {
	password string // Holds your Gmail App Password
	from     string // Holds your Gmail address
}

func NewClient(password, from string) *Client {
	return &Client{password: password, from: from}
}

// Enabled is nil-safe, so handlers can call it without checking for nil.
func (c *Client) Enabled() bool {
	return c != nil && c.password != "" && c.from != ""
}

func (c *Client) Send(to, subject, htmlBody string) error {
	if !c.Enabled() {
		return nil
	}

	smtpHost := "smtp.gmail.com"
	smtpPort := "587"

	auth := smtp.PlainAuth("", c.from, c.password, smtpHost)

	// Construct email headers with HTML support
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n"
	subjectHeader := fmt.Sprintf("Subject: %s\n", subject)
	fromHeader := fmt.Sprintf("From: %s\n", c.from)
	toHeader := fmt.Sprintf("To: %s\n", to)

	msg := []byte(fromHeader + toHeader + subjectHeader + mime + "\n" + htmlBody)

	// Send email via Gmail SMTP server
	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, c.from, []string{to}, msg)
	if err != nil {
		return fmt.Errorf("smtp send error: %w", err)
	}
	
	return nil
}