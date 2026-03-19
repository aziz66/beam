package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/beam-sh/beam/internal/api"
	"github.com/beam-sh/beam/internal/config"
	"github.com/beam-sh/beam/internal/hub"
	"github.com/beam-sh/beam/internal/preview"
	"github.com/beam-sh/beam/internal/room"
)

//go:embed web/*
var webFS embed.FS

func main() {
	cfg := config.Load()

	// Initialize store for pinned rooms
	var store *room.Store
	if cfg.EnablePinnedRooms {
		if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
			log.Fatalf("failed to create data directory: %v", err)
		}
		var err error
		store, err = room.NewStore(cfg.DataDir)
		if err != nil {
			log.Fatalf("failed to open store: %v", err)
		}
		defer store.Close()
	}

	// Initialize room manager
	manager, err := room.NewManager(cfg, store)
	if err != nil {
		log.Fatalf("failed to create room manager: %v", err)
	}
	defer manager.Close()

	// Initialize hub
	h := hub.New(manager, cfg.GracePeriod)

	// Initialize API
	previewer := preview.New()
	apiHandler := api.New(manager, previewer)

	// Static files from embedded FS
	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("failed to create sub filesystem: %v", err)
	}
	staticHandler := http.FileServer(http.FS(webContent))

	mux := http.NewServeMux()

	// API endpoints
	apiHandler.Register(mux)

	// WebSocket endpoint
	mux.HandleFunc("/ws/", h.ServeWS)

	// SPA route — serve index.html for /r/{code}
	mux.HandleFunc("/r/", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/"
		staticHandler.ServeHTTP(w, r)
	})

	// Static files
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Set content type for SVG
		if strings.HasSuffix(r.URL.Path, ".svg") {
			w.Header().Set("Content-Type", "image/svg+xml")
		}
		staticHandler.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			log.Printf("beam server starting on https://%s", addr)
			if err := srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server error: %v", err)
			}
		} else {
			log.Printf("beam server starting on http://%s", addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server error: %v", err)
			}
		}
	}()

	<-done
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
	log.Println("beam server stopped")
}
