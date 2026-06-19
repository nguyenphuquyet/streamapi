package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	APIId           int
	APIHash         string
	LogGroupID      string
	Port            string
	MaxUploadSizeMB int64
	DatabasePath    string
	ThumbsDir       string
	TempDir         string
	FFmpegPath      string
	ProxyURL        string
	JWTSecret       string
}

var App *Config

func Load() {
	// Load .env nếu có
	if err := godotenv.Load(); err != nil {
		log.Println("[config] Không tìm thấy .env, dùng biến môi trường hệ thống")
	}

	apiID, err := strconv.Atoi(getEnv("API_ID", "0"))
	if err != nil || apiID == 0 {
		log.Fatal("[config] API_ID không hợp lệ hoặc chưa được cấu hình")
	}

	apiHash := getEnv("API_HASH", "")
	if apiHash == "" {
		log.Fatal("[config] API_HASH chưa được cấu hình")
	}

	maxUpload, _ := strconv.ParseInt(getEnv("MAX_UPLOAD_SIZE_MB", "0"), 10, 64)

	App = &Config{
		APIId:           apiID,
		APIHash:         apiHash,
		LogGroupID:      getEnv("LOG_GROUP_ID", "me"),
		Port:            getEnv("PORT", "8091"),
		MaxUploadSizeMB: maxUpload,
		DatabasePath:    getEnv("DATABASE_PATH", "./data/telecloud.db"),
		ThumbsDir:       getEnv("THUMBS_DIR", "./data/thumbs"),
		TempDir:         getEnv("TEMP_DIR", "./data/temp"),
		FFmpegPath:      getEnv("FFMPEG_PATH", "ffmpeg"),
		ProxyURL:        getEnv("PROXY_URL", ""),
		JWTSecret:       getEnv("JWT_SECRET", "telecloud-default-secret-change-me"),
	}

	// Tạo các thư mục cần thiết
	for _, dir := range []string{App.ThumbsDir, App.TempDir, dirOf(App.DatabasePath)} {
		if dir != "" && dir != "." {
			os.MkdirAll(dir, 0755)
		}
	}

	log.Printf("[config] Đã tải cấu hình: port=%s, group=%s", App.Port, App.LogGroupID)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
