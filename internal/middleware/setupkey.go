package middleware

import (
	"crypto/subtle"
	"net/http"
)

// RequireSetupKey guards endpoints that can create privileged accounts.
// The caller must send the X-Setup-Key header matching the SETUP_KEY env
// var. If SETUP_KEY is empty the endpoint is disabled entirely.
func RequireSetupKey(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("X-Setup-Key")
			if key == "" || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}