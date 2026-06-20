package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ════════════════════════════════════════════════════════════════════════════
// PROGRESS HUB — theo dõi tiến trình upload THẬT (browser->server->Telegram)
// qua SSE (Server-Sent Events), key theo uploadId do client tự sinh.
// ════════════════════════════════════════════════════════════════════════════

// progressState lưu trạng thái hiện tại của 1 lượt upload.
type progressState struct {
	mu         sync.Mutex
	stage      string // "receiving" | "telegram" | "done" | "error"
	pct        int
	message    string
	updated    time.Time
	doneCh     chan struct{} // đóng khi upload kết thúc (done hoặc error)
	closedOnce sync.Once
}

type progressHub struct {
	mu      sync.Mutex
	uploads map[string]*progressState
}

var hub = &progressHub{uploads: make(map[string]*progressState)}

// sessionTTL tính thời gian sống tối thiểu của session theo kích thước file.
// Ước tính upload speed ~2 MB/s lên Telegram (thực tế thấp hơn trên mạng yếu)
// rồi nhân hệ số an toàn 3× + thêm buffer cố định 5 phút.
// Tối thiểu 10 phút, tối đa 3 giờ.
func sessionTTL(fileSizeBytes int64) time.Duration {
	const uploadSpeedBps = 2 * 1024 * 1024 // 2 MB/s (conservative)
	const safetyFactor = 3
	const minTTL = 10 * time.Minute
	const maxTTL = 3 * time.Hour
	const fixedBuffer = 5 * time.Minute

	if fileSizeBytes <= 0 {
		return minTTL
	}
	estimated := time.Duration(float64(fileSizeBytes)/uploadSpeedBps*safetyFactor)*time.Second + fixedBuffer
	if estimated < minTTL {
		return minTTL
	}
	if estimated > maxTTL {
		return maxTTL
	}
	return estimated
}

// newProgressState tạo và đăng ký 1 session progress mới.
// TTL được tính động theo kích thước file để tránh session hết hạn
// trước khi upload xong với file lớn.
func (h *progressHub) newProgressState(uploadID string, fileSizeBytes int64) *progressState {
	st := &progressState{stage: "receiving", pct: 0, updated: time.Now(), doneCh: make(chan struct{})}
	h.mu.Lock()
	h.uploads[uploadID] = st
	h.mu.Unlock()

	ttl := sessionTTL(fileSizeBytes)
	log.Printf("[progress] session %s TTL=%v (fileSize=%d bytes)", uploadID, ttl.Round(time.Second), fileSizeBytes)

	go func() {
		time.Sleep(ttl)
		h.mu.Lock()
		delete(h.uploads, uploadID)
		h.mu.Unlock()
	}()
	return st
}

func (h *progressHub) get(uploadID string) *progressState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.uploads[uploadID]
}

func (st *progressState) set(stage string, pct int, message string) {
	st.mu.Lock()
	st.stage = stage
	st.pct = pct
	st.message = message
	st.updated = time.Now()
	st.mu.Unlock()
}

func (st *progressState) snapshot() (string, int, string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.stage, st.pct, st.message
}

// finish đánh dấu upload kết thúc (thành công hoặc lỗi) và đóng kênh doneCh
// để handler SSE biết dừng stream.
func (st *progressState) finish(stage string, message string) {
	st.set(stage, 100, message)
	st.closedOnce.Do(func() { close(st.doneCh) })
}

// ─── HTTP Handler: SSE endpoint ────────────────────────────────────────────

type progressPayload struct {
	Pct     int    `json:"pct"`
	Message string `json:"message"`
}

// writeSSEEvent ghi 1 event SSE đúng định dạng, JSON-encode an toàn message
// (tránh lỗi nếu message chứa dấu nháy, xuống dòng, v.v.)
func writeSSEEvent(w gin.ResponseWriter, event string, pct int, message string) {
	data, err := json.Marshal(progressPayload{Pct: pct, Message: message})
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

// HandleUploadProgress stream tiến trình upload qua Server-Sent Events.
// Client kết nối TRƯỚC khi gửi POST /api/upload, dùng cùng 1 uploadId.
func HandleUploadProgress(c *gin.Context) {
	uploadID := c.Param("uploadId")

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // tắt buffering nếu có nginx phía trước

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming không được hỗ trợ"})
		return
	}

	// Flush ngay header để client (EventSource) nhận "onopen" lập tức,
	// thay vì phải đợi tới lần Flush đầu tiên (vốn chỉ xảy ra khi tìm thấy
	// state hoặc sau khi timeout). Nếu không flush sớm, client đứng chờ
	// onopen mới gửi POST upload, còn server đứng chờ POST mới có state
	// — hai bên deadlock tới khi hết 30s rồi báo lỗi "hết hạn".
	c.Writer.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Đợi tối đa 30s để state xuất hiện (trường hợp SSE connect trước khi
	// POST /api/upload kịp tạo state — có thể xảy ra do race hoặc file lớn
	// mất thời gian parse multipart trước khi newProgressState được gọi).
	var st *progressState
	for i := 0; i < 300; i++ {
		st = hub.get(uploadID)
		if st != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if st == nil {
		writeSSEEvent(c.Writer, "error", 0, "Upload session không tồn tại hoặc đã hết hạn")
		flusher.Flush()
		return
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	lastPct := -1
	lastStage := ""

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-st.doneCh:
			stage, pct, msg := st.snapshot()
			writeSSEEvent(c.Writer, stage, pct, msg)
			flusher.Flush()
			return
		case <-ticker.C:
			stage, pct, msg := st.snapshot()
			if pct != lastPct || stage != lastStage {
				writeSSEEvent(c.Writer, stage, pct, msg)
				flusher.Flush()
				lastPct = pct
				lastStage = stage
			}
		}
	}
}

// ─── HTTP Handler: SSE endpoint (Upload API) ───────────────────────────────

// HandleUploadAPIProgressSSE stream tiến trình upload qua Server-Sent Events
// cho Upload API (Bearer token). Cách dùng:
//  1. Client tự sinh một uploadId ngẫu nhiên (vd: uuid).
//  2. Mở kết nối SSE tới GET /api/upload-api/progress/:uploadId?token=<api_token> TRƯỚC.
//  3. POST /api/upload-api/upload kèm field "uploadId" trùng giá trị ở bước 1.
//  4. Server push event SSE dạng {"pct","message"} với event name là stage
//     ("receiving" | "telegram" | "done" | "error"), kết thúc khi gặp "done"/"error".
//
// Dùng SSE thay WebSocket vì đây là luồng một chiều (server→client); SSE đơn
// giản hơn, không cần thư viện phía client, hoạt động tốt qua proxy/nginx.
// Xác thực token qua query string (?token=...) vì EventSource API của trình
// duyệt không hỗ trợ gửi header Authorization.
// Việc xác thực (RequireAPITokenWS/RequireAPIToken) đã chạy trước handler này.
func HandleUploadAPIProgressSSE(c *gin.Context) {
	uploadID := c.Param("uploadId")

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // tắt buffering nếu có nginx phía trước

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming không được hỗ trợ"})
		return
	}

	// Flush ngay header để client (EventSource) nhận "onopen" lập tức,
	// thay vì phải đợi tới lần Flush đầu tiên (vốn chỉ xảy ra khi tìm thấy
	// state hoặc sau khi timeout). Nếu không flush sớm, client đứng chờ
	// onopen mới gửi POST upload, còn server đứng chờ POST mới có state
	// — hai bên deadlock tới khi hết 30s rồi báo lỗi "hết hạn".
	c.Writer.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Đợi tối đa 30s để state xuất hiện, phòng trường hợp client mở SSE trước
	// khi POST /api/upload-api/upload kịp tạo progressState (race hiếm gặp).
	var st *progressState
	for i := 0; i < 300; i++ {
		st = hub.get(uploadID)
		if st != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if st == nil {
		writeSSEEvent(c.Writer, "error", 0, "Upload session không tồn tại hoặc đã hết hạn")
		flusher.Flush()
		return
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	lastPct := -1
	lastStage := ""

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-st.doneCh:
			stage, pct, msg := st.snapshot()
			writeSSEEvent(c.Writer, stage, pct, msg)
			flusher.Flush()
			return
		case <-ticker.C:
			stage, pct, msg := st.snapshot()
			if pct != lastPct || stage != lastStage {
				writeSSEEvent(c.Writer, stage, pct, msg)
				flusher.Flush()
				lastPct = pct
				lastStage = stage
			}
		}
	}
}