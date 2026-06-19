package tgclient

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"telecloud/config"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"golang.org/x/net/proxy"
)

// Client wraps gotd Telegram client
type Client struct {
	mu            sync.Mutex
	client        *telegram.Client
	api           *tg.Client
	ctx           context.Context
	cancel        context.CancelFunc
	ready         chan struct{}
	groupID       int64
	isReady       bool
	channelHashes map[int64]int64 // channelID -> accessHash cache
}

// telegramProgress implement uploader.Progress để nhận callback mỗi khi
// 1 part được upload xong lên Telegram — đây là cơ chế CHÍNH THỨC của
// gotd/td (an toàn với WithThreads, vì lib tự đảm bảo gọi Chunk() đúng cách
// dù chạy nhiều goroutine song song), thay vì tự bọc io.Reader.
type telegramProgress struct {
	onChunk func(uploaded, total int64)
}

func (p *telegramProgress) Chunk(ctx context.Context, state uploader.ProgressState) error {
	if p.onChunk != nil {
		p.onChunk(state.Uploaded, state.Total)
	}
	return nil
}

var TG *Client

// Init khởi động Telegram client, kết nối và authenticate
func Init(sessionPath string) (*Client, error) {
	cfg := config.App
	c := &Client{
		ready:         make(chan struct{}),
		channelHashes: make(map[int64]int64),
	}

	// Parse group ID
	if cfg.LogGroupID != "me" {
		gid, err := strconv.ParseInt(cfg.LogGroupID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("LOG_GROUP_ID không hợp lệ: %v", err)
		}
		c.groupID = gid
	}

	opts := telegram.Options{
		SessionStorage: &fileSessionStorage{path: sessionPath},
	}

	// Proxy nếu có
	if cfg.ProxyURL != "" {
		dialFn, err := buildDialer(cfg.ProxyURL)
		if err != nil {
			log.Printf("[tg] Cảnh báo: không thể dùng proxy %s: %v", cfg.ProxyURL, err)
		} else {
			opts.Resolver = dcs.Plain(dcs.PlainOptions{Dial: dialFn})
		}
	}

	c.client = telegram.NewClient(cfg.APIId, cfg.APIHash, opts)
	TG = c
	return c, nil
}

// Run chạy client trong goroutine
func (c *Client) Run(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	return c.client.Run(c.ctx, func(ctx context.Context) error {
		c.api = c.client.API()

		// Kiểm tra auth status
		status, err := c.client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("lỗi kiểm tra auth: %w", err)
		}

		if !status.Authorized {
			return fmt.Errorf("chưa xác thực Telegram. Hãy chạy với flag -auth trước")
		}

		log.Printf("[tg] Đã kết nối Telegram thành công")
		c.mu.Lock()
		c.isReady = true
		c.mu.Unlock()
		close(c.ready)

		// Giữ client chạy
		<-c.ctx.Done()
		return nil
	})
}

// RunAuth chạy flow xác thực tương tác
func (c *Client) RunAuth(ctx context.Context) error {
	return c.client.Run(ctx, func(ctx context.Context) error {
		c.api = c.client.API()

		flow := auth.NewFlow(
			terminalAuth{},
			auth.SendCodeOptions{},
		)
		if err := c.client.Auth().IfNecessary(ctx, flow); err != nil {
			return err
		}

		me, err := c.client.Self(ctx)
		if err != nil {
			return err
		}
		log.Printf("[tg] Xác thực thành công! Đăng nhập với tư cách: %s %s (@%s)",
			me.FirstName, me.LastName, me.Username)
		return nil
	})
}

// WaitReady chờ client sẵn sàng
func (c *Client) WaitReady() {
	<-c.ready
}

// UploadFile gửi file lên Telegram. onProgress (có thể nil) được gọi mỗi khi
// một chunk được đọc xong, với read = số byte đã gửi, total = tổng kích thước.
func (c *Client) UploadFile(ctx context.Context, reader io.Reader, filename string, size int64, mimeType string, onProgress func(read, total int64)) (*UploadResult, error) {
	if !c.isReady {
		return nil, fmt.Errorf("Telegram client chưa sẵn sàng")
	}

	// Part size: 256KB — chia hết cho 4KB, ổn định hơn 512KB với file lớn
	// Telegram yêu cầu part size phải là lũy thừa của 2 và <= 512KB
	partSize := 256 * 1024

	up := uploader.NewUploader(c.api).
		WithThreads(4).
		WithPartSize(partSize)

	if onProgress != nil {
		up = up.WithProgress(&telegramProgress{onChunk: onProgress})
	}

	// Upload với retry tối đa 3 lần
	var tgFile tg.InputFileClass
	var uploadErr error
	for attempt := 1; attempt <= 3; attempt++ {
		tgFile, uploadErr = up.Upload(ctx, uploader.NewUpload(filename, reader, size))
		if uploadErr == nil {
			break
		}
		log.Printf("[tg] Upload attempt %d/3 thất bại: %v", attempt, uploadErr)
		if attempt < 3 {
			// Reset reader về đầu nếu có thể
			if seeker, ok := reader.(io.Seeker); ok {
				seeker.Seek(0, io.SeekStart)
			} else {
				// Không seek được → không retry
				break
			}
		}
	}
	if uploadErr != nil {
		return nil, fmt.Errorf("lỗi upload lên Telegram sau 3 lần thử: %w", uploadErr)
	}

	// Xác định peer đích
	peer, chatID, err := c.resolvePeer(ctx)
	if err != nil {
		return nil, err
	}

	// Gửi file lên nhóm/chat
	var inputMedia tg.InputMediaClass

	isVideo := isVideoMime(mimeType)
	if isVideo {
		inputMedia = &tg.InputMediaUploadedDocument{
			File:     tgFile,
			MimeType: mimeType,
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeFilename{FileName: filename},
				&tg.DocumentAttributeVideo{
					SupportsStreaming: true,
				},
			},
		}
	} else {
		inputMedia = &tg.InputMediaUploadedDocument{
			File:     tgFile,
			MimeType: mimeType,
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeFilename{FileName: filename},
			},
		}
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    inputMedia,
		Message:  "",
		RandomID: randomID(),
	}

	updates, err := c.api.MessagesSendMedia(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("lỗi gửi media: %w", err)
	}

	// Parse kết quả
	result := &UploadResult{ChatID: chatID}
	switch u := updates.(type) {
	case *tg.Updates:
		for _, update := range u.Updates {
			var msgClass tg.MessageClass
			switch up := update.(type) {
			case *tg.UpdateNewMessage:
				msgClass = up.Message
			case *tg.UpdateNewChannelMessage:
				msgClass = up.Message
			default:
				continue
			}

			if m, ok := msgClass.(*tg.Message); ok {
				result.MessageID = int64(m.ID)
				if doc, ok := m.Media.(*tg.MessageMediaDocument); ok {
					if d, ok := doc.Document.(*tg.Document); ok {
						result.AccessHash = d.AccessHash
						result.FileRef = d.FileReference
						result.FileID = fmt.Sprintf("%d", d.ID)
						result.Size = d.Size
					}
				}
			}
		}
	}

	if result.MessageID == 0 {
		return nil, fmt.Errorf("không lấy được message_id sau khi gửi media (updates type: %T)", updates)
	}

	return result, nil
}

// DownloadFileRange download một phần của file (range request cho streaming)
func (c *Client) DownloadFileRange(ctx context.Context, chatID, messageID int64, fileRef []byte, accessHash int64, start, end int64) (io.ReadCloser, int64, error) {
	if !c.isReady {
		return nil, 0, fmt.Errorf("Telegram client chưa sẵn sàng")
	}

	// Lấy lại message để get file location
	peer, _, err := c.resolvePeerByID(ctx, chatID)
	if err != nil {
		return nil, 0, err
	}

	var msgs tg.MessagesMessagesClass

	if inputChannel, ok := peer.(*tg.InputPeerChannel); ok {
		// Channel/supergroup: PHẢI dùng ChannelsGetMessages.
		// MessagesGetMessages không báo lỗi với channel, nó chỉ trả về rỗng,
		// nên không thể dùng làm "thử trước rồi fallback".
		msgs, err = c.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{
				ChannelID:  inputChannel.ChannelID,
				AccessHash: inputChannel.AccessHash,
			},
			ID: []tg.InputMessageClass{&tg.InputMessageID{ID: int(messageID)}},
		})
		if err != nil {
			return nil, 0, fmt.Errorf("không thể lấy message từ channel: %w", err)
		}
	} else {
		msgs, err = c.api.MessagesGetMessages(ctx, []tg.InputMessageClass{
			&tg.InputMessageID{ID: int(messageID)},
		})
		if err != nil {
			return nil, 0, fmt.Errorf("không thể lấy message: %w", err)
		}
	}

	var docLoc *tg.InputDocumentFileLocation
	var totalSize int64

	switch m := msgs.(type) {
	case *tg.MessagesMessages:
		for _, msg := range m.Messages {
			if message, ok := msg.(*tg.Message); ok && int64(message.ID) == messageID {
				if media, ok := message.Media.(*tg.MessageMediaDocument); ok {
					if doc, ok := media.Document.(*tg.Document); ok {
						docLoc = &tg.InputDocumentFileLocation{
							ID:            doc.ID,
							AccessHash:    doc.AccessHash,
							FileReference: doc.FileReference,
						}
						totalSize = doc.Size
					}
				}
			}
		}
	case *tg.MessagesChannelMessages:
		for _, msg := range m.Messages {
			if message, ok := msg.(*tg.Message); ok && int64(message.ID) == messageID {
				if media, ok := message.Media.(*tg.MessageMediaDocument); ok {
					if doc, ok := media.Document.(*tg.Document); ok {
						docLoc = &tg.InputDocumentFileLocation{
							ID:            doc.ID,
							AccessHash:    doc.AccessHash,
							FileReference: doc.FileReference,
						}
						totalSize = doc.Size
					}
				}
			}
		}
	}

	if docLoc == nil {
		log.Printf("[tg] debug: msgs type=%T, content=%+v", msgs, msgs)
		return nil, 0, fmt.Errorf("không tìm thấy document trong message %d", messageID)
	}

	if end <= 0 || end >= totalSize {
		end = totalSize - 1
	}
	length := end - start + 1

	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()

		// MTProto: offset & limit phải chia hết cho 4KB,
		// toàn bộ [offset, offset+limit) phải trong cùng 1MB chunk.
		// 512KB = lũy thừa 2, chia hết 4KB, 1MB/512KB=2 → hợp lệ.
		// Tăng từ 128KB lên 512KB: giảm 4x số round-trips.
		const chunkSize = int64(512 * 1024)
		// Số chunk tải song song — 4 goroutine đủ tận dụng băng thông
		// mà không bị Telegram flood-wait
		const workers = 4

		alignedOffset := (start / chunkSize) * chunkSize
		skipBytes := start - alignedOffset

		totalChunks := (length + skipBytes + chunkSize - 1) / chunkSize

		type chunkResult struct {
			index int
			data  []byte
			err   error
		}

		results := make(chan chunkResult, workers*2)
		sem := make(chan struct{}, workers)

		// Dispatch goroutines
		go func() {
			for i := int64(0); i < totalChunks; i++ {
				sem <- struct{}{}
				offset := alignedOffset + i*chunkSize
				idx := int(i)
				go func(idx int, offset int64) {
					defer func() { <-sem }()
					res, err := c.api.UploadGetFile(ctx, &tg.UploadGetFileRequest{
						Location: docLoc,
						Offset:   offset,
						Limit:    int(chunkSize),
					})
					if err != nil {
						results <- chunkResult{idx, nil, fmt.Errorf("chunk %d lỗi: %w", idx, err)}
						return
					}
					switch r := res.(type) {
					case *tg.UploadFile:
						results <- chunkResult{idx, r.Bytes, nil}
					default:
						results <- chunkResult{idx, nil, fmt.Errorf("chunk %d: unexpected type %T", idx, res)}
					}
				}(idx, offset)
			}
		}()

		// Thu gom và ghi theo thứ tự
		pending := make(map[int][]byte)
		nextWrite := 0
		written := int64(0)
		received := 0

		for received < int(totalChunks) {
			cr := <-results
			received++
			if cr.err != nil {
				pw.CloseWithError(cr.err)
				return
			}
			pending[cr.index] = cr.data

			// Ghi liên tiếp các chunk đã về đúng thứ tự
			for {
				chunk, ok := pending[nextWrite]
				if !ok {
					break
				}
				delete(pending, nextWrite)

				// Chunk đầu: cắt phần align thừa
				if nextWrite == 0 && skipBytes > 0 {
					if int64(len(chunk)) <= skipBytes {
						nextWrite++
						continue
					}
					chunk = chunk[skipBytes:]
				}

				// Chunk cuối: cắt phần thừa
				if written+int64(len(chunk)) > length {
					chunk = chunk[:length-written]
				}

				if len(chunk) == 0 {
					nextWrite++
					continue
				}

				if _, err := pw.Write(chunk); err != nil {
					pw.CloseWithError(err)
					return
				}
				written += int64(len(chunk))
				nextWrite++

				if written >= length {
					return
				}
			}
		}
	}()

	return pr, length, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

type UploadResult struct {
	MessageID  int64
	ChatID     int64
	AccessHash int64
	FileRef    []byte
	FileID     string
	Size       int64
}

func (c *Client) resolvePeer(ctx context.Context) (tg.InputPeerClass, int64, error) {
	if c.groupID == 0 {
		// "me" → Saved Messages
		self, err := c.client.Self(ctx)
		if err != nil {
			return nil, 0, err
		}
		return &tg.InputPeerSelf{}, self.ID, nil
	}
	return c.resolvePeerByID(ctx, c.groupID)
}

func (c *Client) resolvePeerByID(ctx context.Context, chatID int64) (tg.InputPeerClass, int64, error) {
	if chatID == 0 {
		self, err := c.client.Self(ctx)
		if err != nil {
			return nil, 0, err
		}
		return &tg.InputPeerSelf{}, self.ID, nil
	}

	// Channel/supergroup (ID âm, dạng -100xxxxx)
	idStr := fmt.Sprintf("%d", chatID)
	if strings.HasPrefix(idStr, "-100") {
		channelID, _ := strconv.ParseInt(idStr[4:], 10, 64)

		// Kiểm tra cache trước
		c.mu.Lock()
		if hash, ok := c.channelHashes[channelID]; ok {
			c.mu.Unlock()
			return &tg.InputPeerChannel{
				ChannelID:  channelID,
				AccessHash: hash,
			}, chatID, nil
		}
		c.mu.Unlock()

		// Resolve access hash qua danh sách dialogs (ổn định hơn MessagesGetAllChats)
		if hash, ok := c.findChannelAccessHash(ctx, channelID); ok {
			c.mu.Lock()
			c.channelHashes[channelID] = hash
			c.mu.Unlock()
			return &tg.InputPeerChannel{
				ChannelID:  channelID,
				AccessHash: hash,
			}, chatID, nil
		}

		return &tg.InputPeerChannel{ChannelID: channelID}, chatID, nil
	}

	return &tg.InputPeerSelf{}, chatID, nil
}

// findChannelAccessHash tìm AccessHash của một channel bằng cách quét danh sách dialogs.
// Dùng MessagesGetDialogs (ổn định, có lâu năm trong MTProto) thay vì MessagesGetAllChats
// để tránh phụ thuộc vào method có thể không tồn tại/đổi signature theo version gotd/td.
func (c *Client) findChannelAccessHash(ctx context.Context, channelID int64) (int64, bool) {
	res, err := c.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
	})
	if err != nil {
		return 0, false
	}

	var chats []tg.ChatClass
	switch d := res.(type) {
	case *tg.MessagesDialogs:
		chats = d.Chats
	case *tg.MessagesDialogsSlice:
		chats = d.Chats
	}

	for _, chat := range chats {
		if ch, ok := chat.(*tg.Channel); ok && ch.ID == channelID {
			return ch.AccessHash, true
		}
	}
	return 0, false
}

func isVideoMime(mime string) bool {
	return strings.HasPrefix(mime, "video/")
}

func randomID() int64 {
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		// fallback nếu crypto/rand lỗi
		return int64(os.Getpid())*1e9 + time.Now().UnixNano()
	}
	id := int64(b[0])<<56 | int64(b[1])<<48 | int64(b[2])<<40 | int64(b[3])<<32 |
		int64(b[4])<<24 | int64(b[5])<<16 | int64(b[6])<<8 | int64(b[7])
	if id < 0 {
		id = -id
	}
	return id
}

func buildDialer(proxyURL string) (func(ctx context.Context, network, addr string) (net.Conn, error), error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	var proxyAuth *proxy.Auth
	if u.User != nil {
		pass, _ := u.User.Password()
		proxyAuth = &proxy.Auth{
			User:     u.User.Username(),
			Password: pass,
		}
	}

	dialer, err := proxy.SOCKS5("tcp", u.Host, proxyAuth, proxy.Direct)
	if err != nil {
		return nil, err
	}

	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		// Fallback: wrap plain Dial in a context-aware func
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}, nil
	}

	return contextDialer.DialContext, nil
}

// ─── Session Storage ──────────────────────────────────────────────────────────

type fileSessionStorage struct {
	path string
}

func (s *fileSessionStorage) LoadSession(_ context.Context) ([]byte, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func (s *fileSessionStorage) StoreSession(_ context.Context, data []byte) error {
	os.MkdirAll(filepath.Dir(s.path), 0755)
	return os.WriteFile(s.path, data, 0600)
}

// ─── Terminal Auth ────────────────────────────────────────────────────────────

type terminalAuth struct{}

func (terminalAuth) Phone(_ context.Context) (string, error) {
	fmt.Print("Nhập số điện thoại (dạng +84...): ")
	var phone string
	fmt.Scanln(&phone)
	return phone, nil
}

func (terminalAuth) Password(_ context.Context) (string, error) {
	fmt.Print("Nhập mật khẩu 2FA (để trống nếu không có): ")
	var pass string
	fmt.Scanln(&pass)
	return pass, nil
}

func (terminalAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	fmt.Print("Nhập mã OTP từ Telegram: ")
	var code string
	fmt.Scanln(&code)
	return code, nil
}

func (terminalAuth) AcceptTermsOfService(_ context.Context, tos tg.HelpTermsOfService) error {
	return nil
}

func (terminalAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, fmt.Errorf("đăng ký không được hỗ trợ")
}