package api

import (
	"encoding/json"
	"fmt"
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

// newProgressState tạo và đăng ký 1 session progress mới, dọn rác sau 5 phút
// để tránh leak nếu client không bao giờ subscribe / kết nối SSE.
func (h *progressHub) newProgressState(uploadID string) *progressState {
	st := &progressState{stage: "receiving", pct: 0, updated: time.Now(), doneCh: make(chan struct{})}
	h.mu.Lock()
	h.uploads[uploadID] = st
	h.mu.Unlock()

	go func() {
		time.Sleep(5 * time.Minute)
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

	// Đợi tối đa 10s để state xuất hiện (trường hợp SSE connect trước khi
	// POST /api/upload kịp tạo state — hiếm nhưng có thể xảy ra do race).
	var st *progressState
	for i := 0; i < 100; i++ {
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
