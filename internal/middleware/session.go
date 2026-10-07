package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SessionCookieName is the cookie the page routes read the JWT from.
// The existing JSON API still uses the Authorization: Bearer header
// (RequireAuth, in auth.go) — this is a second, parallel way to carry
// the same kind of token, suited to full-page form posts instead of
// JS fetch calls.
const SessionCookieName = "session"

// SetSessionCookie issues the signed JWT as an httpOnly cookie after a
// successful form login. httpOnly means client-side JS can't read it
// (mitigates XSS token theft); in a real deploy behind HTTPS you'd also
// add Secure: true.
func SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(7 * 24 * time.Hour), // matches IssueToken's expiry in auth.go
	})
}

// ClearSessionCookie logs the user out by expiring the cookie immediately.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// RequireAuthCookie is RequireAuth's counterpart for page routes: same
// verification logic, different place to find the token. A page route
// that fails this redirects to /login instead of returning a 401 JSON
// body, since a browser following a link expects a page, not an error
// blob.
func RequireAuthCookie(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil || cookie.Value == "" {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			claims := &Claims{}
			token, err := jwt.ParseWithClaims(cookie.Value, claims, func(t *jwt.Token) (interface{}, error) {
				return []byte(secret), nil
			})
			if err != nil || !token.Valid {
				ClearSessionCookie(w)
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			ctx := context.WithValue(r.Context(), ctxUserID, claims.UserID)
			ctx = context.WithValue(ctx, ctxRole, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
