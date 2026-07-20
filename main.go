package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const dialTimeout = 5 * time.Second

//go:embed frontend/*
var frontendFiles embed.FS

func main() {
	fm := NewForwardManager()
	srv := &Server{fm: fm}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/containers", srv.handleContainers)
	mux.HandleFunc("GET /api/forwards", srv.handleForwardsList)
	mux.HandleFunc("POST /api/forwards", srv.handleForwardsCreate)
	mux.HandleFunc("DELETE /api/forwards/{id}", srv.handleForwardDelete)

	frontend, err := fs.Sub(frontendFiles, "frontend")
	if err != nil {
		log.Fatalf("embedded frontend: %v", err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(frontend)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{Addr: ":" + port, Handler: mux}

	go func() {
		log.Printf("dporter listening on http://localhost:%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	fm.StopAll()
	server.Close()
}
