package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

type Server struct {
	fm *ForwardManager
}

type CreateForwardRequest struct {
	ContainerID   string `json:"containerId"`
	ContainerPort int    `json:"containerPort"`
	LocalPort     int    `json:"localPort"`
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version":   version,
		"commit":    commit,
		"buildTime": buildTime,
	})
}

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := listContainers()
	if err != nil {
		log.Printf("list containers: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, containers)
}

func (s *Server) handleForwardsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.fm.List())
}

func (s *Server) handleForwardsWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws forwards upgrade: %v", err)
		return
	}
	s.fm.Subscribe(conn)

	go func() {
		defer s.fm.Unsubscribe(conn)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()
}

func (s *Server) handleForwardsCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateForwardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.ContainerPort <= 0 || req.ContainerPort > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid container port"})
		return
	}

	if req.LocalPort == 0 {
		req.LocalPort = req.ContainerPort
	}
	if req.LocalPort < 1 || req.LocalPort > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid local port"})
		return
	}

	containers, err := listContainers()
	if err != nil {
		log.Printf("list containers for forward: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var target *ContainerInfo
	for i := range containers {
		c := &containers[i]
		if strings.HasPrefix(c.ID, req.ContainerID) || c.ID == req.ContainerID || c.Name == req.ContainerID {
			target = c
			break
		}
	}
	if target == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "container not found"})
		return
	}

	if target.State != "running" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "container is not running"})
		return
	}

	var containerIP string
	for _, ip := range target.NetworkIPs {
		containerIP = ip
		break
	}
	if containerIP == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "container has no IP address"})
		return
	}

	info := ForwardInfo{
		LocalPort:     req.LocalPort,
		ContainerID:   target.ID,
		ContainerName: target.Name,
		ContainerPort: req.ContainerPort,
		ContainerIP:   containerIP,
		Protocol:      "tcp",
	}

	id, err := s.fm.Start(info)
	if err != nil {
		log.Printf("start forward: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleForwardDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing forward id"})
		return
	}
	s.fm.Stop(id)
	w.WriteHeader(http.StatusNoContent)
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("id")
	if containerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing container id"})
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("ws logs: container=%s", containerID)

	err = streamContainerLogs(&wsWriter{conn: conn}, containerID, 100)
	if err != nil {
		log.Printf("stream logs: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("error: %v", err)))
	}
}

type wsWriter struct {
	conn *websocket.Conn
}

func (w *wsWriter) Write(p []byte) (int, error) {
	err := w.conn.WriteMessage(websocket.TextMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *Server) handleContainerInspect(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("id")
	if containerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing container id"})
		return
	}

	httpClient, _, err := newDockerClient()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var result map[string]interface{}
	if err := dockerGet(httpClient, "containers/"+containerID+"/json", &result); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleContainerRestart(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("id")
	if containerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing container id"})
		return
	}

	httpClient, _, err := newDockerClient()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := dockerPost(httpClient, "containers/"+containerID+"/restart"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleContainerStop(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("id")
	if containerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing container id"})
		return
	}

	httpClient, _, err := newDockerClient()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := dockerPost(httpClient, "containers/"+containerID+"/stop"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleContainerPull(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("id")
	if containerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing container id"})
		return
	}

	imageRef := r.URL.Query().Get("image")
	if imageRef == "" || strings.HasPrefix(imageRef, "sha256:") {
		httpClient, _, err := newDockerClient()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		var inspect struct {
			Config *struct {
				Image string `json:"Image"`
			} `json:"Config"`
			Image string `json:"Image"`
		}
		if err := dockerGet(httpClient, "containers/"+containerID+"/json", &inspect); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "container not found"})
			return
		}
		imageRef = inspect.Config.Image
		if imageRef == "" {
			imageRef = inspect.Image
		}
		if imageRef == "" {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "container has no image reference"})
			return
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	transport, _ := newDockerTransport()
	pullClient := &http.Client{Transport: transport}

	if strings.HasPrefix(imageRef, "sha256:") {
		resolved := resolveImageTag(pullClient, imageRef)
		if resolved == "" {
			fmt.Fprintf(w, "event: pull-error\ndata: cannot resolve digest %s to a pullable tag\n\n", imageRef)
			return
		}
		imageRef = resolved
		fmt.Fprintf(w, "data: {\"status\":\"Resolved: %s\"}\n\n", imageRef)
	}

	if err := dockerPullImage(pullClient, imageRef, w); err != nil {
		fmt.Fprintf(w, "event: pull-error\ndata: %s\n\n", err.Error())
		return
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
}

func (s *Server) handleContainerStart(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("id")
	if containerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing container id"})
		return
	}

	httpClient, _, err := newDockerClient()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := dockerPost(httpClient, "containers/"+containerID+"/start"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleImageInfo(w http.ResponseWriter, r *http.Request) {
	imageName := r.URL.Query().Get("name")
	if imageName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing image name"})
		return
	}

	httpClient, _, err := newDockerClient()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var img struct {
		RepoTags     []string `json:"RepoTags"`
		Size         int64    `json:"Size"`
		Created      string   `json:"Created"`
		Architecture string   `json:"Architecture"`
		Os           string   `json:"Os"`
	}
	if err := dockerGet(httpClient, "images/"+imageName+"/json", &img); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, img)
}
