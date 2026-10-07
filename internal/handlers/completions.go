package handlers

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bluesbirds/academy/internal/middleware"
)

type CompletionHandler struct {
	DB *sql.DB
}

// MarkTaskDone is the entire check-in mechanism: a mentee marks a task
// complete for their enrollment. No separate "status update" entity,
// per design doc section 2.
func (h *CompletionHandler) MarkTaskDone(w http.ResponseWriter, r *http.Request) {
	enrollmentID := chi.URLParam(r, "enrollmentID")
	taskID := chi.URLParam(r, "taskID")
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

	_, err := h.DB.Exec(
		`INSERT INTO task_completions (id, enrollment_id, course_task_id) VALUES (?, ?, ?)
		 ON CONFLICT (enrollment_id, course_task_id) DO NOTHING`,
		uuid.NewString(), enrollmentID, taskID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not mark task done: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}
