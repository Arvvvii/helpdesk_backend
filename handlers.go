package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/supabase-community/supabase-go"
)

// =============================================================================
// TICKET HANDLERS — CRUD Operations dengan RBAC
// =============================================================================

// GetTickets mengambil daftar tiket dengan RBAC filtering (FR-006).
//
// Logika RBAC:
//   - Role 'user'     → hanya tiket yang dibuat sendiri (created_by = user_id)
//   - Role 'helpdesk' → hanya tiket yang di-assign ke dirinya (assigned_to = user_id)
//   - Role 'admin'    → seluruh tiket tanpa filter
func GetTickets(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		role := c.GetString("user_role")

		var tickets []Ticket

		// Buat query dasar
		query := client.From("tickets").Select("*", "exact", false)

		// Terapkan RBAC filtering berdasarkan role
		roleLower := strings.ToLower(strings.TrimSpace(role))
		switch roleLower {
		case "user":
			// User hanya melihat tiket yang dibuat sendiri
			query = query.Eq("created_by", userID)
		case "helpdesk":
			// Helpdesk hanya melihat tiket yang di-assign kepadanya
			query = query.Eq("assigned_to", userID)
		case "admin":
			// Admin melihat semua tiket — tidak ada filter
		default:
			// Fallback aman: jika role tidak dikenali, berlakukan batasan "user"
			query = query.Eq("created_by", userID)
		}

		_, err := query.ExecuteTo(&tickets)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Pastikan selalu return array kosong, bukan null
		if tickets == nil {
			tickets = []Ticket{}
		}

		// Urutkan berdasarkan created_at descending (terbaru di atas)
		sort.Slice(tickets, func(i, j int) bool {
			return tickets[i].CreatedAt > tickets[j].CreatedAt
		})

		c.JSON(http.StatusOK, tickets)
	}
}

// GetTicketByID mengambil detail satu tiket berdasarkan ID.
func GetTicketByID(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		var ticket Ticket
		_, err := client.From("tickets").
			Select("*", "exact", false).
			Eq("id", id).
			Single().
			ExecuteTo(&ticket)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tiket tidak ditemukan"})
			return
		}

		c.JSON(http.StatusOK, ticket)
	}
}

// CreateTicket membuat tiket baru (FR-005) dan mencatat history pembuatan (BR-005).
func CreateTicket(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")

		var newTicket Ticket
		if err := c.ShouldBindJSON(&newTicket); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Override created_by dari JWT token (mencegah spoofing user ID)
		newTicket.CreatedBy = userID

		// Default status untuk tiket baru adalah 'open'
		if newTicket.Status == "" {
			newTicket.Status = "open"
		}

		var result []Ticket
		_, err := client.From("tickets").
			Insert(newTicket, false, "", "", "").
			ExecuteTo(&result)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if len(result) == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat tiket"})
			return
		}

		// Catat ke ticket_histories bahwa tiket baru dibuat (BR-005: Tracking)
		history := map[string]interface{}{
			"ticket_id": result[0].ID,
			"action":    "Ticket created with status 'open'",
			"actor_id":  userID,
		}
		client.From("ticket_histories").Insert(history, false, "", "", "").Execute()

		c.JSON(http.StatusCreated, result[0])
	}
}

// UpdateTicket mengupdate data tiket dan secara otomatis mencatat
// perubahan status ke tabel ticket_histories (BR-005: Tracking).
//
// Logika History:
//   - Jika field 'status' berubah → catat "Status changed from 'X' to 'Y'"
//   - Jika field 'assigned_to' berubah → catat "Ticket assigned to 'Z'"
func UpdateTicket(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		userID := c.GetString("user_id")

		var updateData map[string]interface{}
		if err := c.ShouldBindJSON(&updateData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Ambil data tiket saat ini untuk mendeteksi perubahan
		var currentTicket Ticket
		_, err := client.From("tickets").
			Select("*", "", false).
			Eq("id", id).
			Single().
			ExecuteTo(&currentTicket)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tiket tidak ditemukan"})
			return
		}

		oldStatus := currentTicket.Status
		oldAssignee := currentTicket.AssignedTo

		// Lakukan update pada tabel tickets
		var result []Ticket
		_, err = client.From("tickets").
			Update(updateData, "", "").
			Eq("id", id).
			ExecuteTo(&result)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if len(result) == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengupdate tiket"})
			return
		}

		// === AUTO-LOG KE TICKET_HISTORIES (BR-005) ===

		// Log perubahan status
		if newStatus, ok := updateData["status"].(string); ok && newStatus != oldStatus {
			history := map[string]interface{}{
				"ticket_id": id,
				"action":    fmt.Sprintf("Status changed from '%s' to '%s'", oldStatus, newStatus),
				"actor_id":  userID,
			}
			client.From("ticket_histories").Insert(history, false, "", "", "").Execute()
		}

		// Log perubahan assigned_to
		if newAssignee, ok := updateData["assigned_to"].(string); ok && newAssignee != oldAssignee {
			history := map[string]interface{}{
				"ticket_id": id,
				"action":    fmt.Sprintf("Ticket assigned to '%s'", newAssignee),
				"actor_id":  userID,
			}
			client.From("ticket_histories").Insert(history, false, "", "", "").Execute()
		}

		c.JSON(http.StatusOK, result[0])
	}
}

// DeleteTicket menghapus tiket dari database (Admin only).
// Endpoint ini dilindungi oleh RoleGuard("admin") di router.
// Cascade delete akan otomatis menghapus comments dan histories terkait.
func DeleteTicket(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		// Verifikasi tiket ada sebelum dihapus
		var existing Ticket
		_, err := client.From("tickets").
			Select("id", "", false).
			Eq("id", id).
			Single().
			ExecuteTo(&existing)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tiket tidak ditemukan"})
			return
		}

		// Hapus tiket (ON DELETE CASCADE akan menghapus data terkait)
		var deleted []Ticket
		_, err = client.From("tickets").
			Delete("", "").
			Eq("id", id).
			ExecuteTo(&deleted)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Tiket berhasil dihapus",
			"id":      id,
		})
	}
}

// =============================================================================
// COMMENT HANDLERS — Interaksi dengan tabel public.comments
// =============================================================================

// GetComments mengambil semua komentar untuk sebuah tiket, diurutkan kronologis.
func GetComments(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ticketID := c.Param("id")

		var comments []Comment
		_, err := client.From("comments").
			Select("*", "exact", false).
			Eq("ticket_id", ticketID).
			ExecuteTo(&comments)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if comments == nil {
			comments = []Comment{}
		}

		// Urutkan berdasarkan created_at ascending (kronologis)
		sort.Slice(comments, func(i, j int) bool {
			return comments[i].CreatedAt < comments[j].CreatedAt
		})

		c.JSON(http.StatusOK, comments)
	}
}

// CreateComment menambahkan komentar pada tiket dan mencatat aksi ke ticket_histories.
func CreateComment(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ticketID := c.Param("id")
		userID := c.GetString("user_id")
		userName := c.GetString("user_name")

		var req CommentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Verifikasi tiket ada
		var existing Ticket
		_, err := client.From("tickets").
			Select("id", "", false).
			Eq("id", ticketID).
			Single().
			ExecuteTo(&existing)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tiket tidak ditemukan"})
			return
		}

		// Insert komentar ke tabel comments
		commentData := map[string]interface{}{
			"ticket_id": ticketID,
			"author_id": userID,
			"content":   req.Content,
		}

		var result []Comment
		_, err = client.From("comments").
			Insert(commentData, false, "", "", "").
			ExecuteTo(&result)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if len(result) == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menambahkan komentar"})
			return
		}

		// Catat ke ticket_histories (BR-005: Tracking)
		displayName := userName
		if displayName == "" {
			displayName = userID
		}
		history := map[string]interface{}{
			"ticket_id": ticketID,
			"action":    fmt.Sprintf("Comment added by %s", displayName),
			"actor_id":  userID,
		}
		client.From("ticket_histories").Insert(history, false, "", "", "").Execute()

		c.JSON(http.StatusCreated, result[0])
	}
}

// =============================================================================
// HISTORY/TRACKING HANDLERS (BR-005)
// =============================================================================

// GetTicketHistories mengambil riwayat aksi tiket untuk fitur Tracking.
// Data diurutkan kronologis (terlama di atas) untuk visualisasi timeline.
func GetTicketHistories(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ticketID := c.Param("id")

		var histories []TicketHistory
		_, err := client.From("ticket_histories").
			Select("*", "exact", false).
			Eq("ticket_id", ticketID).
			ExecuteTo(&histories)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if histories == nil {
			histories = []TicketHistory{}
		}

		// Urutkan berdasarkan created_at ascending (timeline kronologis)
		sort.Slice(histories, func(i, j int) bool {
			return histories[i].CreatedAt < histories[j].CreatedAt
		})

		c.JSON(http.StatusOK, histories)
	}
}

// =============================================================================
// DASHBOARD HANDLERS
// =============================================================================

// GetDashboardStats mengembalikan statistik jumlah tiket berdasarkan status.
// Data di-filter sesuai RBAC sehingga setiap role hanya melihat statistik
// dari tiket yang menjadi tanggung jawabnya.
//
// Response:
//
//	{
//	  "open": 5,
//	  "in_progress": 3,
//	  "resolved": 12,
//	  "total": 20
//	}
func GetDashboardStats(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		role := c.GetString("user_role")

		// Ambil hanya kolom status untuk efisiensi (tidak perlu seluruh kolom)
		query := client.From("tickets").Select("status", "", false)

		// Terapkan RBAC filtering (sama seperti GetTickets)
		roleLower := strings.ToLower(strings.TrimSpace(role))
		switch roleLower {
		case "user":
			query = query.Eq("created_by", userID)
		case "helpdesk":
			query = query.Eq("assigned_to", userID)
		case "admin":
			// Admin melihat statistik seluruh tiket
		default:
			// Fallback aman
			query = query.Eq("created_by", userID)
		}

		var tickets []Ticket
		_, err := query.ExecuteTo(&tickets)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Hitung statistik berdasarkan status
		stats := DashboardStats{}
		for _, t := range tickets {
			switch t.Status {
			case "open":
				stats.Open++
			case "in_progress":
				stats.InProgress++
			case "resolved":
				stats.Resolved++
			}
		}
		stats.Total = stats.Open + stats.InProgress + stats.Resolved

		c.JSON(http.StatusOK, stats)
	}
}

// =============================================================================
// USER MANAGEMENT HANDLERS (ADMIN ONLY)
// =============================================================================

// GetAllUsers mengambil daftar semua pengguna.
// Endpoint ini dilindungi oleh RoleGuard("admin") di router.
func GetAllUsers(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		var profiles []Profile
		_, err := client.From("profiles").
			Select("*", "exact", false).
			ExecuteTo(&profiles)
		
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if profiles == nil {
			profiles = []Profile{}
		}

		// Urutkan berdasarkan full_name
		sort.Slice(profiles, func(i, j int) bool {
			return profiles[i].FullName < profiles[j].FullName
		})

		c.JSON(http.StatusOK, profiles)
	}
}

// ToggleUserStatus mengupdate status is_active seorang pengguna.
// Endpoint ini dilindungi oleh RoleGuard("admin") di router.
func ToggleUserStatus(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		var updateData struct {
			IsActive bool `json:"is_active"`
		}

		if err := c.ShouldBindJSON(&updateData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var result []Profile
		_, err := client.From("profiles").
			Update(map[string]interface{}{"is_active": updateData.IsActive}, "", "").
			Eq("id", id).
			ExecuteTo(&result)
			
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if len(result) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Pengguna tidak ditemukan"})
			return
		}

		c.JSON(http.StatusOK, result[0])
	}
}

// GetHelpdeskUsers mengembalikan daftar pengguna yang memiliki role 'helpdesk'.
func GetHelpdeskUsers(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		var profiles []Profile
		_, err := client.From("profiles").Select("*", "", false).Eq("role", "helpdesk").ExecuteTo(&profiles)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, profiles)
	}
}

// ResetPasswordRequest adalah body untuk endpoint reset password
type ResetPasswordRequest struct {
	Email       string `json:"email" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// ResetPassword melakukan bypass reset password menggunakan Supabase Admin API.
func ResetPassword(client *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ResetPasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Email dan password baru wajib diisi"})
			return
		}

		var profiles []Profile
		_, err := client.From("profiles").Select("id", "", false).Eq("username", req.Email).ExecuteTo(&profiles)
		if err != nil || len(profiles) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Pengguna dengan email tersebut tidak ditemukan"})
			return
		}

		userID := profiles[0].ID
		supabaseURL := os.Getenv("SUPABASE_URL")
		supabaseKey := os.Getenv("SUPABASE_KEY") // SERVICE_ROLE_KEY

		adminURL := supabaseURL + "/auth/v1/admin/users/" + userID
		payload, _ := json.Marshal(map[string]string{
			"password": req.NewPassword,
		})

		httpReq, _ := http.NewRequest("PUT", adminURL, bytes.NewBuffer(payload))
		httpReq.Header.Add("apikey", supabaseKey)
		httpReq.Header.Add("Authorization", "Bearer "+supabaseKey)
		httpReq.Header.Add("Content-Type", "application/json")

		httpClient := &http.Client{}
		resp, err := httpClient.Do(httpReq)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghubungi Supabase Auth API: " + err.Error()})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			c.JSON(resp.StatusCode, gin.H{"error": "Gagal mereset password di Supabase: " + string(bodyBytes)})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Password berhasil di-reset"})
	}
}
