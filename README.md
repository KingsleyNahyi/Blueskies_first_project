# Bluesbirds Academy — Backend (POC) and FrontENd. 

This project serves as a solution to the mentee -mentor disconnect in BluesBirds. It seeks to allow Mentors to track progress and aid Mentees have a better way to see their upcoming tasks, communicate to mentors and have a better notification system. 

**Not built yet** (still open per the design doc): the community/social
layer (section 3), contracts, events, notification delivery (email
digesting, flag → notification triggers), and any messaging/contact
mechanism. Handlers are written so those slot in without reshaping what's
here — see "What's next" below.

## Stack

- Go 1.22, `chi` router
- Turso (libSQL) in production; a local SQLite file in dev, via the same
  driver (`libsql-client-go`), so there's zero code difference between
  environments — only the `DATABASE_URL` / `DATABASE_AUTH_TOKEN` env vars
  change
- JWT auth (`golang-jwt`), bcrypt password hashing
- No ORM — plain `database/sql`, since the schema is small enough that an
  ORM would add more ceremony than it saves for a POC

## Project layout

```
cmd/server/main.go          - wiring: config, db, migrations, routes
internal/config/            - env var loading
internal/db/                - connection + a tiny file-based migration runner
internal/models/            - plain structs shared by handlers
internal/middleware/        - JWT issuing/verification + RBAC role gate
internal/handlers/          - one file per resource area
migrations/0001_init.sql    - full schema for section 2 of the design doc
```

## Running it locally

```bash
cp .env.example .env    # then edit if you want, defaults work as-is
go mod tidy
go run ./cmd/server
```

This boots against a local `local.db` SQLite file (auto-created,
migrations auto-applied on startup) and listens on `:8080`.

> **Sandbox note:** this was built and tested in an environment where
> `golang.org` and `modernc.org` were blocked by egress rules, so
> `go.mod` has two `replace` lines pointing `x/crypto` and `x/exp` at
> their GitHub mirrors, and the local-dev SQLite driver is
> `mattn/go-sqlite3` (cgo-based) rather than the more common
> `modernc.org/sqlite` (pure Go, no cgo). If you're building somewhere
> without that restriction, feel free to delete the two `replace` lines
> and swap the driver import in `internal/db/db.go` back to
> `modernc.org/sqlite` — pure Go is generally nicer to work with (no
> cgo/gcc dependency), it's just what was reachable here.

### Switching to real Turso

```bash
turso db create bluesbirds-academy
turso db show bluesbirds-academy --url          # -> DATABASE_URL
turso db tokens create bluesbirds-academy       # -> DATABASE_AUTH_TOKEN
```

Set those two in `.env` (or your host's env config) and nothing else
changes — same binary, same migrations.

## API surface

All routes are under `/api/v1`. Auth is a `Bearer <jwt>` header, issued
by register/login.

| Method | Path | Role | Notes |
|---|---|---|---|
| POST | `/auth/register` | — | `{email, password, name, role}` |
| POST | `/auth/login` | — | `{email, password}` |
| POST | `/courses` | mentor/admin/academy_head | create course template + tasks in one call |
| GET | `/courses` | mentor/admin/academy_head | list the caller's own courses |
| GET | `/courses/{courseID}/tasks` | any authenticated | ordered task list |
| POST | `/enrollments` | mentor/admin/academy_head | enrolls a mentee, auto-spreads due dates |
| GET | `/enrollments/{enrollmentID}/progress` | mentee (own) / mentor / admin | feeds the mentee dashboard |
| POST | `/enrollments/{enrollmentID}/tasks/{taskID}/complete` | mentee (own) | the check-in mechanism |
| POST | `/enrollments/{enrollmentID}/flags` | mentee (own) | the "I'm stuck" button |
| POST | `/flags/{flagID}/resolve` | mentor/admin/academy_head | closes a flag |
| GET | `/dashboard/mentor` | mentor/admin/academy_head | one row per mentee, flagged-first |

Design decisions worth flagging:

- **Due-date auto-spread**: `POST /enrollments` takes an optional
  `days_per_task` (default `2`, roughly matching the "3 checks a week"
  cadence from the doc) and spreads task due dates evenly from today.
  Per-task adjustment after the fact isn't wired up yet — that's a
  `PATCH` on `enrollment_task_due_dates` you can add when the product
  need is clearer.
- **Mentor dashboard status**: derived, not stored — `flagged` if any
  open flag exists, else `behind_pace` if any task is past its due date
  with no completion, else `on_track`. Flagged mentees are sorted first
  to match the mockup.
- **Ownership checks are in the handlers, not a generic middleware**:
  each mentee-scoped route (`progress`, `complete`, `flags`) checks the
  enrollment's `mentee_id` against the JWT's subject inline. This was a
  deliberate call for the POC — a generic "does this JWT own this
  resource" middleware would need per-route lookup functions anyway, so
  the inline version is currently less code, at the cost of being
  slightly repetitive. Worth revisiting if more mentee-scoped resources
  get added.

## What's next

In rough order, matching "still open" in the design doc:

1. **Notifications** — decide the flag → notification trigger and the
   daily-digest email structure, then add an `internal/notify` package
   (Resend/SendGrid client) called from `CreateFlag` and a cron-style job
   for the daily digest.
2. **Community layer** — course communities are just a query over
   `enrollments` per the doc, so a `GET /courses/{id}/roster` endpoint is
   nearly free to add once the "is contact real messaging or just a
   roster" question is answered. Answering that question decides whether
   this is a two-hour endpoint or a much bigger build.
3. **Contracts & events** — new tables + CRUD, gated to `academy_head`
   for creation; open to all authenticated roles for browsing.
4. **Auth hardening** — token refresh/expiry handling, rate limiting on
   `/auth/login`, and tightening the CORS `AllowedOrigins` in
   `cmd/server/main.go` before this leaves POC stage.
