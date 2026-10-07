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

type CourseHandler struct {
	DB *sql.DB
}

type createCourseRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Tasks       []struct {
		Title       string `json:"title"`
		TaskType    string `json:"task_type"`
		ContentURL  string `json:"content_url"`
		Description string `json:"description"`
	} `json:"tasks"`
}

// CreateCourse lets a mentor define a reusable course template with its
// ordered list of tasks in one call.
func (h *CourseHandler) CreateCourse(w http.ResponseWriter, r *http.Request) {
	mentorID, _ := middleware.UserIDFromContext(r.Context())

	var req createCourseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback()

	course := models.Course{
		ID: uuid.NewString(), MentorID: mentorID, Title: req.Title, Description: req.Description,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := tx.Exec(
		`INSERT INTO courses (id, mentor_id, title, description, created_at) VALUES (?, ?, ?, ?, ?)`,
		course.ID, course.MentorID, course.Title, course.Description, course.CreatedAt,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create course: "+err.Error())
		return
	}

	for i, t := range req.Tasks {
		if _, err := tx.Exec(
			`INSERT INTO course_tasks (id, course_id, order_index, title, task_type, content_url, description)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), course.ID, i, t.Title, t.TaskType, t.ContentURL, t.Description,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not create task: "+err.Error())
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save course")
		return
	}

	writeJSON(w, http.StatusCreated, course)
}

// ListCourses returns every course the requesting mentor owns.
func (h *CourseHandler) ListCourses(w http.ResponseWriter, r *http.Request) {
	mentorID, _ := middleware.UserIDFromContext(r.Context())

	rows, err := h.DB.Query(
		`SELECT id, mentor_id, title, description, created_at FROM courses WHERE mentor_id = ? ORDER BY created_at DESC`,
		mentorID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	courses := []models.Course{}
	for rows.Next() {
		var c models.Course
		if err := rows.Scan(&c.ID, &c.MentorID, &c.Title, &c.Description, &c.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		courses = append(courses, c)
	}

	writeJSON(w, http.StatusOK, courses)
}

// GetCourseTasks returns the ordered task list for a course template.
func (h *CourseHandler) GetCourseTasks(w http.ResponseWriter, r *http.Request) {
	courseID := chi.URLParam(r, "courseID")

	rows, err := h.DB.Query(
		`SELECT id, course_id, order_index, title, task_type, content_url, description
		 FROM course_tasks WHERE course_id = ? ORDER BY order_index ASC`,
		courseID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	tasks := []models.CourseTask{}
	for rows.Next() {
		var t models.CourseTask
		if err := rows.Scan(&t.ID, &t.CourseID, &t.OrderIndex, &t.Title, &t.TaskType, &t.ContentURL, &t.Description); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		tasks = append(tasks, t)
	}

	writeJSON(w, http.StatusOK, tasks)
}
