package api

import (
	"net/http"

	"telecloud/middleware"

	"github.com/gin-gonic/gin"
)

// corsForUploadAPI cho phép web/app khác (origin khác) gọi Upload API.
// Bắt buộc vì request có header Authorization + multipart sẽ kích hoạt
// CORS preflight (OPTIONS) mà Gin không tự xử lý.
func corsForUploadAPI() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func SetupRouter() *gin.Engine {
	r := gin.Default()

	// Static files
	r.Static("/static", "./static")

	// Templates
	r.LoadHTMLGlob("templates/*.html")

	// ─── Public routes (không cần đăng nhập) ────────────────────────────────
	r.GET("/setup", HandleSetupPage)
	r.POST("/setup", HandleSetupSubmit)
	r.GET("/login", HandleLoginPage)
	r.POST("/login", HandleLoginSubmit)
	r.GET("/logout", HandleLogout)
	r.GET("/register", HandleRegisterPage)
	r.POST("/register", HandleRegisterSubmit)

	// Health check
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	// Share page (public)
	r.GET("/share/:token", HandleSharePage)

	// ══════════════════════════════════════════════════════════════════════════
	// STREAM - PUBLIC (cho phép web khác embed)
	// ══════════════════════════════════════════════════════════════════════════
	
	// Stream public - KHÔNG cần đăng nhập
	// Đăng ký thêm HEAD vì Gin không tự map HEAD vào handler GET; nhiều
	// video player gửi HEAD trước để lấy Content-Length/Accept-Ranges.
	r.GET("/stream/:id", HandleStream)
	r.HEAD("/stream/:id", HandleStream)
	
	// Stream public qua token (share)
	r.GET("/stream/share/:token", HandleStreamByToken)
	r.HEAD("/stream/share/:token", HandleStreamByToken)
	
	// OPTIONAL: Stream có signed URL (bảo mật hơn)
	// r.GET("/stream/signed/:id", HandleSignedStream)

	// ─── Auth required routes (cần đăng nhập) ──────────────────────────────
	auth := r.Group("/", middleware.RequireAuth())
	{
		auth.GET("/", HandleIndex)

		// File API
		auth.GET("/api/files", HandleAPIListFiles)
		auth.GET("/api/search", HandleAPISearch)
		auth.DELETE("/api/files/:id", HandleAPIDeleteFile)
		auth.POST("/api/files/:id/share", HandleAPIShareFile)
		auth.DELETE("/api/files/:id/share", HandleAPIUnshareFile)
		auth.POST("/api/files/:id/move", HandleAPIMoveFile)

		// Folder API
		auth.POST("/api/folders", HandleAPICreateFolder)
		auth.DELETE("/api/folders/:id", HandleAPIDeleteFolder)

		// Upload
		auth.POST("/api/upload", HandleUpload)
		auth.GET("/api/upload/progress/:uploadId", HandleUploadProgress)

		// Settings
		auth.GET("/api/settings", HandleAPIGetSettings)
		auth.POST("/api/settings/regenerate-token", HandleAPIRegenerateToken)
		auth.POST("/api/settings/change-password", HandleAPIChangePassword)
	}

	// ─── Upload API (Bearer token) ──────────────────────────────────────────
	uploadAPI := r.Group("/api/upload-api", corsForUploadAPI(), middleware.RequireAPIToken())
	{
		uploadAPI.POST("/upload", HandleUploadAPI)
	}
	// Đăng ký riêng route OPTIONS (preflight) — không qua RequireAPIToken vì
	// preflight không có header Authorization, chỉ cần corsForUploadAPI() trả
	// header CORS rồi abort sớm.
	r.OPTIONS("/api/upload-api/upload", corsForUploadAPI())

	// Tiến trình upload qua SSE cho Upload API.
	// Đặt NGOÀI group uploadAPI vì EventSource API của trình duyệt không gửi
	// được header Authorization, nên token được chấp nhận qua query string
	// (?token=...) — cùng cơ chế với WebSocket trước đây.
	r.GET("/api/upload-api/progress/:uploadId", corsForUploadAPI(), middleware.RequireAPITokenWS(), HandleUploadAPIProgressSSE)

	return r
}