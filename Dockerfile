FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o telecloud .

# ─── Runtime ───────────────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ffmpeg ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/telecloud .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static

RUN mkdir -p /app/data

ENV PORT=8091
ENV DATABASE_PATH=/app/data/telecloud.db
ENV THUMBS_DIR=/app/data/thumbs
ENV TEMP_DIR=/app/data/temp

EXPOSE 8091

VOLUME ["/app/data"]

ENTRYPOINT ["/app/telecloud"]
