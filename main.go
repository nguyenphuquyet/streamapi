package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"telecloud/api"
	"telecloud/config"
	"telecloud/database"
	"telecloud/tgclient"
)

func main() {
	authMode := flag.Bool("auth", false, "Chạy chế độ xác thực Telegram")
	flag.Parse()

	// Load cấu hình
	config.Load()

	// Khởi tạo database
	if err := database.Init(config.App.DatabasePath); err != nil {
		log.Fatalf("[main] Lỗi khởi tạo database: %v", err)
	}

	// Khởi tạo Telegram client
	sessionPath := "./data/session.json"
	os.MkdirAll("./data", 0755)

	tg, err := tgclient.Init(sessionPath)
	if err != nil {
		log.Fatalf("[main] Lỗi khởi tạo Telegram client: %v", err)
	}

	ctx := context.Background()

	// Chế độ xác thực
	if *authMode {
		log.Println("[main] ═══ Chế độ xác thực Telegram ═══")
		if err := tg.RunAuth(ctx); err != nil {
			log.Fatalf("[main] Lỗi xác thực: %v", err)
		}
		log.Println("[main] Xác thực thành công! Chạy lại mà không có flag -auth để khởi động server.")
		return
	}

	// Khởi động Telegram client trong background
	tgCtx, tgCancel := context.WithCancel(ctx)
	defer tgCancel()

	go func() {
		if err := tg.Run(tgCtx); err != nil && err != context.Canceled {
			log.Printf("[main] Telegram client lỗi: %v", err)
			os.Exit(1)
		}
	}()

	// Chờ Telegram client sẵn sàng
	log.Println("[main] Đang kết nối Telegram...")
	tg.WaitReady()

	// Khởi động HTTP server
	router := api.SetupRouter()
	srv := &http.Server{
		Addr:         ":" + config.App.Port,
		Handler:      router,
		ReadTimeout:  0, // Không giới hạn cho upload lớn
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("[main] ═══════════════════════════════════════")
		log.Printf("[main]  TeleCloud đang chạy tại:")
		log.Printf("[main]  http://localhost:%s", config.App.Port)
		log.Printf("[main] ═══════════════════════════════════════")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[main] Lỗi server: %v", err)
		}
	}()

	<-quit
	log.Println("[main] Đang tắt server...")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	srv.Shutdown(shutCtx)
	tgCancel()

	log.Println("[main] Server đã dừng.")
}
