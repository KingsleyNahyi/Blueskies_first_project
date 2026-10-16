package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bluesbirds/academy/internal/middleware"
	"github.com/bluesbirds/academy/internal/models"
	"github.com/bluesbirds/academy/internal/notify"
)

	type FlagHandler struct {
	DB         *sql.DB
	Notify     *notify.Client
	AppBaseURL string
}


type createFlagRequest struct {
	CourseTaskID string `json:"course_task_id"`
	Note         string `json:"note"`
}

// CreateFlag is the mentee's "I'm stuck" button: a task-scoped note to
// their mentor. Intentionally not a full comment thread (design doc
// section 2).
func (h *FlagHandler) CreateFlag(w http.ResponseWriter, r *http.Request) {
	enrollmentID := chi.URLParam(r, "enrollmentID")
	userID, _ := middleware.UserIDFromContext(r.Context())

	var owner string
	if err := h.DB.QueryRow(`SELECT mentee_id FROM enrollments WHERE id = ?`, enrollmentID).Scan(&owner); err != nil {
		writeError(w, http.StatusNotFound, "enrollment not found")
		return
	}
	if owner != userID {
		writeError(w, http.StatusForbidden, "not your enrollment")
		return
	}

	var req createFlagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CourseTaskID == "" || req.Note == "" {
		writeError(w, http.StatusBadRequest, "course_task_id and note are required")
		return
	}

	flag := models.Flag{
		ID: uuid.NewString(), EnrollmentID: enrollmentID, CourseTaskID: req.CourseTaskID, Note: req.Note, Status: "open",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	_, err := h.DB.Exec(
		`INSERT INTO flags (id, enrollment_id, course_task_id, note, status, created_at) VALUES (?, ?, ?, ?, 'open', ?)`,
		flag.ID, flag.EnrollmentID, flag.CourseTaskID, flag.Note, flag.CreatedAt,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create flag: "+err.Error())
		return
	}

	go notify.FlagCreated(h.DB, h.Notify, h.AppBaseURL, enrollmentID, req.CourseTaskID, req.Note)
	// trigger would fire once the notification model is decided
	// (design doc section 2, still open).

	writeJSON(w, http.StatusCreated, flag)
}

// ResolveFlag lets a mentor mark a flag as handled (the "Reply" action
// on the mentor dashboard leads here once messaging detail is decided;
// for now it just closes the flag).
func (h *FlagHandler) ResolveFlag(w http.ResponseWriter, r *http.Request) {
	flagID := chi.URLParam(r, "flagID")

	res, err := h.DB.Exec(
		`UPDATE flags SET status = 'resolved', resolved_at = datetime('now') WHERE id = ? AND status = 'open'`,
		flagID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not resolve flag: "+err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "flag not found or already resolved")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
}
