package handlers

import (
	"database/sql"
	"net/http"

	"github.com/bluesbirds/academy/internal/middleware"
	"github.com/bluesbirds/academy/internal/models"
)

type DashboardHandler struct {
	DB *sql.DB
}

// MentorDashboard returns one row per mentee across all of this
// mentor's courses: name, progress fraction, open flag count, and a
// derived status (flagged > behind_pace > on_track), so flagged
// mentees can be surfaced first without the mentor scrolling chat
// history (design doc section 6).
func (h *DashboardHandler) MentorDashboard(w http.ResponseWriter, r *http.Request) {
	mentorID, _ := middleware.UserIDFromContext(r.Context())

	rows, err := h.DB.Query(`
		SELECT
			e.id AS enrollment_id,
			u.id AS mentee_id,
			u.name AS mentee_name,
			c.id AS course_id,
			c.title AS course_title,
			(SELECT COUNT(*) FROM course_tasks ct WHERE ct.course_id = c.id) AS total_tasks,
			(SELECT COUNT(*) FROM task_completions tc WHERE tc.enrollment_id = e.id) AS completed_tasks,
			(SELECT COUNT(*) FROM flags f WHERE f.enrollment_id = e.id AND f.status = 'open') AS open_flag_count,
			(SELECT COUNT(*) FROM enrollment_task_due_dates dd
				LEFT JOIN task_completions tc2 ON tc2.enrollment_id = e.id AND tc2.course_task_id = dd.course_task_id
				WHERE dd.enrollment_id = e.id AND dd.due_date < date('now') AND tc2.id IS NULL) AS overdue_count
		FROM enrollments e
		JOIN users u ON u.id = e.mentee_id
		JOIN courses c ON c.id = e.course_id
		WHERE c.mentor_id = ? AND e.status = 'active'
		ORDER BY u.name ASC
	`, mentorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	defer rows.Close()

	summaries := []models.MenteeSummary{}
	for rows.Next() {
		var s models.MenteeSummary
		var overdueCount int
		if err := rows.Scan(
			&s.EnrollmentID, &s.MenteeID, &s.MenteeName, &s.CourseID, &s.CourseTitle,
			&s.TotalTasks, &s.CompletedTasks, &s.OpenFlagCount, &overdueCount,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error: "+err.Error())
			return
		}

		switch {
		case s.OpenFlagCount > 0:
			s.Status = "flagged"
		case overdueCount > 0:
			s.Status = "behind_pace"
		default:
			s.Status = "on_track"
		}

		summaries = append(summaries, s)
	}

	// Flagged mentees first, matching the mockup in design doc section 6.
	sortFlaggedFirst(summaries)

	writeJSON(w, http.StatusOK, summaries)
}

func sortFlaggedFirst(s []models.MenteeSummary) {
	rank := func(status string) int {
		switch status {
		case "flagged":
			return 0
		case "behind_pace":
			return 1
		default:
			return 2
		}
	}
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && rank(s[j-1].Status) > rank(s[j].Status) {
			s[j-1], s[j] = s[j], s[j-1]
			j--
		}
	}
}
