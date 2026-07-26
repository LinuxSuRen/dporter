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
	var svcAction string
	flag.StringVar(&port, "p", "", "server port (default: 8080, or $PORT)")
	flag.StringVar(&apiURL, "api", "", "remote API base URL (e.g. http://other-host:8080)")
	flag.BoolVar(&authEnabled, "auth", false, "enable HTTP Basic Auth against Linux users (/etc/shadow)")
	flag.StringVar(&svcAction, "service", "", "systemd service management: install, uninstall, status")
	flag.Parse()

	switch svcAction {
	case "install":
		if err := serviceInstall(); err != nil {
			log.Fatalf("service install: %v", err)
		}
		return
	case "uninstall":
		if err := serviceUninstall(); err != nil {
			log.Fatalf("service uninstall: %v", err)
		}
		return
	case "status":
		if err := serviceStatus(); err != nil {
			log.Fatalf("service status: %v", err)
		}
		return
	case "":
	default:
		log.Fatalf("unknown -service action: %s (use install, uninstall, or status)", svcAction)
	}

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
		mux.HandleFunc("POST /api/login", srv.handleLogin)
		mux.HandleFunc("GET /api/logout", srv.handleLogout)
		mux.HandleFunc("POST /api/containers/batch/restart", srv.handleBatchRestart)
		mux.HandleFunc("GET /api/compose/restart", srv.handleComposeRestart)
		mux.HandleFunc("GET /api/compose/restart-pull", srv.handleComposeRestartPull)
		mux.HandleFunc("GET /api/compose/pull", srv.handleComposePull)
		mux.HandleFunc("GET /api/compose/{project}/file", srv.handleComposeFileContent)
		mux.HandleFunc("GET /api/volumes", srv.handleVolumes)
		mux.HandleFunc("GET /api/volumes/{name}", srv.handleVolumeDetail)
		mux.HandleFunc("GET /api/images/info", srv.handleImageInfo)
		mux.HandleFunc("GET /api/containers", srv.handleContainers)
		mux.HandleFunc("GET /api/containers/stats", srv.handleContainerStats)
		mux.HandleFunc("GET /api/containers/stats/stream", srv.handleContainerStatsSSE)
		mux.HandleFunc("GET /api/containers/{id}/shell", srv.handleContainerShell)
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
		handler = authMiddleware(mux)
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
