package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/bluesbirds/academy/internal/config"
	"github.com/bluesbirds/academy/internal/db"
	"github.com/bluesbirds/academy/internal/handlers"
	appmiddleware "github.com/bluesbirds/academy/internal/middleware"
	"github.com/bluesbirds/academy/internal/models"
	"github.com/bluesbirds/academy/internal/notify"
)

func main() {
	cfg := config.Load()

	conn, err := db.Connect(cfg.DatabaseURL, cfg.DatabaseAuth)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer conn.Close()

	if err := db.RunMigrations(conn, "migrations"); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	if err := db.RunMigrations(conn, "migrations"); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	notifyClient := notify.NewClient(cfg.ResendAPIKey, cfg.FromEmail)
	if !notifyClient.Enabled() {
		log.Println("RESEND_API_KEY not set, email sending is disabled")
	}

	authH := &handlers.AuthHandler{DB: conn, JWTSecret: cfg.JWTSecret}
	authH := &handlers.AuthHandler{DB: conn, JWTSecret: cfg.JWTSecret}
	courseH := &handlers.CourseHandler{DB: conn}
	enrollmentH := &handlers.EnrollmentHandler{DB: conn}
	completionH := &handlers.CompletionHandler{DB: conn}
		// was: flagH := &handlers.FlagHandler{DB: conn}
	flagH := &handlers.FlagHandler{DB: conn, Notify: notifyClient, AppBaseURL: cfg.AppBaseURL}

	// was: pageH := &handlers.PageHandler{DB: conn, JWTSecret: cfg.JWTSecret, Templates: handlers.LoadPageTemplates()}
	pageH := &handlers.PageHandler{
		DB: conn, JWTSecret: cfg.JWTSecret, Templates: handlers.LoadPageTemplates(),
		Notify: notifyClient, AppBaseURL: cfg.AppBaseURL,
	}
	dashboardH := &handlers.DashboardHandler{DB: conn}


	r := chi.NewRouter()
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"}, // tighten before this leaves POC stage -- see deployment notes
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
	}))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// The bare site URL: send everyone to /dashboard. Logged-in users land
	// on their dashboard; anyone else gets bounced to /login by the cookie
	// middleware guarding that route.
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})

	// Static assets for the server-rendered frontend (web/static/css/style.css etc).
	fileServer := http.FileServer(http.Dir("web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// --- Server-rendered pages (cookie session, not the JWT header) ---
	r.Get("/login", pageH.LoginForm)
	r.Post("/login", pageH.LoginSubmit)
	r.Get("/signup", pageH.SignupForm)
	r.Post("/signup", pageH.SignupSubmit)
	r.Get("/logout", pageH.Logout)

	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.RequireAuthCookie(cfg.JWTSecret))

		r.Get("/dashboard", pageH.Dashboard)
		r.Get("/enrollments/{enrollmentID}", pageH.EnrollmentDetail)
		r.Post("/enrollments/{enrollmentID}/tasks/{taskID}/complete", pageH.CompleteTaskPage)
		r.Post("/enrollments/{enrollmentID}/tasks/{taskID}/flag", pageH.CreateFlagPage)

		// Mentor-only page actions: creating/editing courses and enrolling mentees.
		// Same RequireRole middleware the JSON API uses below — it just
		// reads role from context, which both the cookie and header auth
		// middlewares populate the same way, so it works unchanged here.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireRole(models.RoleMentor, models.RoleAdmin, models.RoleAcademyHead))
			r.Get("/courses", pageH.CoursesList)
			r.Get("/courses/new", pageH.NewCourseForm)
			r.Post("/courses", pageH.CreateCourseSubmit)
			r.Get("/courses/{courseID}/edit", pageH.CourseEdit)
			r.Post("/courses/{courseID}/tasks", pageH.AddTasksToCourseSubmit)
			r.Get("/enrollments/new", pageH.NewEnrollmentForm)
			r.Post("/enrollments", pageH.CreateEnrollmentSubmit)
			r.Post("/mentor/enrollments/{enrollmentID}/resolve-flags", pageH.ResolveEnrollmentFlagsPage)
		})
	})

	// --- JSON API (unchanged — still JWT-over-header, for any future JS/mobile client) ---
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", authH.Register)
		r.Post("/auth/login", authH.Login)

		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAuth(cfg.JWTSecret))

			r.Group(func(r chi.Router) {
				r.Use(appmiddleware.RequireRole(models.RoleMentor, models.RoleAdmin, models.RoleAcademyHead))
				r.Post("/courses", courseH.CreateCourse)
				r.Get("/courses", courseH.ListCourses)
				r.Post("/enrollments", enrollmentH.CreateEnrollment)
				r.Get("/dashboard/mentor", dashboardH.MentorDashboard)
				r.Post("/flags/{flagID}/resolve", flagH.ResolveFlag)
			})

			r.Get("/courses/{courseID}/tasks", courseH.GetCourseTasks)
			r.Get("/enrollments/{enrollmentID}/progress", enrollmentH.GetMenteeProgress)
			r.Post("/enrollments/{enrollmentID}/tasks/{taskID}/complete", completionH.MarkTaskDone)
			r.Post("/enrollments/{enrollmentID}/flags", flagH.CreateFlag)
		})
	})

	log.Printf("bluesbirds academy backend listening on :%s (env=%s)", cfg.Port, cfg.Env)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatal(err)
	}
}