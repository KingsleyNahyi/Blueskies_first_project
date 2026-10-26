package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/bluesbirds/academy/internal/middleware"
	"github.com/bluesbirds/academy/internal/models"
)

type AuthHandler struct {
	DB        *sql.DB
	JWTSecret string
}

type registerRequest struct {
	Email    string      `json:"email"`
	Password string      `json:"password"`
	Name     string      `json:"name"`
	Role     models.Role `json:"role"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string      `json:"token"`
	User  models.User `json:"user"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var email, password, name string
	var role models.Role

	// Check if it's a standard HTML form submission or JSON
	if r.Header.Get("Content-Type") == "application/json" {
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		email, password, name, role = req.Email, req.Password, req.Name, req.Role
	} else {
		// Parse standard HTML form post data
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form data")
			return
		}
		email = r.FormValue("email")
		password = r.FormValue("password")
		name = r.FormValue("name")
		role = models.Role(r.FormValue("role"))
	}

	if email == "" || password == "" || name == "" {
		writeError(w, http.StatusBadRequest, "email, password and name are required")
		return
	}

	switch role {
	case models.RoleAcademyHead, models.RoleAdmin, models.RoleMentor, models.RoleMentee:
		// valid
	default:
		// default to mentee if empty or invalid
		role = models.RoleMentee
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	user := models.User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: string(hash),
		Name:         name,
		Role:         role,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	_, err = h.DB.Exec(
		`INSERT INTO users (id, email, password_hash, name, role, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		user.ID, user.Email, user.PasswordHash, user.Name, user.Role, user.CreatedAt,
	)
	if err != nil {
		writeError(w, http.StatusConflict, "email already registered or db error: "+err.Error())
		return
	}

	token, err := middleware.IssueToken(h.JWTSecret, user.ID, user.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	// If it's a browser form request, set the cookie/session and redirect to dashboard
	if r.Header.Get("Content-Type") != "application/json" {
		http.SetCookie(w, &http.Cookie{
			Name:     "token",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
		})
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{Token: token, User: user})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var user models.User
	err := h.DB.QueryRow(
		`SELECT id, email, password_hash, name, role, created_at FROM users WHERE email = ?`,
		req.Email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role, &user.CreatedAt)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	token, err := middleware.IssueToken(h.JWTSecret, user.ID, user.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Token: token, User: user})
}
