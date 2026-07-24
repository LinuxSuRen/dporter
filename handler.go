package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
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

type wsReadWriter struct {
	conn *websocket.Conn
	buf  []byte
}

func (rw *wsReadWriter) Read(p []byte) (int, error) {
	if len(rw.buf) > 0 {
		n := copy(p, rw.buf)
		rw.buf = rw.buf[n:]
		return n, nil
	}
	_, msg, err := rw.conn.ReadMessage()
	if err != nil {
		return 0, err
	}
	n := copy(p, msg)
	if n < len(msg) {
		rw.buf = msg[n:]
	}
	return n, nil
}

func (rw *wsReadWriter) Write(p []byte) (int, error) {
	err := rw.conn.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *Server) handleContainerShell(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("id")
	if containerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing container id"})
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade shell: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("ws shell: container=%s", containerID)

	rw := &wsReadWriter{conn: conn}
	go func() {
		if err := dockerExec(containerID, rw, rw, nil); err != nil {
			log.Printf("exec: %v", err)
		}
	}()
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

func (s *Server) handleComposeRestart(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing project name"})
		return
	}

	containers, err := listContainers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var configFile, workingDir string
	for _, c := range containers {
		if c.ComposeProject == project && c.ComposeConfigFiles != "" {
			configFile = c.ComposeConfigFiles
			workingDir = c.ComposeWorkingDir
			break
		}
	}
	if configFile == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no compose config found for " + project})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)

	sendEvent := func(evt, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt, data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	sendData := func(data string) {
		fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	// collect running service names and container names
	var names []string
	serviceMap := make(map[string]bool)
	for _, c := range containers {
		if c.ComposeProject == project && c.State == "running" {
			names = append(names, c.Name)
			if c.ComposeService != "" {
				serviceMap[c.ComposeService] = true
			}
		}
	}
	var services []string
	for s := range serviceMap {
		services = append(services, s)
	}
	namesJSON, _ := json.Marshal(names)
	sendData(fmt.Sprintf(`{"type":"restart","phase":"containers","names":%s}`, string(namesJSON)))

	// Step 1: docker compose down (only running services)
	sendData(fmt.Sprintf(`{"type":"restart","phase":"down","project":"%s","status":"running"}`, project))

	downArgs := append([]string{"compose", "-f", configFile, "down"}, services...)
	cmd := exec.Command("docker", downArgs...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	cmd.Start()

	go func() {
		scanStream(stdout, sendData)
	}()
	go func() {
		scanStream(stderr, sendData)
	}()

	if err := cmd.Wait(); err != nil {
		sendEvent("restart-error", fmt.Sprintf(`"down failed: %v"`, err))
		return
	}

	sendData(fmt.Sprintf(`{"type":"restart","phase":"down","project":"%s","status":"done"}`, project))

	// Step 2: docker compose up -d (only running services, with their profiles)
	sendData(fmt.Sprintf(`{"type":"restart","phase":"up","project":"%s","status":"running"}`, project))

	profiles := activeProfiles(configFile, services)
	args := []string{"compose", "-f", configFile}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "up", "-d")
	args = append(args, services...)

	cmd = exec.Command("docker", args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	stdout, _ = cmd.StdoutPipe()
	stderr, _ = cmd.StderrPipe()
	cmd.Start()

	go func() {
		scanStream(stdout, sendData)
	}()
	go func() {
		scanStream(stderr, sendData)
	}()

	if err := cmd.Wait(); err != nil {
		sendEvent("restart-error", fmt.Sprintf(`"up failed: %v"`, err))
		return
	}

	sendData(fmt.Sprintf(`{"type":"restart","phase":"up","project":"%s","status":"done"}`, project))
	sendEvent("done", "{}")
}

func (s *Server) handleComposeRestartPull(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing project name"})
		return
	}

	containers, err := listContainers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var configFile, workingDir string
	for _, c := range containers {
		if c.ComposeProject == project && c.ComposeConfigFiles != "" {
			configFile = c.ComposeConfigFiles
			workingDir = c.ComposeWorkingDir
			break
		}
	}
	if configFile == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no compose config found for " + project})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)

	sendEvent := func(evt, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt, data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	sendData := func(data string) {
		fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	execCmd := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		if workingDir != "" {
			cmd.Dir = workingDir
		}
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		cmd.Start()
		go func() { scanStream(stdout, sendData) }()
		go func() { scanStream(stderr, sendData) }()
		return cmd.Wait()
	}

	var names []string
	serviceMap := make(map[string]bool)
	for _, c := range containers {
		if c.ComposeProject == project && c.State == "running" {
			names = append(names, c.Name)
			if c.ComposeService != "" {
				serviceMap[c.ComposeService] = true
			}
		}
	}
	var services []string
	for s := range serviceMap {
		services = append(services, s)
	}
	namesJSON, _ := json.Marshal(names)
	sendData(fmt.Sprintf(`{"type":"restart","phase":"containers","names":%s}`, string(namesJSON)))

	// Step 1: pull images first (services stay running, minimal downtime)
	sendData(fmt.Sprintf(`{"type":"restart","phase":"pull","project":"%s","status":"running"}`, project))

	var targets []ContainerInfo
	for _, c := range containers {
		if c.ComposeProject == project && c.State == "running" {
			targets = append(targets, c)
		}
	}

	if len(targets) > 0 {
		transport, _ := newDockerTransport()
		pullClient := &http.Client{Transport: transport}

		type imageTarget struct {
			Image      string
			Containers []string
		}
		imageMap := make(map[string]*imageTarget)
		var orderedImages []string

		for _, c := range targets {
			imageRef := c.Image
			if strings.HasPrefix(imageRef, "sha256:") {
				if resolved := resolveImageTag(pullClient, imageRef); resolved != "" {
					imageRef = resolved
				}
			}
			if existing, ok := imageMap[imageRef]; ok {
				existing.Containers = append(existing.Containers, c.Name)
			} else {
				imageMap[imageRef] = &imageTarget{Image: imageRef, Containers: []string{c.Name}}
				orderedImages = append(orderedImages, imageRef)
			}
		}

		total := len(orderedImages)
		for i, imgRef := range orderedImages {
			img := imageMap[imgRef]
			for _, name := range img.Containers {
				fmt.Fprintf(w, "data: {\"type\":\"container\",\"idx\":%d,\"total\":%d,\"name\":\"%s\",\"image\":\"%s\",\"status\":\"pulling\"}\n\n", i, total, name, imgRef)
			}
			if flusher != nil {
				flusher.Flush()
			}
			if err := dockerPullImage(pullClient, imgRef, w); err != nil {
				for _, name := range img.Containers {
					fmt.Fprintf(w, "data: {\"type\":\"container\",\"idx\":%d,\"name\":\"%s\",\"status\":\"error\",\"error\":\"%s\"}\n\n", i, name, err.Error())
				}
			} else {
				for _, name := range img.Containers {
					fmt.Fprintf(w, "data: {\"type\":\"container\",\"idx\":%d,\"name\":\"%s\",\"status\":\"done\"}\n\n", i, name)
				}
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}

	sendData(fmt.Sprintf(`{"type":"restart","phase":"pull","project":"%s","status":"done"}`, project))

	// Step 2: down (only running services)
	sendData(fmt.Sprintf(`{"type":"restart","phase":"down","project":"%s","status":"running"}`, project))
	if err := execCmd("docker", append([]string{"compose", "-f", configFile, "down"}, services...)...); err != nil {
		sendEvent("restart-error", fmt.Sprintf(`"down failed: %v"`, err))
		return
	}
	sendData(fmt.Sprintf(`{"type":"restart","phase":"down","project":"%s","status":"done"}`, project))

	// Step 3: up (only running services, with their profiles)
	sendData(fmt.Sprintf(`{"type":"restart","phase":"up","project":"%s","status":"running"}`, project))
	profiles := activeProfiles(configFile, services)
	args := []string{"compose", "-f", configFile}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "up", "-d")
	args = append(args, services...)
	if err := execCmd("docker", args...); err != nil {
		sendEvent("restart-error", fmt.Sprintf(`"up failed: %v"`, err))
		return
	}
	sendData(fmt.Sprintf(`{"type":"restart","phase":"up","project":"%s","status":"done"}`, project))
	sendEvent("done", "{}")
}

func scanStream(r io.Reader, emit func(string)) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Escape the line as JSON string for safe SSE delivery
		encoded, _ := json.Marshal(line)
		emit(fmt.Sprintf(`{"type":"restart","phase":"output","text":%s}`, string(encoded)))
	}
}

func detectProfiles(configFile string) []string {
	return activeProfiles(configFile, nil)
}

func activeProfiles(configFile string, runningServices []string) []string {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil
	}

	runningSet := make(map[string]bool)
	for _, s := range runningServices {
		runningSet[s] = true
	}
	returnAll := runningServices == nil

	lines := strings.Split(string(data), "\n")

	serviceProfiles := make(map[string][]string)
	profileServices := make(map[string]map[string]bool)
	inServices := false
	currentService := ""
	inServiceProfiles := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "services:" {
			inServices = true
			continue
		}
		if !inServices {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				if strings.HasSuffix(trimmed, ":") {
					currentService = strings.TrimSuffix(trimmed, ":")
					inServiceProfiles = false
				} else {
					inServices = false
					currentService = ""
				}
			}
			continue
		}
		if currentService == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if strings.HasPrefix(trimmed, "profiles:") {
			inServiceProfiles = true
			if bracket := strings.Index(trimmed, "["); bracket != -1 {
				content := trimmed[bracket:]
				content = strings.Trim(content, "[]")
				for _, p := range strings.Split(content, ",") {
					p = strings.TrimSpace(p)
					p = strings.Trim(p, "\"'")
					if p != "" {
						serviceProfiles[currentService] = append(serviceProfiles[currentService], p)
						if profileServices[p] == nil {
							profileServices[p] = make(map[string]bool)
						}
						profileServices[p][currentService] = true
					}
				}
				inServiceProfiles = false
			}
			continue
		}
		if inServiceProfiles && strings.HasPrefix(trimmed, "- ") {
			val := strings.TrimSpace(trimmed[2:])
			val = strings.Trim(val, "\"'")
			if val != "" {
				serviceProfiles[currentService] = append(serviceProfiles[currentService], val)
				if profileServices[val] == nil {
					profileServices[val] = make(map[string]bool)
				}
				profileServices[val][currentService] = true
			}
			continue
		}
		if inServiceProfiles && strings.HasPrefix(trimmed, "-") && trimmed != "-" {
			val := strings.TrimSpace(trimmed[1:])
			val = strings.Trim(val, "\"'")
			if val != "" {
				serviceProfiles[currentService] = append(serviceProfiles[currentService], val)
				if profileServices[val] == nil {
					profileServices[val] = make(map[string]bool)
				}
				profileServices[val][currentService] = true
			}
			continue
		}
		if indent <= 4 || (!strings.HasPrefix(line, "     ") && !strings.HasPrefix(line, "\t\t")) {
			inServiceProfiles = false
		}
	}

	if returnAll {
		var result []string
		for p := range profileServices {
			result = append(result, p)
		}
		return result
	}

	var result []string
	for profile, svcSet := range profileServices {
		allRunning := true
		for svc := range svcSet {
			if !runningSet[svc] {
				allRunning = false
				break
			}
		}
		if allRunning && len(svcSet) > 0 {
			result = append(result, profile)
		}
	}
	return result
}

func (s *Server) handleBatchRestart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	httpClient, _, err := newDockerClient()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var failed []string
	for _, id := range req.IDs {
		if err := dockerPost(httpClient, "containers/"+id+"/restart"); err != nil {
			failed = append(failed, id)
		}
	}

	if len(failed) > 0 {
		writeJSON(w, http.StatusPartialContent, map[string]interface{}{
			"restarted": len(req.IDs) - len(failed),
			"failed":    failed,
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleComposePull(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing project name"})
		return
	}

	containers, err := listContainers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var targets []ContainerInfo
	for _, c := range containers {
		if c.ComposeProject == project && c.State == "running" {
			targets = append(targets, c)
		}
	}

	if len(targets) == 0 {
		fmt.Fprintf(w, "event: pull-error\ndata: no running containers in project %s\n\n", project)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	transport, _ := newDockerTransport()
	pullClient := &http.Client{Transport: transport}

	type imageTarget struct {
		Image      string
		Containers []string
	}
	imageMap := make(map[string]*imageTarget)
	var orderedImages []string

	for _, c := range targets {
		imageRef := c.Image
		if strings.HasPrefix(imageRef, "sha256:") {
			if resolved := resolveImageTag(pullClient, imageRef); resolved != "" {
				imageRef = resolved
			}
		}
		if existing, ok := imageMap[imageRef]; ok {
			existing.Containers = append(existing.Containers, c.Name)
		} else {
			imageMap[imageRef] = &imageTarget{Image: imageRef, Containers: []string{c.Name}}
			orderedImages = append(orderedImages, imageRef)
		}
	}

	flusher, _ := w.(http.Flusher)
	total := len(orderedImages)

	for i, imgRef := range orderedImages {
		img := imageMap[imgRef]

		for _, name := range img.Containers {
			fmt.Fprintf(w, "data: {\"type\":\"container\",\"idx\":%d,\"total\":%d,\"name\":\"%s\",\"image\":\"%s\",\"status\":\"pulling\"}\n\n", i, total, name, imgRef)
		}
		if flusher != nil {
			flusher.Flush()
		}

		if err := dockerPullImage(pullClient, imgRef, w); err != nil {
			for _, name := range img.Containers {
				fmt.Fprintf(w, "data: {\"type\":\"container\",\"idx\":%d,\"name\":\"%s\",\"status\":\"error\",\"error\":\"%s\"}\n\n", i, name, err.Error())
			}
		} else {
			for _, name := range img.Containers {
				fmt.Fprintf(w, "data: {\"type\":\"container\",\"idx\":%d,\"name\":\"%s\",\"status\":\"done\"}\n\n", i, name)
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
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

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
