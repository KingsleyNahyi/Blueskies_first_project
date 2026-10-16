package handlers

import (
	"database/sql"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/bluesbirds/academy/internal/middleware"
	"github.com/bluesbirds/academy/internal/notify"
	"github.com/bluesbirds/academy/internal/models"
)

type PageHandler struct {
	DB        *sql.DB
	JWTSecret string
	Templates map[string]*template.Template
	Notify     *notify.Client
AppBaseURL string
}

// LoadPageTemplates builds one *template.Template per page, each combining
// the shared base.html layout with that page's own content block. They're
// parsed separately per page (not all together with ParseGlob) because
// every page file defines a block named "content" — parsing them into one
// shared set would make the later files silently overwrite the earlier
// ones' "content" definition.
func LoadPageTemplates() map[string]*template.Template {
	pages := []string{
		"login", "signup", "mentee_dashboard", "mentor_dashboard",
		"course_new", "course_edit", "courses_list",
		"enrollment_new", "enrollment_detail",
	}
	out := map[string]*template.Template{}
	for _, p := range pages {
		out[p] = template.Must(template.ParseFiles(
			"web/templates/base.html",
			"web/templates/"+p+".html",
		))
	}
	return out
}

func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?"
	}
	if len(parts) == 1 {
		return strings.ToUpper(parts[0][:1])
	}
	return strings.ToUpper(parts[0][:1] + parts[len(parts)-1][:1])
}

// --- Auth pages ---

type loginView struct {
	PageTitle string
	User      *models.User
	Error     string
}

func (h *PageHandler) LoginForm(w http.ResponseWriter, r *http.Request) {
	h.Templates["login"].ExecuteTemplate(w, "base", loginView{PageTitle: "Log in"})
}

func (h *PageHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	email := r.FormValue("email")
	password := r.FormValue("password")

	var user models.User
	err := h.DB.QueryRow(
		`SELECT id, email, password_hash, name, role, created_at FROM users WHERE email = ?`,
		email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role, &user.CreatedAt)

	invalid := err == sql.ErrNoRows
	if err == nil {
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
			invalid = true
		}
	} else if err != sql.ErrNoRows {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	if invalid {
		w.WriteHeader(http.StatusUnauthorized)
		h.Templates["login"].ExecuteTemplate(w, "base", loginView{
			PageTitle: "Log in",
			Error:     "Invalid email or password.",
		})
		return
	}

	token, err := middleware.IssueToken(h.JWTSecret, user.ID, user.Role)
	if err != nil {
		http.Error(w, "could not issue session", http.StatusInternalServerError)
		return
	}
	middleware.SetSessionCookie(w, token)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// SignupForm / SignupSubmit are the PUBLIC, unauthenticated self-signup
// flow — deliberately separate from the JSON API's /api/v1/auth/register,
// which still accepts any role and stays that way on purpose: it's how
// you (the admin) create mentor / academy_head / admin accounts by hand
// for now, via curl/Invoke-RestMethod, not something exposed in the UI.
// This handler NEVER reads a role from the form — it's hardcoded to
// mentee below. That's the actual security fix: a public page that can
// create privileged accounts is the bug, not the existence of roles.
func (h *PageHandler) SignupForm(w http.ResponseWriter, r *http.Request) {
	h.Templates["signup"].ExecuteTemplate(w, "base", loginView{PageTitle: "Sign up"})
}

func (h *PageHandler) SignupSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	if name == "" || email == "" || len(password) < 8 {
		h.Templates["signup"].ExecuteTemplate(w, "base", loginView{
			PageTitle: "Sign up", Error: "Name, email, and a password of at least 8 characters are required.",
		})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "could not hash password", http.StatusInternalServerError)
		return
	}

	userID := uuid.NewString()
	_, err = h.DB.Exec(
		`INSERT INTO users (id, email, password_hash, name, role) VALUES (?, ?, ?, ?, 'mentee')`,
		userID, email, string(hash), name,
	)
	if err != nil {
		h.Templates["signup"].ExecuteTemplate(w, "base", loginView{
			PageTitle: "Sign up", Error: "That email is already registered.",
		})
		return
	}

	token, err := middleware.IssueToken(h.JWTSecret, userID, models.RoleMentee)
	if err != nil {
		http.Error(w, "could not issue session", http.StatusInternalServerError)
		return
	}
	middleware.SetSessionCookie(w, token)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *PageHandler) Logout(w http.ResponseWriter, r *http.Request) {
	middleware.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Dashboard: routes to the mentee or mentor view by role ---

func (h *PageHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	role, _ := middleware.RoleFromContext(r.Context())

	var user models.User
	if err := h.DB.QueryRow(
		`SELECT id, email, name, role, created_at FROM users WHERE id = ?`,
		userID,
	).Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.CreatedAt); err != nil {
		http.Error(w, "could not load user", http.StatusInternalServerError)
		return
	}

	if role == models.RoleMentee {
		h.renderMenteeOverview(w, userID, &user)
		return
	}
	h.renderMentorDashboard(w, userID, &user)
}

type taskView struct {
	ID          string
	Title       string
	DueDate     string
	CompletedAt string
	Flagged     bool
}

// courseCardView is one row on the mentee's course-overview dashboard.
type courseCardView struct {
	EnrollmentID    string
	CourseTitle     string
	MentorName      string
	CompletedCount  int
	TotalCount      int
	PercentComplete int
	HasOpenFlag     bool
}

type menteeOverviewView struct {
	PageTitle string
	User      *models.User
	Courses   []courseCardView
}

// renderMenteeOverview lists every active enrollment for this mentee —
// the dashboard is now a list of courses, not a single course's detail
// (that moved to EnrollmentDetail / /enrollments/{id}).
func (h *PageHandler) renderMenteeOverview(w http.ResponseWriter, userID string, user *models.User) {
	rows, err := h.DB.Query(`
		SELECT
			e.id, c.title, u.name,
			(SELECT COUNT(*) FROM course_tasks ct WHERE ct.course_id = c.id),
			(SELECT COUNT(*) FROM task_completions tc WHERE tc.enrollment_id = e.id),
			EXISTS (SELECT 1 FROM flags f WHERE f.enrollment_id = e.id AND f.status = 'open')
		FROM enrollments e
		JOIN courses c ON c.id = e.course_id
		JOIN users u ON u.id = c.mentor_id
		WHERE e.mentee_id = ? AND e.status = 'active'
		ORDER BY e.enrolled_at DESC
	`, userID)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var courses []courseCardView
	for rows.Next() {
		var c courseCardView
		var hasFlag int
		if err := rows.Scan(&c.EnrollmentID, &c.CourseTitle, &c.MentorName, &c.TotalCount, &c.CompletedCount, &hasFlag); err != nil {
			http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		c.HasOpenFlag = hasFlag == 1
		if c.TotalCount > 0 {
			c.PercentComplete = c.CompletedCount * 100 / c.TotalCount
		}
		courses = append(courses, c)
	}

	h.Templates["mentee_dashboard"].ExecuteTemplate(w, "base", menteeOverviewView{
		PageTitle: "Your courses", User: user, Courses: courses,
	})
}

type enrollmentDetailView struct {
	PageTitle       string
	User            *models.User
	EnrollmentID    string
	CourseTitle     string
	MentorName      string
	CompletedCount  int
	TotalCount      int
	PercentComplete int
	Tasks           []taskView
}

// EnrollmentDetail is the per-course task timeline a mentee lands on
// after clicking a course card — this is what the old single-course
// mentee dashboard used to show directly.
func (h *PageHandler) EnrollmentDetail(w http.ResponseWriter, r *http.Request) {
	enrollmentID := chi.URLParam(r, "enrollmentID")
	userID, _ := middleware.UserIDFromContext(r.Context())
	user := h.mustCurrentUser(w, r)
	if user == nil {
		return
	}

	var owner, courseTitle, mentorName string
	err := h.DB.QueryRow(`
		SELECT e.mentee_id, c.title, u.name
		FROM enrollments e
		JOIN courses c ON c.id = e.course_id
		JOIN users u ON u.id = c.mentor_id
		WHERE e.id = ?
	`, enrollmentID).Scan(&owner, &courseTitle, &mentorName)
	if err == sql.ErrNoRows {
		http.Error(w, "enrollment not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Mentees may only view their own enrollment. (Mentor/admin viewing
	// any enrollment's detail page isn't wired up yet — only the
	// aggregate mentor dashboard is, for now.)
	if owner != userID {
		http.Error(w, "not your enrollment", http.StatusForbidden)
		return
	}

	rows, err := h.DB.Query(`
		SELECT ct.id, ct.title, dd.due_date, tc.completed_at,
			EXISTS (SELECT 1 FROM flags f WHERE f.enrollment_id = ? AND f.course_task_id = ct.id AND f.status = 'open')
		FROM course_tasks ct
		JOIN enrollments e ON e.course_id = ct.course_id
		LEFT JOIN enrollment_task_due_dates dd ON dd.enrollment_id = e.id AND dd.course_task_id = ct.id
		LEFT JOIN task_completions tc ON tc.enrollment_id = e.id AND tc.course_task_id = ct.id
		WHERE e.id = ?
		ORDER BY ct.order_index ASC
	`, enrollmentID, enrollmentID)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tasks []taskView
	completed, total := 0, 0
	for rows.Next() {
		var t taskView
		var dueDate, completedAt sql.NullString
		var flaggedInt int
		if err := rows.Scan(&t.ID, &t.Title, &dueDate, &completedAt, &flaggedInt); err != nil {
			http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		t.DueDate = dueDate.String
		t.CompletedAt = completedAt.String
		t.Flagged = flaggedInt == 1
		total++
		if t.CompletedAt != "" {
			completed++
		}
		tasks = append(tasks, t)
	}

	pct := 0
	if total > 0 {
		pct = completed * 100 / total
	}

	h.Templates["enrollment_detail"].ExecuteTemplate(w, "base", enrollmentDetailView{
		PageTitle: courseTitle, User: user,
		EnrollmentID: enrollmentID, CourseTitle: courseTitle, MentorName: mentorName,
		CompletedCount: completed, TotalCount: total, PercentComplete: pct,
		Tasks: tasks,
	})
}

type menteeRowView struct {
	EnrollmentID    string
	MenteeName      string
	Initials        string
	Status          string
	CompletedTasks  int
	TotalTasks      int
	PercentComplete int
}

type mentorDashboardView struct {
	PageTitle string
	User      *models.User
	Mentees   []menteeRowView
}

// renderMentorDashboard mirrors DashboardHandler.MentorDashboard's query
// (same status derivation: flagged > behind_pace > on_track) but adds
// the display-only fields (Initials, PercentComplete) the template needs.
func (h *PageHandler) renderMentorDashboard(w http.ResponseWriter, mentorID string, user *models.User) {
	rows, err := h.DB.Query(`
		SELECT
			e.id, u.name,
			(SELECT COUNT(*) FROM course_tasks ct WHERE ct.course_id = c.id),
			(SELECT COUNT(*) FROM task_completions tc WHERE tc.enrollment_id = e.id),
			(SELECT COUNT(*) FROM flags f WHERE f.enrollment_id = e.id AND f.status = 'open'),
			(SELECT COUNT(*) FROM enrollment_task_due_dates dd
				LEFT JOIN task_completions tc2 ON tc2.enrollment_id = e.id AND tc2.course_task_id = dd.course_task_id
				WHERE dd.enrollment_id = e.id AND dd.due_date < date('now') AND tc2.id IS NULL)
		FROM enrollments e
		JOIN users u ON u.id = e.mentee_id
		JOIN courses c ON c.id = e.course_id
		WHERE c.mentor_id = ? AND e.status = 'active'
		ORDER BY u.name ASC
	`, mentorID)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var mentees []menteeRowView
	for rows.Next() {
		var m menteeRowView
		var openFlags, overdue int
		if err := rows.Scan(&m.EnrollmentID, &m.MenteeName, &m.TotalTasks, &m.CompletedTasks, &openFlags, &overdue); err != nil {
			http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		switch {
		case openFlags > 0:
			m.Status = "flagged"
		case overdue > 0:
			m.Status = "behind_pace"
		default:
			m.Status = "on_track"
		}
		m.Initials = initials(m.MenteeName)
		if m.TotalTasks > 0 {
			m.PercentComplete = m.CompletedTasks * 100 / m.TotalTasks
		}
		mentees = append(mentees, m)
	}

	sortMenteesFlaggedFirst(mentees)

	h.Templates["mentor_dashboard"].ExecuteTemplate(w, "base", mentorDashboardView{
		PageTitle: "Dashboard", User: user, Mentees: mentees,
	})
}

func sortMenteesFlaggedFirst(m []menteeRowView) {
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
	for i := 1; i < len(m); i++ {
		j := i
		for j > 0 && rank(m[j-1].Status) > rank(m[j].Status) {
			m[j-1], m[j] = m[j], m[j-1]
			j--
		}
	}
}

// --- Form actions (mentee marks a task done / flags it stuck) ---

func (h *PageHandler) CompleteTaskPage(w http.ResponseWriter, r *http.Request) {
	enrollmentID := chi.URLParam(r, "enrollmentID")
	taskID := chi.URLParam(r, "taskID")
	userID, _ := middleware.UserIDFromContext(r.Context())

	var owner string
	if err := h.DB.QueryRow(`SELECT mentee_id FROM enrollments WHERE id = ?`, enrollmentID).Scan(&owner); err != nil || owner != userID {
		http.Error(w, "not your enrollment", http.StatusForbidden)
		return
	}

	h.DB.Exec(
		`INSERT INTO task_completions (id, enrollment_id, course_task_id) VALUES (?, ?, ?)
		 ON CONFLICT (enrollment_id, course_task_id) DO NOTHING`,
		uuid.NewString(), enrollmentID, taskID,
	)
	http.Redirect(w, r, "/enrollments/"+enrollmentID, http.StatusSeeOther)
}

func (h *PageHandler) CreateFlagPage(w http.ResponseWriter, r *http.Request) {
	enrollmentID := chi.URLParam(r, "enrollmentID")
	taskID := chi.URLParam(r, "taskID")
	userID, _ := middleware.UserIDFromContext(r.Context())

	var owner string
	if err := h.DB.QueryRow(`SELECT mentee_id FROM enrollments WHERE id = ?`, enrollmentID).Scan(&owner); err != nil || owner != userID {
		http.Error(w, "not your enrollment", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	note := r.FormValue("note")
	if note == "" {
		http.Error(w, "note is required", http.StatusBadRequest)
		return
	}
if _, err := h.DB.Exec(
	`INSERT INTO flags (id, enrollment_id, course_task_id, note, status) VALUES (?, ?, ?, ?, 'open')`,
	uuid.NewString(), enrollmentID, taskID, note,
); err == nil {
	go notify.FlagCreated(h.DB, h.Notify, h.AppBaseURL, enrollmentID, taskID, note)
}
	http.Redirect(w, r, "/enrollments/"+enrollmentID, http.StatusSeeOther)
}

// ResolveEnrollmentFlagsPage is a POC simplification of the JSON API's
// per-flag /flags/{flagID}/resolve: the mentor dashboard's "Resolve"
// button acts per mentee row, not per individual flag, so this closes
// every open flag on that enrollment at once rather than needing a
// specific flag ID from the page.
func (h *PageHandler) ResolveEnrollmentFlagsPage(w http.ResponseWriter, r *http.Request) {
	enrollmentID := chi.URLParam(r, "enrollmentID")
	h.DB.Exec(`UPDATE flags SET status = 'resolved', resolved_at = datetime('now') WHERE enrollment_id = ? AND status = 'open'`, enrollmentID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// --- Course creation (mentor) ---

type courseFormView struct {
	PageTitle string
	User      *models.User
	Error     string
}

func (h *PageHandler) NewCourseForm(w http.ResponseWriter, r *http.Request) {
	user := h.mustCurrentUser(w, r)
	if user == nil {
		return
	}
	h.Templates["course_new"].ExecuteTemplate(w, "base", courseFormView{PageTitle: "New course", User: user})
}

// validTaskTypes guards against a malformed/tampered <select> value —
// used both here and in AddTasksToCourseSubmit.
var validTaskTypes = map[string]bool{"video": true, "reading": true, "resource": true, "other": true}

// CreateCourseSubmit reads the task_title / task_type form fields the
// "+ Add task" JS produces. Because every row uses the SAME field name
// (name="task_title" on every row), the browser submits them as repeated
// fields, and r.Form["task_title"] comes back as a slice in submission
// order — index i of task_title lines up with index i of task_type.
// No array-index naming (task_title[0], task_title[1]...) needed.
func (h *PageHandler) CreateCourseSubmit(w http.ResponseWriter, r *http.Request) {
	user := h.mustCurrentUser(w, r)
	if user == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	description := r.FormValue("description")
	titles := r.Form["task_title"]
	types := r.Form["task_type"]

	type parsedTask struct{ Title, Type string }
	var tasks []parsedTask
	for i, t := range titles {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		taskType := "other"
		if i < len(types) && validTaskTypes[types[i]] {
			taskType = types[i]
		}
		tasks = append(tasks, parsedTask{Title: t, Type: taskType})
	}

	if title == "" || len(tasks) == 0 {
		h.Templates["course_new"].ExecuteTemplate(w, "base", courseFormView{
			PageTitle: "New course", User: user, Error: "Title and at least one task are required.",
		})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	courseID := uuid.NewString()
	if _, err := tx.Exec(
		`INSERT INTO courses (id, mentor_id, title, description) VALUES (?, ?, ?, ?)`,
		courseID, user.ID, title, description,
	); err != nil {
		http.Error(w, "could not create course: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for i, t := range tasks {
		if _, err := tx.Exec(
			`INSERT INTO course_tasks (id, course_id, order_index, title, task_type) VALUES (?, ?, ?, ?, ?)`,
			uuid.NewString(), courseID, i, t.Title, t.Type,
		); err != nil {
			http.Error(w, "could not create task: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "could not save course", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/enrollments/new", http.StatusSeeOther)
}

// --- Course list + edit (mentor) ---

type courseListItem struct {
	ID            string
	Title         string
	TaskCount     int
	EnrolledCount int
}

type coursesListView struct {
	PageTitle string
	User      *models.User
	Courses   []courseListItem
}

func (h *PageHandler) CoursesList(w http.ResponseWriter, r *http.Request) {
	user := h.mustCurrentUser(w, r)
	if user == nil {
		return
	}

	rows, err := h.DB.Query(`
		SELECT
			c.id, c.title,
			(SELECT COUNT(*) FROM course_tasks ct WHERE ct.course_id = c.id),
			(SELECT COUNT(*) FROM enrollments e WHERE e.course_id = c.id AND e.status = 'active')
		FROM courses c
		WHERE c.mentor_id = ?
		ORDER BY c.created_at DESC
	`, user.ID)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var courses []courseListItem
	for rows.Next() {
		var c courseListItem
		if err := rows.Scan(&c.ID, &c.Title, &c.TaskCount, &c.EnrolledCount); err != nil {
			http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		courses = append(courses, c)
	}

	h.Templates["courses_list"].ExecuteTemplate(w, "base", coursesListView{
		PageTitle: "Your courses", User: user, Courses: courses,
	})
}

type existingTaskView struct {
	Title    string
	TaskType string
}

type courseEditView struct {
	PageTitle     string
	User          *models.User
	CourseID      string
	CourseTitle   string
	ExistingTasks []existingTaskView
	Error         string
}

// courseOwnedByMentor is a small shared guard: confirms courseID exists
// and belongs to this mentor, returning the course title for display.
// Used by both CourseEdit and AddTasksToCourseSubmit so the ownership
// check can't be forgotten in one of the two.
func (h *PageHandler) courseOwnedByMentor(courseID, mentorID string) (string, bool) {
	var title string
	err := h.DB.QueryRow(`SELECT title FROM courses WHERE id = ? AND mentor_id = ?`, courseID, mentorID).Scan(&title)
	return title, err == nil
}

func (h *PageHandler) CourseEdit(w http.ResponseWriter, r *http.Request) {
	user := h.mustCurrentUser(w, r)
	if user == nil {
		return
	}
	courseID := chi.URLParam(r, "courseID")

	courseTitle, ok := h.courseOwnedByMentor(courseID, user.ID)
	if !ok {
		http.Error(w, "course not found", http.StatusNotFound)
		return
	}

	rows, err := h.DB.Query(
		`SELECT title, task_type FROM course_tasks WHERE course_id = ? ORDER BY order_index ASC`, courseID,
	)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tasks []existingTaskView
	for rows.Next() {
		var t existingTaskView
		if err := rows.Scan(&t.Title, &t.TaskType); err != nil {
			http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		tasks = append(tasks, t)
	}

	h.Templates["course_edit"].ExecuteTemplate(w, "base", courseEditView{
		PageTitle: courseTitle, User: user,
		CourseID: courseID, CourseTitle: courseTitle, ExistingTasks: tasks,
	})
}

// defaultTaskSpacingDays is used when backfilling due dates for tasks
// added to a course AFTER mentees are already enrolled. The original
// days_per_task a mentor chose at enrollment time isn't stored anywhere
// per-enrollment (only the resulting due dates are), so there's no way
// to recover their exact original cadence here — this is a deliberate
// POC simplification, same spirit as the fixed default in
// EnrollmentHandler.CreateEnrollment.
const defaultTaskSpacingDays = 2

// AddTasksToCourseSubmit appends new tasks to an existing course, then —
// this is the important part — backfills a due date for each new task
// on every mentee CURRENTLY enrolled in it, so an edited course actually
// shows up on mentees' dashboards instead of silently having orphaned
// tasks with no due date.
func (h *PageHandler) AddTasksToCourseSubmit(w http.ResponseWriter, r *http.Request) {
	user := h.mustCurrentUser(w, r)
	if user == nil {
		return
	}
	courseID := chi.URLParam(r, "courseID")
	courseTitle, ok := h.courseOwnedByMentor(courseID, user.ID)
	if !ok {
		http.Error(w, "course not found", http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	titles := r.Form["task_title"]
	types := r.Form["task_type"]

	type parsedTask struct{ Title, Type string }
	var newTasks []parsedTask
	for i, t := range titles {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		taskType := "other"
		if i < len(types) && validTaskTypes[types[i]] {
			taskType = types[i]
		}
		newTasks = append(newTasks, parsedTask{Title: t, Type: taskType})
	}
	if len(newTasks) == 0 {
		rows, _ := h.DB.Query(`SELECT title, task_type FROM course_tasks WHERE course_id = ? ORDER BY order_index ASC`, courseID)
		var existing []existingTaskView
		if rows != nil {
			for rows.Next() {
				var t existingTaskView
				rows.Scan(&t.Title, &t.TaskType)
				existing = append(existing, t)
			}
			rows.Close()
		}
		h.Templates["course_edit"].ExecuteTemplate(w, "base", courseEditView{
			PageTitle: courseTitle, User: user, CourseID: courseID, CourseTitle: courseTitle,
			ExistingTasks: existing, Error: "Add at least one task.",
		})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var nextOrderIndex int
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(order_index), -1) + 1 FROM course_tasks WHERE course_id = ?`, courseID,
	).Scan(&nextOrderIndex); err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	type newTaskID struct {
		ID   string
		Task parsedTask
	}
	var inserted []newTaskID
	for i, t := range newTasks {
		taskID := uuid.NewString()
		if _, err := tx.Exec(
			`INSERT INTO course_tasks (id, course_id, order_index, title, task_type) VALUES (?, ?, ?, ?, ?)`,
			taskID, courseID, nextOrderIndex+i, t.Title, t.Type,
		); err != nil {
			http.Error(w, "could not add task: "+err.Error(), http.StatusInternalServerError)
			return
		}
		inserted = append(inserted, newTaskID{ID: taskID, Task: t})
	}

	// Backfill due dates for every mentee already active in this course.
	enrollRows, err := tx.Query(`SELECT id FROM enrollments WHERE course_id = ? AND status = 'active'`, courseID)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var enrollmentIDs []string
	for enrollRows.Next() {
		var id string
		enrollRows.Scan(&id)
		enrollmentIDs = append(enrollmentIDs, id)
	}
	enrollRows.Close()

	for _, enrollmentID := range enrollmentIDs {
		var lastDue sql.NullString
		tx.QueryRow(
			`SELECT MAX(due_date) FROM enrollment_task_due_dates WHERE enrollment_id = ?`, enrollmentID,
		).Scan(&lastDue)

		baseline := time.Now()
		if lastDue.Valid && lastDue.String != "" {
			if parsed, err := time.Parse("2006-01-02", lastDue.String); err == nil {
				baseline = parsed
			}
		}

		for i, nt := range inserted {
			due := baseline.AddDate(0, 0, (i+1)*defaultTaskSpacingDays).Format("2006-01-02")
			if _, err := tx.Exec(
				`INSERT INTO enrollment_task_due_dates (id, enrollment_id, course_task_id, due_date) VALUES (?, ?, ?, ?)`,
				uuid.NewString(), enrollmentID, nt.ID, due,
			); err != nil {
				http.Error(w, "could not set due dates: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "could not save tasks", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/courses/"+courseID+"/edit", http.StatusSeeOther)
}

// --- Enrollment creation (mentor) ---

type simpleCourse struct{ ID, Title string }
type simpleMentee struct{ ID, Name, Email string }

type enrollmentFormView struct {
	PageTitle string
	User      *models.User
	Error     string
	Courses   []simpleCourse
	Mentees   []simpleMentee
}

func (h *PageHandler) NewEnrollmentForm(w http.ResponseWriter, r *http.Request) {
	user := h.mustCurrentUser(w, r)
	if user == nil {
		return
	}

	courses, mentees, err := h.loadCoursesAndMentees(user.ID)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.Templates["enrollment_new"].ExecuteTemplate(w, "base", enrollmentFormView{
		PageTitle: "Enroll a mentee", User: user, Courses: courses, Mentees: mentees,
	})
}

// CreateEnrollmentSubmit runs the same enroll-then-spread-due-dates
// transaction as the JSON API's EnrollmentHandler.CreateEnrollment.
func (h *PageHandler) CreateEnrollmentSubmit(w http.ResponseWriter, r *http.Request) {
	user := h.mustCurrentUser(w, r)
	if user == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	menteeID := r.FormValue("mentee_id")
	courseID := r.FormValue("course_id")
	daysPerTask, err := strconv.Atoi(r.FormValue("days_per_task"))
	if err != nil || daysPerTask <= 0 {
		daysPerTask = 2
	}

	reRender := func(errMsg string) {
		courses, mentees, _ := h.loadCoursesAndMentees(user.ID)
		h.Templates["enrollment_new"].ExecuteTemplate(w, "base", enrollmentFormView{
			PageTitle: "Enroll a mentee", User: user, Error: errMsg, Courses: courses, Mentees: mentees,
		})
	}

	if menteeID == "" || courseID == "" {
		reRender("Please choose both a course and a mentee.")
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	enrollmentID := uuid.NewString()
	if _, err := tx.Exec(
		`INSERT INTO enrollments (id, mentee_id, course_id, status) VALUES (?, ?, ?, 'active')`,
		enrollmentID, menteeID, courseID,
	); err != nil {
		reRender("Could not enroll — maybe this mentee is already in this course?")
		return
	}

	rows, err := tx.Query(`SELECT id FROM course_tasks WHERE course_id = ? ORDER BY order_index ASC`, courseID)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var taskIDs []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		taskIDs = append(taskIDs, id)
	}
	rows.Close()

	now := time.Now()
	for i, taskID := range taskIDs {
		due := now.AddDate(0, 0, (i+1)*daysPerTask).Format("2006-01-02")
		if _, err := tx.Exec(
			`INSERT INTO enrollment_task_due_dates (id, enrollment_id, course_task_id, due_date) VALUES (?, ?, ?, ?)`,
			uuid.NewString(), enrollmentID, taskID, due,
		); err != nil {
			http.Error(w, "could not set due dates: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "could not save enrollment", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// --- Shared helpers ---

func (h *PageHandler) mustCurrentUser(w http.ResponseWriter, r *http.Request) *models.User {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var user models.User
	if err := h.DB.QueryRow(
		`SELECT id, email, name, role, created_at FROM users WHERE id = ?`, userID,
	).Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.CreatedAt); err != nil {
		http.Error(w, "could not load user", http.StatusInternalServerError)
		return nil
	}
	return &user
}

func (h *PageHandler) loadCoursesAndMentees(mentorID string) ([]simpleCourse, []simpleMentee, error) {
	var courses []simpleCourse
	rows, err := h.DB.Query(`SELECT id, title FROM courses WHERE mentor_id = ? ORDER BY created_at DESC`, mentorID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var c simpleCourse
		if err := rows.Scan(&c.ID, &c.Title); err != nil {
			rows.Close()
			return nil, nil, err
		}
		courses = append(courses, c)
	}
	rows.Close()

	var mentees []simpleMentee
	rows, err = h.DB.Query(`SELECT id, name, email FROM users WHERE role = 'mentee' ORDER BY name ASC`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var m simpleMentee
		if err := rows.Scan(&m.ID, &m.Name, &m.Email); err != nil {
			rows.Close()
			return nil, nil, err
		}
		mentees = append(mentees, m)
	}
	rows.Close()

	return courses, mentees, nil
}
