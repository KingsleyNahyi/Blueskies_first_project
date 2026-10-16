package notify

import (
	"database/sql"
	"fmt"
	"html"
	"log"
)

// FlagCreated emails the course's mentor. Call it with `go` so a slow
// email never delays the request. Errors are logged, never returned.
func FlagCreated(db *sql.DB, c *Client, baseURL, enrollmentID, taskID, note string) {
	if !c.Enabled() {
		return
	}
	var mentorEmail, mentorName, menteeName, courseTitle, taskTitle string
	err := db.QueryRow(`
		SELECT mu.email, mu.name, me.name, c.title, ct.title
		FROM enrollments e
		JOIN courses c      ON c.id = e.course_id
		JOIN users mu       ON mu.id = c.mentor_id
		JOIN users me       ON me.id = e.mentee_id
		JOIN course_tasks ct ON ct.id = ?
		WHERE e.id = ?`, taskID, enrollmentID,
	).Scan(&mentorEmail, &mentorName, &menteeName, &courseTitle, &taskTitle)
	if err != nil {
		log.Printf("notify: flag lookup failed: %v", err)
		return
	}

	subject := fmt.Sprintf("%s is stuck on \"%s\"", menteeName, taskTitle)
	body := fmt.Sprintf(
		`<p>Hi %s,</p><p><b>%s</b> flagged <b>%s</b> in <b>%s</b>:</p><blockquote>%s</blockquote><p><a href="%s/enrollments/%s">View their progress</a></p>`,
		html.EscapeString(mentorName), html.EscapeString(menteeName), html.EscapeString(taskTitle),
		html.EscapeString(courseTitle), html.EscapeString(note), baseURL, enrollmentID,
	)
	if err := c.Send(mentorEmail, subject, body); err != nil {
		log.Printf("notify: send failed: %v", err)
	}
}