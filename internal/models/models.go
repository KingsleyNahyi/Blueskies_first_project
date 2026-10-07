package models

type Role string

const (
	RoleAcademyHead Role = "academy_head"
	RoleAdmin       Role = "admin"
	RoleMentor      Role = "mentor"
	RoleMentee      Role = "mentee"
)

type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	Name         string `json:"name"`
	Role         Role   `json:"role"`
	CreatedAt    string `json:"created_at"`
}

type Course struct {
	ID          string `json:"id"`
	MentorID    string `json:"mentor_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

type CourseTask struct {
	ID          string `json:"id"`
	CourseID    string `json:"course_id"`
	OrderIndex  int    `json:"order_index"`
	Title       string `json:"title"`
	TaskType    string `json:"task_type"` // video | reading | resource | other
	ContentURL  string `json:"content_url,omitempty"`
	Description string `json:"description,omitempty"`
}

type Enrollment struct {
	ID          string `json:"id"`
	MenteeID    string `json:"mentee_id"`
	CourseID    string `json:"course_id"`
	Status      string `json:"status"`
	EnrolledAt  string `json:"enrolled_at"`
}

type EnrollmentTaskDueDate struct {
	ID           string `json:"id"`
	EnrollmentID string `json:"enrollment_id"`
	CourseTaskID string `json:"course_task_id"`
	DueDate      string `json:"due_date"`
}

type TaskCompletion struct {
	ID           string `json:"id"`
	EnrollmentID string `json:"enrollment_id"`
	CourseTaskID string `json:"course_task_id"`
	CompletedAt  string `json:"completed_at"`
}

type Flag struct {
	ID           string  `json:"id"`
	EnrollmentID string  `json:"enrollment_id"`
	CourseTaskID string  `json:"course_task_id"`
	Note         string  `json:"note"`
	Status       string  `json:"status"` // open | resolved
	CreatedAt    string  `json:"created_at"`
	ResolvedAt   *string `json:"resolved_at,omitempty"`
}

// TaskWithProgress is a joined view used by the mentee dashboard: a
// course task plus this enrollment's due date / completion state.
type TaskWithProgress struct {
	CourseTask
	DueDate     *string `json:"due_date,omitempty"`
	CompletedAt *string `json:"completed_at,omitempty"`
	Flagged     bool    `json:"flagged"`
}

// MenteeSummary is a row in the mentor dashboard's mentee list.
type MenteeSummary struct {
	EnrollmentID   string `json:"enrollment_id"`
	MenteeID       string `json:"mentee_id"`
	MenteeName     string `json:"mentee_name"`
	CourseID       string `json:"course_id"`
	CourseTitle    string `json:"course_title"`
	TotalTasks     int    `json:"total_tasks"`
	CompletedTasks int    `json:"completed_tasks"`
	OpenFlagCount  int    `json:"open_flag_count"`
	Status         string `json:"status"` // on_track | behind_pace | flagged
}
