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
)

type EnrollmentHandler struct {
	DB *sql.DB
}

type createEnrollmentRequest struct {
	MenteeID string `json:"mentee_id"`
	CourseID string `json:"course_id"`
	// DaysPerTask spreads due dates evenly, one task every N days,
	// starting today. Auto-spread now, adjustable per learner later
	// (per design doc section 2, "detail TBD" -> we pick a default).
	DaysPerTask int `json:"days_per_task"`
}

// CreateEnrollment enrolls a mentee in a course and auto-spreads a due
// date per task. A mentor calls this (they own the course being
// assigned).
func (h *EnrollmentHandler) CreateEnrollment(w http.ResponseWriter, r *http.Request) {
	var req createEnrollmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MenteeID == "" || req.CourseID == "" {
		writeError(w, http.StatusBadRequest, "mentee_id and course_id are required")
		return
	}
	if req.DaysPerTask <= 0 {
		req.DaysPerTask = 2 // default: roughly matches a 3x/week check-in cadence
	}

	tx, err := h.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback()

	enrollment := models.Enrollment{
		ID: uuid.NewString(), MenteeID: req.MenteeID, CourseID: req.CourseID, Status: "active",
		EnrolledAt: time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := tx.Exec(
		`INSERT INTO enrollments (id, mentee_id, course_id, status, enrolled_at) VALUES (?, ?, ?, 'active', ?)`,
		enrollment.ID, enrollment.MenteeID, enrollment.CourseID, enrollment.EnrolledAt,
	); err != nil {
		writeError(w, http.StatusConflict, "could not enroll: "+err.Error())
		return
	}

	rows, err := tx.Query(
		`SELECT id FROM course_tasks WHERE course_id = ? ORDER BY order_index ASC`,
		req.CourseID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	var taskIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		taskIDs = append(taskIDs, id)
	}
	rows.Close()

	now := time.Now()
	for i, taskID := range taskIDs {
		due := now.AddDate(0, 0, (i+1)*req.DaysPerTask).Format("2006-01-02")
		if _, err := tx.Exec(
			`INSERT INTO enrollment_task_due_dates (id, enrollment_id, course_task_id, due_date) VALUES (?, ?, ?, ?)`,
			uuid.NewString(), enrollment.ID, taskID, due,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not set due dates: "+err.Error())
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save enrollment")
		return
	}

	writeJSON(w, http.StatusCreated, enrollment)
}

// GetMenteeProgress is the data behind the mentee dashboard: the
// enrolled course's tasks, each with its due date, completion state,
// and whether it's currently flagged.
func (h *EnrollmentHandler) GetMenteeProgress(w http.ResponseWriter, r *http.Request) {
	enrollmentID := chi.URLParam(r, "enrollmentID")
	userID, _ := middleware.UserIDFromContext(r.Context())
	role, _ := middleware.RoleFromContext(r.Context())

	// A mentee may only view their own enrollment; mentors/admins can
	// view any (the mentor dashboard relies on this).
	if role == models.RoleMentee {
		var owner string
		if err := h.DB.QueryRow(`SELECT mentee_id FROM enrollments WHERE id = ?`, enrollmentID).Scan(&owner); err != nil {
			writeError(w, http.StatusNotFound, "enrollment not found")
			return
		}
		if owner != userID {
			writeError(w, http.StatusForbidden, "not your enrollment")
			return
		}
	}

	rows, err := h.DB.Query(`
		SELECT
			ct.id, ct.course_id, ct.order_index, ct.title, ct.task_type, ct.content_url, ct.description,
			dd.due_date,
			tc.completed_at,
			EXISTS (
				SELECT 1 FROM flags f
				WHERE f.enrollment_id = ? AND f.course_task_id = ct.id AND f.status = 'open'
			) AS flagged
		FROM course_tasks ct
		JOIN enrollments e ON e.course_id = ct.course_id
		LEFT JOIN enrollment_task_due_dates dd ON dd.enrollment_id = e.id AND dd.course_task_id = ct.id
		LEFT JOIN task_completions tc ON tc.enrollment_id = e.id AND tc.course_task_id = ct.id
		WHERE e.id = ?
		ORDER BY ct.order_index ASC
	`, enrollmentID, enrollmentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	defer rows.Close()

	tasks := []models.TaskWithProgress{}
	for rows.Next() {
		var t models.TaskWithProgress
		var flaggedInt int
		if err := rows.Scan(
			&t.ID, &t.CourseID, &t.OrderIndex, &t.Title, &t.TaskType, &t.ContentURL, &t.Description,
			&t.DueDate, &t.CompletedAt, &flaggedInt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error: "+err.Error())
			return
		}
		t.Flagged = flaggedInt == 1
		tasks = append(tasks, t)
	}

	writeJSON(w, http.StatusOK, tasks)
}
