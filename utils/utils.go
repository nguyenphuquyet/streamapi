package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ─── MIME ─────────────────────────────────────────────────────────────────────

func DetectMimeType(data []byte, filename string) string {
	// Thử detect từ nội dung
	mimeType := http.DetectContentType(data)

	// Nếu generic, thử từ extension
	if mimeType == "application/octet-stream" || mimeType == "text/plain; charset=utf-8" {
		ext := strings.ToLower(filepath.Ext(filename))
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	return mimeType
}

func IsVideoMime(mime string) bool {
	return strings.HasPrefix(mime, "video/")
}

func IsAudioMime(mime string) bool {
	return strings.HasPrefix(mime, "audio/")
}

func IsImageMime(mime string) bool {
	return strings.HasPrefix(mime, "image/")
}

// ─── Thumbnail ────────────────────────────────────────────────────────────────

// GenerateVideoThumbnail dùng ffmpeg để tạo ảnh thumbnail từ video
func GenerateVideoThumbnail(ffmpegPath, videoPath, outputPath string) error {
	if ffmpegPath == "disabled" {
		return fmt.Errorf("ffmpeg bị tắt")
	}

	os.MkdirAll(filepath.Dir(outputPath), 0755)

	cmd := exec.Command(ffmpegPath,
		"-i", videoPath,
		"-ss", "00:00:05",
		"-vframes", "1",
		"-vf", "scale=320:-1",
		"-y",
		outputPath,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		// Thử lại ở giây 1
		cmd2 := exec.Command(ffmpegPath,
			"-i", videoPath,
			"-ss", "00:00:01",
			"-vframes", "1",
			"-vf", "scale=320:-1",
			"-y",
			outputPath,
		)
		return cmd2.Run()
	}
	return nil
}

// GetVideoInfo lấy thông tin video qua ffprobe
func GetVideoInfo(ffmpegPath, videoPath string) (duration int, width int, height int) {
	if ffmpegPath == "disabled" {
		return 0, 0, 0
	}

	ffprobe := strings.Replace(ffmpegPath, "ffmpeg", "ffprobe", 1)

	cmd := exec.Command(ffprobe,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,duration",
		"-of", "csv=p=0",
		videoPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, 0
	}

	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) >= 3 {
		fmt.Sscanf(parts[0], "%d", &width)
		fmt.Sscanf(parts[1], "%d", &height)
		var dur float64
		fmt.Sscanf(parts[2], "%f", &dur)
		duration = int(dur)
	}
	return
}

// ─── Token ────────────────────────────────────────────────────────────────────

func GenerateToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ─── Password ─────────────────────────────────────────────────────────────────

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ─── File size ────────────────────────────────────────────────────────────────

func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// ─── Path ─────────────────────────────────────────────────────────────────────

func CleanPath(p string) string {
	if p == "" {
		return "/"
	}
	p = filepath.Clean("/" + p)
	return filepath.ToSlash(p)
}

func SafeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	return name
}

// ─── Time ─────────────────────────────────────────────────────────────────────

func FormatTime(t time.Time) string {
	return t.Format("02/01/2006 15:04")
}
