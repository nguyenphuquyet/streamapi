package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"telecloud/config"
	"telecloud/database"
	"telecloud/middleware"
	"telecloud/tgclient"
	"telecloud/utils"

	"github.com/gin-gonic/gin"
)

// ─── Auth Handlers ────────────────────────────────────────────────────────────

func HandleSetupPage(c *gin.Context) {
	count, _ := database.CountUsers()
	if count > 0 {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	c.HTML(http.StatusOK, "setup.html", gin.H{"title": "Thiết lập TeleCloud"})
}

func HandleSetupSubmit(c *gin.Context) {
	count, _ := database.CountUsers()
	if count > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "Đã có admin"})
		return
	}

	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")

	if username == "" || len(password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username và mật khẩu (ít nhất 6 ký tự) là bắt buộc"})
		return
	}

	hash, err := utils.HashPassword(password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Lỗi hệ thống"})
		return
	}

	apiToken := utils.GenerateToken(32)
	_, err = database.InsertUser(&database.User{
		Username:     username,
		PasswordHash: hash,
		IsAdmin:      true,
		APIToken:     apiToken,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Lỗi tạo tài khoản: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tạo tài khoản thành công"})
}

func HandleLoginPage(c *gin.Context) {
	count, _ := database.CountUsers()
	if count == 0 {
		c.Redirect(http.StatusFound, "/setup")
		return
	}
	c.HTML(http.StatusOK, "login.html", gin.H{"title": "Đăng nhập TeleCloud"})
}

func HandleLoginSubmit(c *gin.Context) {
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")

	user, err := database.GetUserByUsername(username)
	if err != nil || !utils.CheckPassword(password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Tên đăng nhập hoặc mật khẩu không đúng"})
		return
	}

	c.SetCookie(middleware.SessionCookieName, user.APIToken, 86400*30, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "Đăng nhập thành công"})
}

func HandleRegisterPage(c *gin.Context) {
	count, _ := database.CountUsers()
	if count == 0 {
		c.Redirect(http.StatusFound, "/setup")
		return
	}
	c.HTML(http.StatusOK, "register.html", gin.H{"title": "Đăng ký TeleCloud"})
}

func HandleRegisterSubmit(c *gin.Context) {
	count, _ := database.CountUsers()
	if count == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "Chưa có admin, hãy dùng /setup"})
		return
	}

	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")

	if username == "" || len(password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username và mật khẩu (ít nhất 6 ký tự) là bắt buộc"})
		return
	}

	if existing, err := database.GetUserByUsername(username); err == nil && existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Tên đăng nhập đã được sử dụng"})
		return
	}

	hash, err := utils.HashPassword(password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Lỗi hệ thống"})
		return
	}

	apiToken := utils.GenerateToken(32)
	_, err = database.InsertUser(&database.User{
		Username:     username,
		PasswordHash: hash,
		IsAdmin:      false,
		APIToken:     apiToken,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Lỗi tạo tài khoản: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Đăng ký thành công"})
}

func HandleLogout(c *gin.Context) {
	c.SetCookie(middleware.SessionCookieName, "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}

// ─── File Browser ─────────────────────────────────────────────────────────────

func HandleIndex(c *gin.Context) {
	count, _ := database.CountUsers()
	if count == 0 {
		c.Redirect(http.StatusFound, "/setup")
		return
	}
	c.HTML(http.StatusOK, "index.html", gin.H{"title": "TeleCloud"})
}

// FileDTO là định dạng JSON dùng chung cho mọi API trả về danh sách file
type FileDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	SizeStr    string `json:"size_str"`
	MimeType   string `json:"mime_type"`
	IsVideo    bool   `json:"is_video"`
	Duration   int    `json:"duration"`
	ThumbURL   string `json:"thumb_url"`
	ShareToken string `json:"share_token"`
	UploadedAt string `json:"uploaded_at"`
}

func toFileDTO(f *database.FileRecord) FileDTO {
	dto := FileDTO{
		ID:         f.ID,
		Name:       f.OriginalName,
		Size:       f.Size,
		SizeStr:    utils.FormatSize(f.Size),
		MimeType:   f.MimeType,
		IsVideo:    f.IsVideo,
		Duration:   f.Duration,
		ShareToken: f.ShareToken,
		UploadedAt: utils.FormatTime(f.UploadedAt),
	}
	return dto
}

func toFileDTOs(files []*database.FileRecord) []FileDTO {
	dtos := make([]FileDTO, 0, len(files))
	for _, f := range files {
		dtos = append(dtos, toFileDTO(f))
	}
	return dtos
}

func HandleAPIListFiles(c *gin.Context) {
	user := middleware.GetUser(c)
	path := utils.CleanPath(c.Query("path"))

	files, err := database.ListFiles(user.ID, path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	folders, err := database.ListFolders(user.ID, path)
	if err != nil {
		folders = []*database.FolderRecord{}
	}

	type FolderDTO struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Path      string `json:"path"`
		FullPath  string `json:"full_path"`
		CreatedAt string `json:"created_at"`
	}
	folderDTOs := make([]FolderDTO, 0, len(folders))
	for _, f := range folders {
		folderDTOs = append(folderDTOs, FolderDTO{
			ID:        f.ID,
			Name:      f.Name,
			Path:      f.Path,
			FullPath:  f.FullPath,
			CreatedAt: utils.FormatTime(f.CreatedAt),
		})
	}

	c.JSON(http.StatusOK, gin.H{"files": toFileDTOs(files), "folders": folderDTOs, "path": path})
}

// HandleAPICreateFolder tạo thư mục mới
func HandleAPICreateFolder(c *gin.Context) {
	user := middleware.GetUser(c)
	parentPath := utils.CleanPath(c.PostForm("path"))
	name := strings.TrimSpace(c.PostForm("name"))

	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tên thư mục không được trống"})
		return
	}
	for _, ch := range []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"} {
		name = strings.ReplaceAll(name, ch, "")
	}
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tên thư mục chứa ký tự không hợp lệ"})
		return
	}

	var fullPath string
	if parentPath == "/" {
		fullPath = "/" + name
	} else {
		fullPath = parentPath + "/" + name
	}

	if _, err := database.GetFolderByFullPath(user.ID, fullPath); err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Thư mục đã tồn tại"})
		return
	}

	id, err := database.InsertFolder(&database.FolderRecord{
		UserID:   user.ID,
		Name:     name,
		Path:     parentPath,
		FullPath: fullPath,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "name": name, "full_path": fullPath})
}

// HandleAPIDeleteFolder xóa thư mục (và toàn bộ nội dung bên trong)
func HandleAPIDeleteFolder(c *gin.Context) {
	user := middleware.GetUser(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID không hợp lệ"})
		return
	}

	row := database.DB.QueryRow(`SELECT full_path FROM folders WHERE id=? AND user_id=?`, id, user.ID)
	var fullPath string
	if err := row.Scan(&fullPath); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy thư mục"})
		return
	}

	if err := database.DeleteFolderRecursive(user.ID, fullPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Đã xóa thư mục"})
}

// HandleAPIMoveFile di chuyển file sang thư mục khác
func HandleAPIMoveFile(c *gin.Context) {
	user := middleware.GetUser(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID không hợp lệ"})
		return
	}

	// Kiểm tra file thuộc về user này
	if _, err := database.GetFileByIDAndUser(id, user.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy file"})
		return
	}

	newPath := utils.CleanPath(c.PostForm("path"))
	if newPath != "/" {
		if _, err := database.GetFolderByFullPath(user.ID, newPath); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Thư mục đích không tồn tại"})
			return
		}
	}

	if err := database.MoveFileToFolder(id, newPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Đã di chuyển file"})
}

func HandleAPISearch(c *gin.Context) {
	user := middleware.GetUser(c)
	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Thiếu query"})
		return
	}

	files, err := database.SearchFiles(user.ID, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": toFileDTOs(files)})
}

func HandleAPIDeleteFile(c *gin.Context) {
	user := middleware.GetUser(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID không hợp lệ"})
		return
	}

	// Kiểm tra file thuộc về user này
	if _, err := database.GetFileByIDAndUser(id, user.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy file"})
		return
	}

	if err := database.DeleteFile(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Đã xóa file"})
}

func HandleAPIShareFile(c *gin.Context) {
	user := middleware.GetUser(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID không hợp lệ"})
		return
	}

	file, err := database.GetFileByIDAndUser(id, user.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy file"})
		return
	}

	if file.ShareToken == "" {
		token := utils.GenerateToken(16)
		database.UpdateFileShareToken(id, token)
		file.ShareToken = token
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	host := c.Request.Host
	shareURL := fmt.Sprintf("%s://%s/share/%s", scheme, host, file.ShareToken)
	directURL := fmt.Sprintf("%s://%s/stream/%d", scheme, host, file.ID)

	c.JSON(http.StatusOK, gin.H{
		"share_url":  shareURL,
		"direct_url": directURL,
		"token":      file.ShareToken,
	})
}

func HandleAPIUnshareFile(c *gin.Context) {
	user := middleware.GetUser(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID không hợp lệ"})
		return
	}
	// Kiểm tra quyền sở hữu trước khi unshare
	if _, err := database.GetFileByIDAndUser(id, user.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy file"})
		return
	}
	database.UpdateFileShareToken(id, "")
	c.JSON(http.StatusOK, gin.H{"message": "Đã tắt chia sẻ"})
}

// ─── Upload Handler ───────────────────────────────────────────────────────────

func HandleUpload(c *gin.Context) {
	user := middleware.GetUser(c)
	cfg := config.App
	path := utils.CleanPath(c.PostForm("path"))
	uploadID := c.PostForm("uploadId")

	if cfg.MaxUploadSizeMB > 0 {
		maxBytes := cfg.MaxUploadSizeMB * 1024 * 1024
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
	}

	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Không thể parse form: " + err.Error()})
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Không có file nào được gửi"})
		return
	}

	type UploadResult struct {
		Name    string `json:"name"`
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
		ID      int64  `json:"id,omitempty"`
	}

	var st *progressState
	if uploadID != "" {
		st = hub.newProgressState(uploadID)
	}

	results := make([]UploadResult, 0, len(files))

	for _, fileHeader := range files {
		result, id, err := processUpload(c.Request.Context(), user.ID, fileHeader, path, st)
		r := UploadResult{Name: fileHeader.Filename}
		if err != nil {
			r.Error = err.Error()
			log.Printf("[upload] Lỗi upload %s: %v", fileHeader.Filename, err)
			if st != nil {
				st.finish("error", err.Error())
			}
		} else {
			r.Success = true
			r.ID = id
			_ = result
			if st != nil {
				st.finish("done", "Hoàn thành")
			}
		}
		results = append(results, r)
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

func processUpload(ctx context.Context, userID int64, fileHeader *multipart.FileHeader, path string, st *progressState) (*database.FileRecord, int64, error) {
	cfg := config.App

	f, err := fileHeader.Open()
	if err != nil {
		return nil, 0, fmt.Errorf("không thể mở file: %w", err)
	}
	defer f.Close()

	magic := make([]byte, 512)
	n, _ := f.Read(magic)
	f.Seek(0, io.SeekStart)

	mimeType := utils.DetectMimeType(magic[:n], fileHeader.Filename)
	originalName := utils.SafeFilename(fileHeader.Filename)
	safeName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), originalName)
	isVideo := utils.IsVideoMime(mimeType)

	var tmpPath string
	var tmpFile *os.File
	var duration, width, height int

	if isVideo && cfg.FFmpegPath != "disabled" {
		tmpPath = filepath.Join(cfg.TempDir, safeName)

		if err := saveTempFile(f, tmpPath); err == nil {
			tmpFile, err = os.Open(tmpPath)
			if err == nil {
				duration, width, height = utils.GetVideoInfo(cfg.FFmpegPath, tmpPath)
				f.Seek(0, io.SeekStart)
			} else {
				log.Printf("[upload] Warning: Không thể mở temp file %s: %v", tmpPath, err)
			}
		} else {
			log.Printf("[upload] Warning: Không thể tạo temp file %s: %v", tmpPath, err)
		}
	}

	defer func() {
		if tmpPath != "" {
			if tmpFile != nil {
				tmpFile.Close()
			}
			if err := os.Remove(tmpPath); err != nil {
				if !os.IsNotExist(err) {
					log.Printf("[upload] LỖI: Không thể xóa temp file %s: %v", tmpPath, err)
				}
			} else {
				log.Printf("[upload] Đã xóa temp file: %s", filepath.Base(tmpPath))
			}
		}
	}()

	var reader io.Reader = f
	if tmpFile != nil {
		if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
			log.Printf("[upload] Warning: Không thể seek temp file: %v", err)
			f.Seek(0, io.SeekStart)
			reader = f
		} else {
			reader = tmpFile
		}
	} else {
		f.Seek(0, io.SeekStart)
	}

	if st != nil {
		st.set("telegram", 0, "Đang gửi lên Telegram...")
	}
	result, err := tgclient.TG.UploadFile(ctx, reader, safeName, fileHeader.Size, mimeType, func(read, total int64) {
		if st == nil || total <= 0 {
			return
		}
		pct := int(read * 100 / total)
		if pct > 99 {
			pct = 99
		}
		st.set("telegram", pct, "Đang gửi lên Telegram...")
	})
	if err != nil {
		return nil, 0, err
	}

	record := &database.FileRecord{
		UserID:       userID,
		Name:         safeName,
		OriginalName: originalName,
		Size:         fileHeader.Size,
		MimeType:     mimeType,
		Path:         path,
		MessageID:    result.MessageID,
		ChatID:       result.ChatID,
		FileID:       result.FileID,
		AccessHash:   result.AccessHash,
		FileRef:      result.FileRef,
		IsVideo:      isVideo,
		Duration:     duration,
		Width:        width,
		Height:       height,
	}

	id, err := database.InsertFile(record)
	if err != nil {
		return nil, 0, err
	}
	record.ID = id

	log.Printf("[upload] OK: %s (%s), message_id=%d", originalName, utils.FormatSize(fileHeader.Size), result.MessageID)
	return record, id, nil
}

func saveTempFile(r io.Reader, path string) error {
	os.MkdirAll(filepath.Dir(path), 0755)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

// ─── Stream Handler ───────────────────────────────────────────────────────────

func HandleStream(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID không hợp lệ"})
		return
	}

	file, err := database.GetFileByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy file"})
		return
	}

	serveFileStream(c, file)
}

func HandleStreamByToken(c *gin.Context) {
	token := c.Param("token")
	file, err := database.GetFileByShareToken(token)
	if err != nil || file == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Link không hợp lệ hoặc đã hết hạn"})
		return
	}
	serveFileStream(c, file)
}

func serveFileStream(c *gin.Context, file *database.FileRecord) {
	rangeHeader := c.GetHeader("Range")
	var start, end int64
	totalSize := file.Size

	if rangeHeader != "" {
		rangeHeader = strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.SplitN(rangeHeader, "-", 2)
		if len(parts) == 2 {
			start, _ = strconv.ParseInt(parts[0], 10, 64)
			if parts[1] != "" {
				end, _ = strconv.ParseInt(parts[1], 10, 64)
			} else {
				end = start + 2*1024*1024 - 1
				if end >= totalSize {
					end = totalSize - 1
				}
			}
		}
	} else {
		end = totalSize - 1
	}

	if start < 0 || start >= totalSize || end < start {
		c.Header("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	reader, length, err := tgclient.TG.DownloadFileRange(
		c.Request.Context(),
		file.ChatID,
		file.MessageID,
		file.FileRef,
		file.AccessHash,
		start,
		end,
	)
	if err != nil {
		log.Printf("[stream] Lỗi download file %d: %v", file.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Lỗi stream file"})
		return
	}
	defer reader.Close()

	statusCode := http.StatusOK
	if rangeHeader != "" {
		statusCode = http.StatusPartialContent
	}

	c.Header("Content-Type", file.MimeType)
	c.Header("Content-Length", strconv.FormatInt(length, 10))
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, file.OriginalName))
	if rangeHeader != "" {
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize))
	}
	c.Header("Cache-Control", "no-store")

	c.Status(statusCode)
	io.Copy(c.Writer, reader)
}

// ─── Share Page ───────────────────────────────────────────────────────────────

func HandleSharePage(c *gin.Context) {
	token := c.Param("token")
	file, err := database.GetFileByShareToken(token)
	if err != nil || file == nil || file.ShareToken == "" {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"title":   "Không tìm thấy",
			"message": "Link chia sẻ không hợp lệ hoặc đã bị xóa",
		})
		return
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	streamURL := fmt.Sprintf("%s://%s/stream/%d", scheme, c.Request.Host, file.ID)

	c.HTML(http.StatusOK, "share.html", gin.H{
		"title":     file.OriginalName + " - TeleCloud",
		"file":      file,
		"sizeStr":   utils.FormatSize(file.Size),
		"streamURL": streamURL,
	})
}

// ─── Settings / API Token ─────────────────────────────────────────────────────

func HandleAPIGetSettings(c *gin.Context) {
	user := middleware.GetUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Chưa đăng nhập"})
		return
	}

	cfg := config.App
	scheme := "http"
	wsScheme := "ws"
	if c.Request.TLS != nil {
		scheme = "https"
		wsScheme = "wss"
	}
	host := c.Request.Host

	c.JSON(http.StatusOK, gin.H{
		"username":        user.Username,
		"api_token":       user.APIToken,
		"upload_url":      fmt.Sprintf("%s://%s/api/upload-api/upload", scheme, host),
		"ws_progress_url": fmt.Sprintf("%s://%s/api/upload-api/progress/{uploadId}?token=%s", wsScheme, host, user.APIToken),
		"max_upload_mb":   cfg.MaxUploadSizeMB,
	})
}

func HandleAPIRegenerateToken(c *gin.Context) {
	user := middleware.GetUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Chưa đăng nhập"})
		return
	}

	newToken := utils.GenerateToken(32)
	if err := database.UpdateUserAPIToken(user.ID, newToken); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.SetCookie(middleware.SessionCookieName, newToken, 86400*30, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"api_token": newToken})
}

func HandleAPIChangePassword(c *gin.Context) {
	user := middleware.GetUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Chưa đăng nhập"})
		return
	}

	oldPass := c.PostForm("old_password")
	newPass := c.PostForm("new_password")

	if !utils.CheckPassword(oldPass, user.PasswordHash) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mật khẩu cũ không đúng"})
		return
	}
	if len(newPass) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mật khẩu mới phải ít nhất 6 ký tự"})
		return
	}

	hash, _ := utils.HashPassword(newPass)
	database.UpdateUserPassword(user.ID, hash)
	c.JSON(http.StatusOK, gin.H{"message": "Đổi mật khẩu thành công"})
}

// ─── Upload API (external) ────────────────────────────────────────────────────

func HandleUploadAPI(c *gin.Context) {
	user := middleware.GetUser(c)
	path := utils.CleanPath(c.PostForm("path"))
	shareMode := c.PostForm("share")
	uploadID := c.PostForm("uploadId")

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Thiếu file"})
		return
	}

	// Nếu client gửi kèm uploadId, tạo progressState để client có thể theo dõi
	// tiến trình qua WebSocket tại /api/upload-api/progress/:uploadId.
	var st *progressState
	if uploadID != "" {
		st = hub.newProgressState(uploadID)
	}

	_, id, err := processUpload(c.Request.Context(), user.ID, fileHeader, path, st)
	if err != nil {
		if st != nil {
			st.finish("error", err.Error())
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if st != nil {
		st.finish("done", "Hoàn thành")
	}

	resp := gin.H{
		"success": true,
		"id":      id,
		"name":    fileHeader.Filename,
		"size":    fileHeader.Size,
	}

	if shareMode == "public" {
		token := utils.GenerateToken(16)
		database.UpdateFileShareToken(id, token)
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		resp["share_url"] = fmt.Sprintf("%s://%s/share/%s", scheme, c.Request.Host, token)
		resp["direct_url"] = fmt.Sprintf("%s://%s/stream/%d", scheme, c.Request.Host, id)
	}

	c.JSON(http.StatusOK, resp)
}

// ─── Temp File Cleanup ─────────────────────────────────────────────────────────

func CleanupOldTempFiles() {
	cfg := config.App
	tempDir := cfg.TempDir

	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		return
	}

	cutoff := time.Now().Add(-1 * time.Hour)
	files, err := os.ReadDir(tempDir)
	if err != nil {
		log.Printf("[cleanup] Lỗi đọc thư mục temp: %v", err)
		return
	}

	deletedCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			path := filepath.Join(tempDir, file.Name())
			if err := os.Remove(path); err == nil {
				deletedCount++
				log.Printf("[cleanup] Đã xóa temp file cũ: %s (age: %v)",
					file.Name(), time.Since(info.ModTime()).Round(time.Minute))
			}
		}
	}

	if deletedCount > 0 {
		log.Printf("[cleanup] Đã xóa %d file temp cũ", deletedCount)
	}
}

func StartTempCleanupScheduler() {
	go func() {
		time.Sleep(5 * time.Second)
		CleanupOldTempFiles()
	}()

	ticker := time.NewTicker(30 * time.Minute)
	go func() {
		for range ticker.C {
			CleanupOldTempFiles()
		}
	}()

	log.Println("[cleanup] Temp file cleanup scheduler đã khởi động (mỗi 30 phút)")
}