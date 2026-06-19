BINARY=telecloud
GO=go

.PHONY: all build run auth clean docker-build docker-up

all: build

# Build binary
build:
	CGO_ENABLED=1 $(GO) build -ldflags="-s -w" -o $(BINARY) .
	@echo "✓ Build thành công: ./$(BINARY)"

# Chạy server
run:
	$(GO) run .

# Xác thực Telegram (chỉ chạy lần đầu)
auth:
	$(GO) run . -auth

# Build + chạy
dev: build
	./$(BINARY)

# Cài dependencies
deps:
	$(GO) mod tidy
	$(GO) mod download

# Xóa binary
clean:
	rm -f $(BINARY)
	rm -rf ./data/temp/*

# Docker
docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# Xác thực qua Docker
docker-auth:
	docker compose run --rm telecloud /app/telecloud -auth

help:
	@echo "Các lệnh có thể dùng:"
	@echo "  make deps        - Cài Go dependencies"
	@echo "  make auth        - Xác thực Telegram (lần đầu tiên)"
	@echo "  make run         - Chạy server (go run)"
	@echo "  make build       - Build binary"
	@echo "  make dev         - Build và chạy"
	@echo "  make docker-auth - Xác thực Telegram qua Docker"
	@echo "  make docker-up   - Khởi động Docker"
	@echo "  make docker-logs - Xem logs Docker"
