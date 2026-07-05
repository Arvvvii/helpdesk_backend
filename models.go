package main

// =============================================================================
// MODELS — Definisi struct data untuk Helpdesk E-Ticketing v2.0.0
// Setiap struct merepresentasikan satu tabel di Supabase (public schema).
// =============================================================================

// Ticket merepresentasikan tiket helpdesk sesuai tabel public.tickets
type Ticket struct {
	ID            string `json:"id,omitempty"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Status        string `json:"status"`
	Priority      string `json:"priority,omitempty"`
	CreatedBy     string `json:"created_by"`
	AssignedTo    string `json:"assigned_to,omitempty"`
	AttachmentURL string `json:"attachment_url,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

// Comment merepresentasikan komentar pada tiket sesuai tabel public.comments
type Comment struct {
	ID        string `json:"id"`
	TicketID  string `json:"ticket_id"`
	AuthorID  string `json:"author_id"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at,omitempty"`
}

// TicketHistory merepresentasikan log riwayat aksi pada tiket
// sesuai tabel public.ticket_histories (BR-005: Fitur Tracking)
type TicketHistory struct {
	ID        string `json:"id"`
	TicketID  string `json:"ticket_id"`
	Action    string `json:"action"`
	ActorID   string `json:"actor_id"`
	CreatedAt string `json:"created_at,omitempty"`
}

// DashboardStats merepresentasikan statistik tiket untuk endpoint GET /dashboard/stats
type DashboardStats struct {
	Open       int `json:"open"`
	InProgress int `json:"in_progress"`
	Resolved   int `json:"resolved"`
	Total      int `json:"total"`
}

// CommentRequest adalah request body untuk POST /tickets/:id/comments
type CommentRequest struct {
	Content string `json:"content" binding:"required"`
}

// Profile merepresentasikan data profil user dari tabel public.profiles
// Digunakan oleh JWT middleware untuk lookup role aplikasi (user/helpdesk/admin)
type Profile struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
	IsActive bool   `json:"is_active"`
}
