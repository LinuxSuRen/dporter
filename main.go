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

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

//go:embed frontend/*
var frontendFiles embed.FS

func main() {
	var port string
	var apiURL string
	var authEnabled bool
	flag.StringVar(&port, "p", "", "server port (default: 8080, or $PORT)")
	flag.StringVar(&apiURL, "api", "", "remote API base URL (e.g. http://other-host:8080)")
	flag.BoolVar(&authEnabled, "auth", false, "enable HTTP Basic Auth against Linux users (/etc/shadow)")
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
		proxy.Transport = &http.Transport{Proxy: nil}
		mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
			log.Printf("proxy: %s %s -> %s", r.Method, r.URL.Path, apiURL)
			proxy.ServeHTTP(w, r)
		})
		log.Printf("api proxy enabled: /api/* -> %s", apiURL)
	} else {
		fm = NewForwardManager()
		srv := &Server{fm: fm}
		mux.HandleFunc("GET /api/version", srv.handleVersion)
		mux.HandleFunc("GET /api/images/info", srv.handleImageInfo)
		mux.HandleFunc("GET /api/containers", srv.handleContainers)
		mux.HandleFunc("GET /api/containers/{id}/logs", srv.handleContainerLogs)
		mux.HandleFunc("GET /api/containers/{id}/inspect", srv.handleContainerInspect)
		mux.HandleFunc("POST /api/containers/{id}/restart", srv.handleContainerRestart)
		mux.HandleFunc("POST /api/containers/{id}/stop", srv.handleContainerStop)
		mux.HandleFunc("POST /api/containers/{id}/start", srv.handleContainerStart)
		mux.HandleFunc("GET /api/containers/{id}/pull", srv.handleContainerPull)
		mux.HandleFunc("GET /api/forwards", srv.handleForwardsList)
		mux.HandleFunc("GET /api/forwards/ws", srv.handleForwardsWS)
		mux.HandleFunc("POST /api/forwards", srv.handleForwardsCreate)
		mux.HandleFunc("DELETE /api/forwards/{id}", srv.handleForwardDelete)
	}

	frontend, err := fs.Sub(frontendFiles, "frontend")
	if err != nil {
		log.Fatalf("embedded frontend: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(frontend)))

	var handler http.Handler = mux
	if authEnabled {
		handler = basicAuthMiddleware(mux)
	}

	server := &http.Server{Addr: ":" + port, Handler: handler}

	go func() {
		authMsg := ""
		if authEnabled {
			authMsg = " (auth enabled)"
		}
		log.Printf("dporter listening on http://localhost:%s%s", port, authMsg)
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
