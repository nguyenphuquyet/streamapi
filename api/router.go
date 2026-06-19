package api

import (
	"telecloud/middleware"

	"github.com/gin-gonic/gin"
)

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

	// Share page (public)
	r.GET("/share/:token", HandleSharePage)

	// ══════════════════════════════════════════════════════════════════════════
	// STREAM - PUBLIC (cho phép web khác embed)
	// ══════════════════════════════════════════════════════════════════════════
	
	// Stream public - KHÔNG cần đăng nhập
	r.GET("/stream/:id", HandleStream)
	
	// Stream public qua token (share)
	r.GET("/stream/share/:token", HandleStreamByToken)
	
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
	uploadAPI := r.Group("/api/upload-api", middleware.RequireAPIToken())
	{
		uploadAPI.POST("/upload", HandleUploadAPI)
	}

	return r
}