-- Core users table. A mentor and a mentee are both just users with a
-- role; academy_head/admin are the other two decided roles.
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    name          TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('academy_head', 'admin', 'mentor', 'mentee')),
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Course templates. Created once by a mentor, reused across mentees.
-- Not tied to any specific mentee (per design doc section 2).
CREATE TABLE IF NOT EXISTS courses (
    id          TEXT PRIMARY KEY,
    mentor_id   TEXT NOT NULL REFERENCES users(id),
    title       TEXT NOT NULL,
    description TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Ordered content items within a course template (videos, reading, etc).
CREATE TABLE IF NOT EXISTS course_tasks (
    id          TEXT PRIMARY KEY,
    course_id   TEXT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    order_index INTEGER NOT NULL,
    title       TEXT NOT NULL,
    task_type   TEXT NOT NULL CHECK (task_type IN ('video', 'reading', 'resource', 'other')),
    content_url TEXT,
    description TEXT,
    UNIQUE (course_id, order_index)
);

-- Mentee <-> course link. This is where per-person timing lives.
CREATE TABLE IF NOT EXISTS enrollments (
    id           TEXT PRIMARY KEY,
    mentee_id    TEXT NOT NULL REFERENCES users(id),
    course_id    TEXT NOT NULL REFERENCES courses(id),
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed', 'paused')),
    enrolled_at  TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (mentee_id, course_id)
);

-- Per-task due dates for a given enrollment. Decided against a single
-- course-level end date so pace can be tracked task-by-task, matching
-- the "3 checks a week" cadence mentors already use.
CREATE TABLE IF NOT EXISTS enrollment_task_due_dates (
    id             TEXT PRIMARY KEY,
    enrollment_id  TEXT NOT NULL REFERENCES enrollments(id) ON DELETE CASCADE,
    course_task_id TEXT NOT NULL REFERENCES course_tasks(id) ON DELETE CASCADE,
    due_date       TEXT NOT NULL,
    UNIQUE (enrollment_id, course_task_id)
);

-- The check-in mechanism itself: mentee marks a task done.
CREATE TABLE IF NOT EXISTS task_completions (
    id             TEXT PRIMARY KEY,
    enrollment_id  TEXT NOT NULL REFERENCES enrollments(id) ON DELETE CASCADE,
    course_task_id TEXT NOT NULL REFERENCES course_tasks(id) ON DELETE CASCADE,
    completed_at   TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (enrollment_id, course_task_id)
);

-- Task-scoped "I'm stuck" flag with a note. No general comment thread
-- for the POC, per design doc section 2.
CREATE TABLE IF NOT EXISTS flags (
    id             TEXT PRIMARY KEY,
    enrollment_id  TEXT NOT NULL REFERENCES enrollments(id) ON DELETE CASCADE,
    course_task_id TEXT NOT NULL REFERENCES course_tasks(id) ON DELETE CASCADE,
    note           TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    resolved_at    TEXT
);

CREATE INDEX IF NOT EXISTS idx_course_tasks_course ON course_tasks(course_id);
CREATE INDEX IF NOT EXISTS idx_enrollments_mentee ON enrollments(mentee_id);
CREATE INDEX IF NOT EXISTS idx_enrollments_course ON enrollments(course_id);
CREATE INDEX IF NOT EXISTS idx_task_completions_enrollment ON task_completions(enrollment_id);
CREATE INDEX IF NOT EXISTS idx_flags_enrollment ON flags(enrollment_id);
CREATE INDEX IF NOT EXISTS idx_flags_status ON flags(status);
