package database

import (
	"database/sql"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

// FileRecord đại diện cho một file được lưu trên Telegram
type FileRecord struct {
	ID           int64
	UserID       int64  // chủ sở hữu file
	Name         string
	OriginalName string
	Size         int64
	MimeType     string
	Path         string // virtual path, e.g. /videos/movie.mp4
	MessageID    int64  // Telegram message ID
	ChatID       int64  // Telegram chat ID
	FileID       string // Telegram file_id (cho bot API compat)
	AccessHash   int64  // MTProto access hash
	FileRef      []byte // MTProto file reference
	ThumbPath    string
	IsVideo      bool
	Duration     int // seconds
	Width        int
	Height       int
	ShareToken   string // token chia sẻ public
	UploadedAt   time.Time
	UpdatedAt    time.Time
}

// FolderRecord đại diện cho một thư mục ảo
type FolderRecord struct {
	ID        int64
	UserID    int64  // chủ sở hữu thư mục
	Name      string
	Path      string // parent path
	FullPath  string // full path = path + name
	CreatedAt time.Time
}

// User cho hệ thống auth web
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
	APIToken     string // Bearer token cho Upload API
	CreatedAt    time.Time
}

func Init(path string) error {
	var err error
	DB, err = sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return err
	}

	DB.SetMaxOpenConns(1)
	DB.SetMaxIdleConns(1)

	if err = DB.Ping(); err != nil {
		return err
	}

	if err = migrate(); err != nil {
		return err
	}

	log.Printf("[database] Kết nối SQLite thành công: %s", path)
	return nil
}

func migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS files (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id       INTEGER NOT NULL DEFAULT 0,
			name          TEXT NOT NULL,
			original_name TEXT NOT NULL DEFAULT '',
			size          INTEGER NOT NULL DEFAULT 0,
			mime_type     TEXT NOT NULL DEFAULT '',
			path          TEXT NOT NULL DEFAULT '/',
			message_id    INTEGER NOT NULL DEFAULT 0,
			chat_id       INTEGER NOT NULL DEFAULT 0,
			file_id       TEXT NOT NULL DEFAULT '',
			access_hash   INTEGER NOT NULL DEFAULT 0,
			file_ref      BLOB,
			thumb_path    TEXT NOT NULL DEFAULT '',
			is_video      INTEGER NOT NULL DEFAULT 0,
			duration      INTEGER NOT NULL DEFAULT 0,
			width         INTEGER NOT NULL DEFAULT 0,
			height        INTEGER NOT NULL DEFAULT 0,
			share_token   TEXT NOT NULL DEFAULT '',
			uploaded_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_files_user_path ON files(user_id, path)`,
		`CREATE INDEX IF NOT EXISTS idx_files_share_token ON files(share_token)`,
		// Migration: thêm user_id vào bảng files cũ nếu chưa có
		`ALTER TABLE files ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS users (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			username      TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_admin      INTEGER NOT NULL DEFAULT 0,
			api_token     TEXT NOT NULL DEFAULT '',
			created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_api_token ON users(api_token)`,
		`CREATE TABLE IF NOT EXISTS folders (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL DEFAULT 0,
			name       TEXT NOT NULL,
			path       TEXT NOT NULL DEFAULT '/',
			full_path  TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_folders_user_fullpath ON folders(user_id, full_path)`,
		`CREATE INDEX IF NOT EXISTS idx_folders_user_path ON folders(user_id, path)`,
		// Migration: thêm user_id vào bảng folders cũ nếu chưa có
		`ALTER TABLE folders ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0`,
	}

	for _, q := range queries {
		if _, err := DB.Exec(q); err != nil {
			// Bỏ qua lỗi "duplicate column" khi chạy ALTER TABLE lần 2+
			if isAlreadyExistsErr(err) {
				continue
			}
			return err
		}
	}
	return nil
}

// isAlreadyExistsErr bỏ qua lỗi khi column/index đã tồn tại (SQLite)
func isAlreadyExistsErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "duplicate column name") ||
		contains(msg, "already exists") ||
		contains(msg, "UNIQUE constraint") && contains(msg, "idx_")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ─── File CRUD ────────────────────────────────────────────────────────────────

func InsertFile(f *FileRecord) (int64, error) {
	res, err := DB.Exec(`
		INSERT INTO files (user_id, name, original_name, size, mime_type, path, message_id, chat_id,
		                   file_id, access_hash, file_ref, thumb_path, is_video, duration,
		                   width, height, share_token, uploaded_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		f.UserID, f.Name, f.OriginalName, f.Size, f.MimeType, f.Path, f.MessageID, f.ChatID,
		f.FileID, f.AccessHash, f.FileRef, f.ThumbPath,
		boolToInt(f.IsVideo), f.Duration, f.Width, f.Height,
		f.ShareToken, time.Now(), time.Now(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetFileByID lấy file theo ID — không filter user (dùng nội bộ / stream)
func GetFileByID(id int64) (*FileRecord, error) {
	row := DB.QueryRow(`SELECT `+fileColumns()+` FROM files WHERE id=?`, id)
	return scanFile(row)
}

// GetFileByIDAndUser lấy file theo ID và kiểm tra chủ sở hữu
func GetFileByIDAndUser(id, userID int64) (*FileRecord, error) {
	row := DB.QueryRow(`SELECT `+fileColumns()+` FROM files WHERE id=? AND user_id=?`, id, userID)
	return scanFile(row)
}

func GetFileByShareToken(token string) (*FileRecord, error) {
	row := DB.QueryRow(`SELECT `+fileColumns()+` FROM files WHERE share_token=?`, token)
	return scanFile(row)
}

func ListFiles(userID int64, path string) ([]*FileRecord, error) {
	rows, err := DB.Query(`SELECT `+fileColumns()+` FROM files WHERE user_id=? AND path=? ORDER BY uploaded_at DESC`, userID, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*FileRecord
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			continue
		}
		files = append(files, f)
	}
	return files, nil
}

func SearchFiles(userID int64, query string) ([]*FileRecord, error) {
	q := "%" + query + "%"
	rows, err := DB.Query(`SELECT `+fileColumns()+` FROM files WHERE user_id=? AND (name LIKE ? OR original_name LIKE ?) ORDER BY uploaded_at DESC LIMIT 50`, userID, q, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*FileRecord
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			continue
		}
		files = append(files, f)
	}
	return files, nil
}

func DeleteFile(id int64) error {
	_, err := DB.Exec(`DELETE FROM files WHERE id=?`, id)
	return err
}

func UpdateFileShareToken(id int64, token string) error {
	_, err := DB.Exec(`UPDATE files SET share_token=?, updated_at=? WHERE id=?`, token, time.Now(), id)
	return err
}

func UpdateFileRef(id int64, fileRef []byte, accessHash int64) error {
	_, err := DB.Exec(`UPDATE files SET file_ref=?, access_hash=?, updated_at=? WHERE id=?`,
		fileRef, accessHash, time.Now(), id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func fileColumns() string {
	return `id, user_id, name, original_name, size, mime_type, path, message_id, chat_id,
	        file_id, access_hash, file_ref, thumb_path, is_video, duration,
	        width, height, share_token, uploaded_at, updated_at`
}

func scanFile(s scanner) (*FileRecord, error) {
	f := &FileRecord{}
	var isVideo int
	err := s.Scan(
		&f.ID, &f.UserID, &f.Name, &f.OriginalName, &f.Size, &f.MimeType, &f.Path,
		&f.MessageID, &f.ChatID, &f.FileID, &f.AccessHash, &f.FileRef,
		&f.ThumbPath, &isVideo, &f.Duration, &f.Width, &f.Height,
		&f.ShareToken, &f.UploadedAt, &f.UpdatedAt,
	)
	f.IsVideo = isVideo == 1
	return f, err
}

// ─── Folder CRUD ──────────────────────────────────────────────────────────────

func InsertFolder(f *FolderRecord) (int64, error) {
	res, err := DB.Exec(`
		INSERT INTO folders (user_id, name, path, full_path, created_at)
		VALUES (?,?,?,?,?)`,
		f.UserID, f.Name, f.Path, f.FullPath, time.Now(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func ListFolders(userID int64, path string) ([]*FolderRecord, error) {
	rows, err := DB.Query(`SELECT id, user_id, name, path, full_path, created_at FROM folders WHERE user_id=? AND path=? ORDER BY name ASC`, userID, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []*FolderRecord
	for rows.Next() {
		f := &FolderRecord{}
		if err := rows.Scan(&f.ID, &f.UserID, &f.Name, &f.Path, &f.FullPath, &f.CreatedAt); err != nil {
			continue
		}
		folders = append(folders, f)
	}
	return folders, nil
}

func GetFolderByFullPath(userID int64, fullPath string) (*FolderRecord, error) {
	row := DB.QueryRow(`SELECT id, user_id, name, path, full_path, created_at FROM folders WHERE user_id=? AND full_path=?`, userID, fullPath)
	f := &FolderRecord{}
	err := row.Scan(&f.ID, &f.UserID, &f.Name, &f.Path, &f.FullPath, &f.CreatedAt)
	return f, err
}

func DeleteFolder(id int64) error {
	_, err := DB.Exec(`DELETE FROM folders WHERE id=?`, id)
	return err
}

// DeleteFolderRecursive xóa thư mục + toàn bộ thư mục con và file bên trong (theo user)
func DeleteFolderRecursive(userID int64, fullPath string) error {
	_, err := DB.Exec(`DELETE FROM folders WHERE user_id=? AND (full_path=? OR full_path LIKE ?)`, userID, fullPath, fullPath+"/%")
	if err != nil {
		return err
	}
	_, err = DB.Exec(`DELETE FROM files WHERE user_id=? AND (path=? OR path LIKE ?)`, userID, fullPath, fullPath+"/%")
	return err
}

// MoveFileToFolder cập nhật path của file
func MoveFileToFolder(fileID int64, newPath string) error {
	_, err := DB.Exec(`UPDATE files SET path=?, updated_at=? WHERE id=?`, newPath, time.Now(), fileID)
	return err
}

// CountFilesInFolder đếm file trong thư mục (bao gồm sub-folder) theo user
func CountFilesInFolder(userID int64, fullPath string) (int, error) {
	var count int
	err := DB.QueryRow(`SELECT COUNT(*) FROM files WHERE user_id=? AND (path=? OR path LIKE ?)`, userID, fullPath, fullPath+"/%").Scan(&count)
	return count, err
}

// ─── User CRUD ────────────────────────────────────────────────────────────────

func InsertUser(u *User) (int64, error) {
	res, err := DB.Exec(`
		INSERT INTO users (username, password_hash, is_admin, api_token, created_at)
		VALUES (?,?,?,?,?)`,
		u.Username, u.PasswordHash, boolToInt(u.IsAdmin), u.APIToken, time.Now(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetUserByUsername(username string) (*User, error) {
	row := DB.QueryRow(`SELECT id, username, password_hash, is_admin, api_token, created_at FROM users WHERE username=?`, username)
	u := &User{}
	var isAdmin int
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdmin, &u.APIToken, &u.CreatedAt)
	u.IsAdmin = isAdmin == 1
	return u, err
}

func GetUserByAPIToken(token string) (*User, error) {
	row := DB.QueryRow(`SELECT id, username, password_hash, is_admin, api_token, created_at FROM users WHERE api_token=?`, token)
	u := &User{}
	var isAdmin int
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdmin, &u.APIToken, &u.CreatedAt)
	u.IsAdmin = isAdmin == 1
	return u, err
}

func CountUsers() (int, error) {
	var count int
	err := DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func UpdateUserPassword(id int64, hash string) error {
	_, err := DB.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func UpdateUserAPIToken(id int64, token string) error {
	_, err := DB.Exec(`UPDATE users SET api_token=? WHERE id=?`, token, id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}