# 🚀 TeleCloud Go

Ứng dụng lưu trữ file không giới hạn trên Telegram, viết bằng Golang.

**Tính năng:**
- 📁 Upload file lên Telegram (hỗ trợ đến 2GB/4GB qua MTProto Userbot)
- 🎬 Stream video trực tiếp với HTTP Range requests
- 🔗 Upload API với Bearer Token (cho CI/CD, script)
- 🖼️ Tự động tạo thumbnail cho video (cần FFmpeg)
- 📤 Chia sẻ file qua link public
- 🔒 Xác thực web với tài khoản admin
- 🗃️ Lưu trữ metadata với SQLite
- 🐳 Docker-ready

---

## 📋 Yêu cầu

- Go 1.21+
- FFmpeg (tùy chọn, để tạo thumbnail)
- Tài khoản Telegram

---

## ⚡ Bắt đầu nhanh

### 1. Lấy Telegram API credentials

Truy cập https://my.telegram.org → **API development tools** → tạo app mới → lấy `API_ID` và `API_HASH`.

### 2. Cấu hình

```bash
cp .env.example .env
# Chỉnh sửa .env với API_ID, API_HASH, LOG_GROUP_ID của bạn
```

Nội dung `.env`:
```env
API_ID=12345678
API_HASH=your_hash_here
LOG_GROUP_ID=me        # hoặc -100xxxxxxxxxx (ID nhóm/kênh)
PORT=8091
```

### 3. Cài dependencies

```bash
go mod tidy
```

### 4. Xác thực Telegram (lần đầu)

```bash
make auth
# hoặc: go run . -auth
```

Nhập số điện thoại, OTP, và mật khẩu 2FA (nếu có).

### 5. Chạy server

```bash
make run
# hoặc: go run .
```

Mở trình duyệt: **http://localhost:8091**

Lần đầu truy cập → tạo tài khoản admin.

---

## 🐳 Docker

```bash
cp .env.example .env
# Chỉnh sửa .env

# Xác thực Telegram (lần đầu)
make docker-auth

# Khởi động
make docker-up

# Xem logs
make docker-logs
```

---

## 🔌 Upload API

Dùng để upload file từ script, CI/CD, hoặc ứng dụng khác.

**Lấy Bearer Token:** Đăng nhập web → Cài đặt → Upload API

### Endpoint

```
POST /api/upload-api/upload
Authorization: Bearer <your-token>
Content-Type: multipart/form-data
```

### Parameters

| Tham số | Bắt buộc | Mô tả |
|---------|----------|-------|
| `file`  | ✅ | File cần upload |
| `path`  | ❌ | Đường dẫn virtual (mặc định: `/`) |
| `share` | ❌ | Đặt `public` để nhận link chia sẻ ngay |

### Ví dụ curl

```bash
# Upload cơ bản
curl -X POST http://localhost:8091/api/upload-api/upload \
  -H "Authorization: Bearer your-token-here" \
  -F "file=@/path/to/video.mp4"

# Upload và lấy link chia sẻ
curl -X POST http://localhost:8091/api/upload-api/upload \
  -H "Authorization: Bearer your-token-here" \
  -F "file=@video.mp4" \
  -F "path=/videos" \
  -F "share=public"
```

### Response

```json
{
  "success": true,
  "id": 42,
  "name": "video.mp4",
  "size": 104857600,
  "share_url": "http://localhost:8091/share/abc123def456",
  "direct_url": "http://localhost:8091/stream/42"
}
```

---

## 🎬 Stream Video

```
GET /stream/{id}
GET /stream/share/{share_token}
```

Hỗ trợ **HTTP Range requests** — tương thích với mọi trình phát video.

---

## 🌐 Nginx Reverse Proxy

```nginx
server {
    listen 80;
    server_name your.domain.com;
    client_max_body_size 0;  # Không giới hạn upload size

    location / {
        proxy_pass http://127.0.0.1:8091;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_request_buffering off;  # Quan trọng cho upload lớn
        proxy_buffering off;          # Quan trọng cho streaming
        proxy_read_timeout 3600s;
        proxy_connect_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

---

## 📁 Cấu trúc dự án

```
telecloud/
├── main.go              # Entry point
├── config/              # Cấu hình từ .env
├── database/            # SQLite models & CRUD
├── tgclient/            # Telegram MTProto client (gotd/td)
├── api/
│   ├── handlers.go      # HTTP handlers
│   └── router.go        # Route definitions
├── middleware/          # Auth middleware
├── utils/               # Utilities (mime, thumbnail, token...)
├── templates/           # HTML templates
├── static/              # CSS, JS
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── .env.example
```

---

## 🛠️ Phát triển

```bash
# Xem tất cả lệnh
make help
```

---

## 📜 License

MIT License
