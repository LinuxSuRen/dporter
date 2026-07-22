package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const dialTimeout = 5 * time.Second

//go:embed frontend/*
var frontendFiles embed.FS

func main() {
	var port string
	var apiURL string
	flag.StringVar(&port, "p", "", "server port (default: 8080, or $PORT)")
	flag.StringVar(&apiURL, "api", "", "remote API base URL (e.g. http://other-host:8080)")
	flag.Parse()

	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	var fm *ForwardManager
	if apiURL != "" {
		target, err := url.Parse(apiURL)
		if err != nil {
			log.Fatalf("invalid -api URL: %v", err)
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
			log.Printf("proxy: %s %s -> %s", r.Method, r.URL.Path, apiURL)
			proxy.ServeHTTP(w, r)
		})
		log.Printf("api proxy enabled: /api/* -> %s", apiURL)
	} else {
		fm = NewForwardManager()
		srv := &Server{fm: fm}
		mux.HandleFunc("GET /api/containers", srv.handleContainers)
		mux.HandleFunc("GET /api/forwards", srv.handleForwardsList)
		mux.HandleFunc("POST /api/forwards", srv.handleForwardsCreate)
		mux.HandleFunc("DELETE /api/forwards/{id}", srv.handleForwardDelete)
	}

	frontend, err := fs.Sub(frontendFiles, "frontend")
	if err != nil {
		log.Fatalf("embedded frontend: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(frontend)))

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
	if fm != nil {
		fm.StopAll()
	}
	server.Close()
}
