package middleware

import (
	"net/http"
	"strings"

	"telecloud/database"

	"github.com/gin-gonic/gin"
)

const UserKey = "user"
const SessionCookieName = "tc_session"

// RequireAuth kiểm tra session cookie hoặc Basic auth
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Kiểm tra session token từ cookie
		token, err := c.Cookie(SessionCookieName)
		if err != nil || token == "" {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		user, err := database.GetUserByAPIToken(token)
		if err != nil || user == nil {
			c.SetCookie(SessionCookieName, "", -1, "/", "", false, true)
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		c.Set(UserKey, user)
		c.Next()
	}
}

// RequireAPIToken kiểm tra Bearer token cho Upload API
func RequireAPIToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Thiếu Authorization header"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Format Authorization không hợp lệ. Dùng: Bearer <token>"})
			c.Abort()
			return
		}

		token := strings.TrimSpace(parts[1])
		user, err := database.GetUserByAPIToken(token)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token không hợp lệ"})
			c.Abort()
			return
		}

		c.Set(UserKey, user)
		c.Next()
	}
}

// GetUser lấy user hiện tại từ context
func GetUser(c *gin.Context) *database.User {
	if u, exists := c.Get(UserKey); exists {
		if user, ok := u.(*database.User); ok {
			return user
		}
	}
	return nil
}
