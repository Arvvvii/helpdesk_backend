package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/supabase-community/supabase-go"
)

// =============================================================================
// JWT AUTH MIDDLEWARE
// =============================================================================

func JWTAuthMiddleware(jwtSecret string, supabaseClient *supabase.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Header Authorization diperlukan",
			})
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Format authorization tidak valid, gunakan: Bearer <token>",
			})
			return
		}

		// Validasi token langsung ke Supabase Auth API
		req, _ := http.NewRequest("GET", os.Getenv("SUPABASE_URL")+"/auth/v1/user", nil)
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("apikey", os.Getenv("SUPABASE_KEY"))

		httpClient := &http.Client{Timeout: 5 * time.Second}
		resp, err := httpClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Token tidak valid atau sudah expired",
			})
			return
		}
		defer resp.Body.Close()

		var userResp struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&userResp); err != nil || userResp.ID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Gagal membaca user data dari token",
			})
			return
		}

		userID := userResp.ID

		// 5. Query tabel profiles untuk mendapatkan role aplikasi
		//    Role di JWT Supabase hanya berisi "authenticated",
		//    sedangkan role aplikasi (user/helpdesk/admin) disimpan di tabel profiles.
		var profile Profile
		_, err = supabaseClient.From("profiles").
			Select("id, role, full_name, is_active", "exact", false).
			Eq("id", userID).
			Single().
			ExecuteTo(&profile)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "Profil user tidak ditemukan di database",
			})
			return
		}

		if !profile.IsActive {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "Akun Anda telah dinonaktifkan oleh Admin",
			})
			return
		}

		// 6. Simpan data user ke Gin context untuk digunakan oleh handler
		c.Set("user_id", userID)
		c.Set("user_role", profile.Role)
		c.Set("user_name", profile.FullName)

		c.Next()
	}
}

// =============================================================================
// ROLE GUARD MIDDLEWARE
// =============================================================================
// Middleware tambahan untuk membatasi akses endpoint berdasarkan role.
// Digunakan setelah JWTAuthMiddleware untuk endpoint yang memerlukan role spesifik.
//
// Contoh penggunaan:
//   router.DELETE("/tickets/:id", RoleGuard("admin"), DeleteTicket(client))
//   router.PUT("/tickets/:id", RoleGuard("admin", "helpdesk"), UpdateTicket(client))

func RoleGuard(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("user_role")
		if !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "Role tidak ditemukan dalam context",
			})
			return
		}

		roleStr, ok := role.(string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "Format role tidak valid",
			})
			return
		}

		// Cek apakah role user termasuk dalam daftar yang diizinkan
		for _, allowed := range allowedRoles {
			if roleStr == allowed {
				c.Next()
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "Akses ditolak: role '" + roleStr + "' tidak memiliki izin untuk endpoint ini",
		})
	}
}
