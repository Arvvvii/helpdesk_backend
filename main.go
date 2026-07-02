package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/supabase-community/supabase-go"
)

// =============================================================================
// MAIN — Entry point Helpdesk E-Ticketing Backend v2.0.0
// =============================================================================
// Perubahan dari v1.0.0:
//   - Ditambahkan JWT Auth Middleware untuk proteksi seluruh endpoint (NFR 4.5)
//   - GET /tickets di-refactor dengan RBAC filtering berdasarkan role
//   - Endpoint baru: DELETE /tickets/:id (Admin only)
//   - Endpoint baru: GET/POST /tickets/:id/comments
//   - Endpoint baru: GET /tickets/:id/histories (BR-005: Tracking)
//   - Endpoint baru: GET /dashboard/stats
//   - PUT /tickets/:id otomatis mencatat perubahan status ke ticket_histories

func main() {
	// Load environment variables dari file .env (hanya berfungsi di lokal)
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: File .env tidak ditemukan, menggunakan environment variables sistem")
	}

	// ================================================================
	// KONFIGURASI
	// ================================================================
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_KEY")
	jwtSecret := os.Getenv("SUPABASE_JWT_SECRET")
	port := os.Getenv("PORT")

	if supabaseURL == "" || supabaseKey == "" {
		log.Fatal("FATAL: SUPABASE_URL dan SUPABASE_KEY wajib diisi di environment")
	}
	if jwtSecret == "" {
		log.Fatal("FATAL: SUPABASE_JWT_SECRET wajib diisi di environment")
	}
	
	// Konfigurasi Port Dinamis untuk Dewacloud
	if port == "" {
		port = "8080"
	}
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	// ================================================================
	// INISIALISASI SUPABASE CLIENT
	// ================================================================
	client, err := supabase.NewClient(supabaseURL, supabaseKey, nil)
	if err != nil {
		log.Fatalf("Gagal menginisialisasi Supabase client: %v", err)
	}

	r := gin.Default()

	// ================================================================
	// PUBLIC ROUTES — Tidak memerlukan autentikasi
	// ================================================================
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Backend Helpdesk v2.0.0 Ready!",
			"version": "2.0.0",
		})
	})

	// Endpoint khusus bypass Reset Password (Public karena user lupa password)
	r.POST("/users/reset-password", ResetPassword(client))

	// ================================================================
	// PROTECTED ROUTES — Memerlukan JWT Supabase yang valid
	// ================================================================
	auth := r.Group("/")
	auth.Use(JWTAuthMiddleware(jwtSecret, client))
	{
		auth.GET("/tickets", GetTickets(client))
		auth.GET("/tickets/:id", GetTicketByID(client))
		auth.POST("/tickets", CreateTicket(client))
		auth.PUT("/tickets/:id", UpdateTicket(client))
		auth.DELETE("/tickets/:id", RoleGuard("admin"), DeleteTicket(client))

		auth.GET("/tickets/:id/comments", GetComments(client))
		auth.POST("/tickets/:id/comments", CreateComment(client))

		auth.GET("/tickets/:id/histories", GetTicketHistories(client))

		auth.GET("/dashboard/stats", GetDashboardStats(client))

		auth.GET("/users", RoleGuard("admin"), GetAllUsers(client))
		auth.GET("/users/helpdesk", RoleGuard("admin"), GetHelpdeskUsers(client))
		auth.PATCH("/users/:id/status", RoleGuard("admin"), ToggleUserStatus(client))
	}

	// ================================================================
	// JALANKAN SERVER
	// ================================================================
	log.Printf("🚀 Helpdesk Backend v2.0.0 running on port %s", port)
	r.Run(port) // Format port sudah ditangani di atas
}