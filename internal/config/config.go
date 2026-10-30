package config

import (
	"os"
	"strings"
)

// Config holds everything the app needs to boot. All values come from
// env vars so the same binary runs locally (sqlite file) and on
// Turso in prod, without a rebuild.
type Config struct {
	Port         string
	DatabaseURL  string // e.g. "file:local.db" for dev, or "libsql://<db>.turso.io"
	DatabaseAuth string // Turso auth token; empty for local sqlite
	JWTSecret    string
	Env          string // "dev" | "prod"
	AppBaseURL   string // public URL of the site, no trailing slash; used in emails and as the Google redirect

	// Google sign-in (OAuth 2.0). Leave blank to hide the Google buttons.
	GoogleClientID     string
	GoogleClientSecret string

	// Anyone can self-register as a mentee. Mentor self-registration needs
	// this code; if it is empty, mentor sign-up is switched off entirely.
	MentorSignupCode string

	// Protects the JSON endpoint that can create ANY role (admin, academy_head).
	// Send it as the X-Setup-Key header. Empty = that endpoint is disabled.
	SetupKey string
}

// Email settings (GMAIL_*) are read directly by notify.NewClientFromEnv.
func Load() Config {
	return Config{
		Port:         getEnv("PORT", "8080"),
		DatabaseURL:  getEnv("DATABASE_URL", "file:local.db"),
		DatabaseAuth: getEnv("DATABASE_AUTH_TOKEN", ""),
		JWTSecret:    getEnv("JWT_SECRET", "dev-secret-change-me"),
		Env:          getEnv("APP_ENV", "dev"),
		AppBaseURL:   strings.TrimRight(getEnv("APP_BASE_URL", "http://localhost:8080"), "/"),

		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", ""),

		MentorSignupCode: getEnv("MENTOR_SIGNUP_CODE", ""),
		SetupKey:         getEnv("SETUP_KEY", ""),
	}
}

// getEnv returns the env var if it is set and non-empty, otherwise the fallback.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}