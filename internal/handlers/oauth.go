package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bluesbirds/academy/internal/middleware"
	"github.com/bluesbirds/academy/internal/models"
)

// Google OAuth 2.0 "authorization code" flow, written against plain
// net/http (no extra dependency). The shape of it:
//
//   1. /auth/google/login or /auth/google/signup  -> redirect to Google
//   2. Google asks the user to pick an account and approve
//   3. Google redirects back to /auth/google/callback?code=...&state=...
//   4. We swap the code for an access token (server to server), ask Google
//      who the user is, then log them in (or create them) and set the same
//      session cookie the email/password login uses.

const (
	googleAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL    = "https://oauth2.googleapis.com/token"
	googleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"
	oauthCookieName   = "oauth_state"
)

func (h *PageHandler) googleEnabled() bool {
	return h.GoogleClientID != "" && h.GoogleClientSecret != ""
}

func (h *PageHandler) googleRedirectURI() string {
	return strings.TrimRight(h.AppBaseURL, "/") + "/auth/google/callback"
}

// GoogleLogin starts the flow for someone who already has an account.
func (h *PageHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	h.startGoogle(w, r, "login", "")
}

// GoogleSignup starts the flow for a new account. The role (and the mentor
// code, if mentor) are validated BEFORE leaving for Google, then carried
// through the round trip inside a signed cookie.
func (h *PageHandler) GoogleSignup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	role, errMsg := h.validateSignupRole(r.FormValue("role"), r.FormValue("code"))
	if errMsg != "" {
		view := h.authPage("Sign up", errMsg)
		if r.FormValue("role") == string(models.RoleMentor) {
			view.Role = r.FormValue("role")
		}
		h.Templates["signup"].ExecuteTemplate(w, "base", view)
		return
	}
	h.startGoogle(w, r, "signup", string(role))
}

func (h *PageHandler) startGoogle(w http.ResponseWriter, r *http.Request, mode, role string) {
	if !h.googleEnabled() {
		http.Redirect(w, r, "/login?e=google_off", http.StatusSeeOther)
		return
	}

	nonce := randomHex(16)
	h.setOAuthCookie(w, nonce+"|"+mode+"|"+role)

	q := url.Values{}
	q.Set("client_id", h.GoogleClientID)
	q.Set("redirect_uri", h.googleRedirectURI())
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", nonce)
	q.Set("prompt", "select_account")
	http.Redirect(w, r, googleAuthURL+"?"+q.Encode(), http.StatusFound)
}

// GoogleCallback is where Google sends the browser back.
func (h *PageHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	payload, ok := h.readOAuthCookie(r)
	h.clearOAuthCookie(w)
	parts := strings.Split(payload, "|")
	if !ok || len(parts) != 3 {
		http.Redirect(w, r, "/login?e=google_failed", http.StatusSeeOther)
		return
	}
	nonce, mode, role := parts[0], parts[1], parts[2]

	// CSRF check: the state Google echoes back must match the nonce we stored.
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(nonce)) != 1 {
		http.Redirect(w, r, "/login?e=google_failed", http.StatusSeeOther)
		return
	}
	code := r.URL.Query().Get("code")
	if r.URL.Query().Get("error") != "" || code == "" {
		http.Redirect(w, r, "/login?e=google_failed", http.StatusSeeOther)
		return
	}

	info, err := h.fetchGoogleUser(code)
	if err != nil {
		log.Printf("google oauth: %v", err)
		http.Redirect(w, r, "/login?e=google_failed", http.StatusSeeOther)
		return
	}
	if info.Email == "" || !info.EmailVerified {
		http.Redirect(w, r, "/login?e=google_unverified", http.StatusSeeOther)
		return
	}
	email := strings.ToLower(strings.TrimSpace(info.Email))

	var userID string
	var userRole models.Role
	err = h.DB.QueryRow(`SELECT id, role FROM users WHERE lower(email) = ?`, email).Scan(&userID, &userRole)
	switch {
	case err == nil:
		// Existing account: just log in. Google has verified they own this email.
	case err == sql.ErrNoRows && mode == "signup" && (role == string(models.RoleMentee) || role == string(models.RoleMentor)):
		name := strings.TrimSpace(info.Name)
		if name == "" {
			name = strings.Split(email, "@")[0]
		}
		userID = uuid.NewString()
		userRole = models.Role(role)
		// password_hash is left empty: Google-created accounts have no password,
		// and bcrypt can never match an empty hash, so password login stays closed.
		if _, err := h.DB.Exec(
			`INSERT INTO users (id, email, password_hash, name, role) VALUES (?, ?, '', ?, ?)`,
			userID, email, name, string(userRole),
		); err != nil {
			log.Printf("google oauth: creating user: %v", err)
			http.Redirect(w, r, "/login?e=google_failed", http.StatusSeeOther)
			return
		}
	case err == sql.ErrNoRows:
		http.Redirect(w, r, "/signup?e=no_account", http.StatusSeeOther)
		return
	default:
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	token, err := middleware.IssueToken(h.JWTSecret, userID, userRole)
	if err != nil {
		http.Error(w, "could not issue session", http.StatusInternalServerError)
		return
	}
	middleware.SetSessionCookie(w, token)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

type googleUser struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// fetchGoogleUser swaps the one-time code for an access token, then asks
// Google's userinfo endpoint who that token belongs to.
func (h *PageHandler) fetchGoogleUser(code string) (*googleUser, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", h.GoogleClientID)
	form.Set("client_secret", h.GoogleClientSecret)
	form.Set("redirect_uri", h.googleRedirectURI())
	form.Set("grant_type", "authorization_code")

	resp, err := client.PostForm(googleTokenURL, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, &oauthError{"token exchange failed: " + resp.Status + " " + string(body)}
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return nil, &oauthError{"token response had no access_token"}
	}

	req, _ := http.NewRequest(http.MethodGet, googleUserInfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	uresp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer uresp.Body.Close()
	ubody, _ := io.ReadAll(io.LimitReader(uresp.Body, 1<<20))
	if uresp.StatusCode != http.StatusOK {
		return nil, &oauthError{"userinfo failed: " + uresp.Status}
	}
	var info googleUser
	if err := json.Unmarshal(ubody, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

type oauthError struct{ msg string }

func (e *oauthError) Error() string { return e.msg }

// --- signed state cookie ---
//
// The cookie carries "nonce|mode|role". It is HMAC-signed with the server's
// secret so the browser can't edit it, which matters: without the signature
// someone could change role=mentee to role=mentor on the way back from Google.

func (h *PageHandler) sign(payload string) string {
	mac := hmac.New(sha256.New, []byte(h.JWTSecret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *PageHandler) setOAuthCookie(w http.ResponseWriter, payload string) {
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + h.sign(payload)
	http.SetCookie(w, &http.Cookie{
		Name:     oauthCookieName,
		Value:    value,
		Path:     "/auth/google",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   strings.HasPrefix(h.AppBaseURL, "https://"),
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *PageHandler) readOAuthCookie(r *http.Request) (string, bool) {
	c, err := r.Cookie(oauthCookieName)
	if err != nil {
		return "", false
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	payload := string(raw)
	if !hmac.Equal([]byte(parts[1]), []byte(h.sign(payload))) {
		return "", false
	}
	return payload, true
}

func (h *PageHandler) clearOAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: oauthCookieName, Value: "", Path: "/auth/google", MaxAge: -1, HttpOnly: true})
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}