package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type ContainerPortInfo struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	HostPort *int   `json:"hostPort"`
}

type ContainerInfo struct {
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	Image             string              `json:"image"`
	State             string              `json:"state"`
	NetworkIPs        map[string]string   `json:"networkIps"`
	Ports             []ContainerPortInfo `json:"ports"`
	ComposeProject    string              `json:"composeProject,omitempty"`
	ComposeConfigFiles string             `json:"composeConfigFiles,omitempty"`
	ComposeWorkingDir string              `json:"composeWorkingDir,omitempty"`
	ComposeService    string              `json:"composeService,omitempty"`
}

type containerSummary struct {
	ID              string   `json:"Id"`
	Names           []string `json:"Names"`
	Image           string   `json:"Image"`
	State           string   `json:"State"`
	NetworkSettings *struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

type containerInspect struct {
	Config *struct {
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
		Labels       map[string]string   `json:"Labels"`
	} `json:"Config"`
	HostConfig *struct {
		PortBindings map[string][]struct {
			HostPort string `json:"HostPort"`
		} `json:"PortBindings"`
	} `json:"HostConfig"`
	NetworkSettings *struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

func newDockerTransport() (*http.Transport, string) {
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	socketAddr := strings.TrimPrefix(host, "unix://")
	socketAddr = strings.TrimPrefix(socketAddr, "tcp://")

	proto := "unix"
	if host != "" && strings.HasPrefix(host, "tcp://") {
		proto = "tcp"
	} else if _, err := os.Stat(socketAddr); os.IsNotExist(err) {
		proto = "tcp"
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := &net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, proto, socketAddr)
		},
	}
	return transport, socketAddr
}

func newDockerClient() (*http.Client, string, error) {
	transport, socketAddr := newDockerTransport()
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}, socketAddr, nil
}

func dockerGet(httpClient *http.Client, path string, v interface{}) error {
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost/v1.43/%s", path), nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("docker GET %s failed: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("docker GET %s: HTTP %s%s", path, resp.Status, suffix(body))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func dockerPost(httpClient *http.Client, path string) error {
	req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost/v1.43/%s", path), nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("docker POST %s failed: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("docker POST %s: HTTP %s%s", path, resp.Status, suffix(body))
	}
	return nil
}

func suffix(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return " - " + string(b)
}

func dockerPullImage(httpClient *http.Client, imageRef string, progress io.Writer) error {
	image, tag := parseImageRef(imageRef)
	u, _ := url.Parse("http://localhost/v1.43/images/create")
	q := u.Query()
	q.Set("fromImage", image)
	q.Set("tag", tag)
	u.RawQuery = q.Encode()
	req, err := http.NewRequest("POST", u.String(), nil)
	if err != nil {
		return err
	}

	if auth := getRegistryAuth(image); auth != "" {
		req.Header.Set("X-Registry-Auth", auth)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pull %q failed: %w", imageRef, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("pull %q: HTTP %s%s", imageRef, resp.Status, suffix(body))
	}

	if progress == nil {
		return nil
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var check struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(line), &check) == nil && check.Error != "" {
			fmt.Fprintf(progress, "event: pull-error\ndata: %s\n\n", line)
			if f, ok := progress.(http.Flusher); ok {
				f.Flush()
			}
			return fmt.Errorf("pull %q: %s", imageRef, check.Error)
		}
		fmt.Fprintf(progress, "data: %s\n\n", line)
		if f, ok := progress.(http.Flusher); ok {
			f.Flush()
		}
	}
	return scanner.Err()
}

func parseImageRef(ref string) (image, tag string) {
	tag = "latest"
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		afterColon := ref[idx+1:]
		if !strings.Contains(afterColon, "/") {
			image = ref[:idx]
			tag = afterColon
			return
		}
	}
	image = ref
	return
}

func getRegistryAuth(image string) string {
	configPath := os.Getenv("DOCKER_CONFIG")
	if configPath == "" {
		home, _ := os.UserHomeDir()
		configPath = home + "/.docker/config.json"
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	registry := image
	if idx := strings.Index(registry, "/"); idx != -1 {
		registry = registry[:idx]
	}
	if cred, ok := cfg.Auths[registry]; ok && cred.Auth != "" {
		return base64EncodeAuth(cred.Auth)
	}
	return ""
}

func base64EncodeAuth(encoded string) string {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return ""
	}
	authObj := map[string]string{
		"username": parts[0],
		"password": parts[1],
	}
	jsonBytes, _ := json.Marshal(authObj)
	return base64.StdEncoding.EncodeToString(jsonBytes)
}

func resolveImageTag(httpClient *http.Client, digestRef string) string {
	var imgInfo struct {
		RepoTags []string `json:"RepoTags"`
	}
	if err := dockerGet(httpClient, "images/"+digestRef+"/json", &imgInfo); err == nil && len(imgInfo.RepoTags) > 0 {
		return imgInfo.RepoTags[0]
	}
	return ""
}

func listContainers() ([]ContainerInfo, error) {
	httpClient, _, err := newDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to docker: %w", err)
	}

	var summaries []containerSummary
	if err := dockerGet(httpClient, "containers/json?all=true", &summaries); err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	result := make([]ContainerInfo, 0, len(summaries))
	for _, cs := range summaries {
		var inspect containerInspect
		if err := dockerGet(httpClient, "containers/"+cs.ID+"/json", &inspect); err != nil {
			continue
		}

		info := ContainerInfo{
			ID:         cs.ID[:12],
			Image:      cs.Image,
			State:      cs.State,
			NetworkIPs: make(map[string]string),
		}
		if len(cs.Names) > 0 {
			info.Name = strings.TrimPrefix(cs.Names[0], "/")
		}

		if inspect.NetworkSettings != nil {
			for netName, netCfg := range inspect.NetworkSettings.Networks {
				if netCfg.IPAddress != "" {
					info.NetworkIPs[netName] = netCfg.IPAddress
				}
			}
		}

		if inspect.Config != nil {
			for portKey := range inspect.Config.ExposedPorts {
				p, prot := parseDockerPort(portKey)
				cp := ContainerPortInfo{Port: p, Protocol: prot}

				if inspect.HostConfig != nil {
					if bindings, ok := inspect.HostConfig.PortBindings[portKey]; ok && len(bindings) > 0 {
						if hp, err := strconv.Atoi(bindings[0].HostPort); err == nil {
							cp.HostPort = &hp
						}
					}
				}

				info.Ports = append(info.Ports, cp)
			}

			if inspect.Config.Labels != nil {
				info.ComposeProject = inspect.Config.Labels["com.docker.compose.project"]
				info.ComposeConfigFiles = inspect.Config.Labels["com.docker.compose.project.config_files"]
				info.ComposeWorkingDir = inspect.Config.Labels["com.docker.compose.project.working_dir"]
				info.ComposeService = inspect.Config.Labels["com.docker.compose.service"]
			}
		}

		result = append(result, info)
	}

	return result, nil
}

// dockerGetStream makes a streaming GET request to the Docker API and returns the response body.
// The caller must close the response body.
func dockerGetStream(httpClient *http.Client, path string) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost/v1.43/%s", path), nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker stream %s failed: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		return nil, fmt.Errorf("docker stream %s: HTTP %s%s", path, resp.Status, suffix(body))
	}
	return resp.Body, nil
}

// streamContainerLogs connects to the Docker logs API and writes demultiplexed log lines to w.
// Docker multiplexes stdout/stderr into a stream with 8-byte headers:
//
//	header: [stream_type(1), 0, 0, 0, size(4 bytes big-endian)]
//	stream_type: 1=stdout, 2=stderr
func streamContainerLogs(w io.Writer, containerID string, tail int) error {
	transport, _ := newDockerTransport()
	httpClient := &http.Client{Transport: transport}

	path := fmt.Sprintf("containers/%s/logs?stdout=1&stderr=1&follow=1&tail=%d", containerID, tail)
	body, err := dockerGetStream(httpClient, path)
	if err != nil {
		return err
	}
	defer body.Close()

	header := make([]byte, 8)
	for {
		_, err := io.ReadFull(body, header)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 {
			continue
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(body, buf); err != nil {
			return err
		}
		if _, err := w.Write(buf); err != nil {
			return err
		}
	}
}

func parseDockerPort(portKey string) (int, string) {
	parts := strings.SplitN(portKey, "/", 2)
	port, _ := strconv.Atoi(parts[0])
	prot := "tcp"
	if len(parts) == 2 {
		prot = parts[1]
	}
	return port, prot
}

func dockerExec(containerID string, stdin io.Reader, stdout io.Writer, resize <-chan [2]int) error {
	transport, _ := newDockerTransport()
	client := &http.Client{Transport: transport}

	// Create exec instance
	createBody, _ := json.Marshal(map[string]interface{}{
		"AttachStdin":  true,
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          true,
		"Env":          []string{"TERM=xterm-256color"},
		"Cmd":          []string{"/bin/sh"},
	})
	req, err := http.NewRequest("POST", "http://localhost/v1.43/containers/"+containerID+"/exec", io.NopCloser(strings.NewReader(string(createBody))))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("exec create: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("exec create: HTTP %s - %s", resp.Status, string(body))
	}
	var createResult struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createResult); err != nil {
		return fmt.Errorf("exec create decode: %w", err)
	}

	// Start exec with hijacked connection
	startBody, _ := json.Marshal(map[string]interface{}{
		"Detach": false,
		"Tty":    true,
	})
	startReq, err := http.NewRequest("POST", "http://localhost/v1.43/exec/"+createResult.ID+"/start", io.NopCloser(strings.NewReader(string(startBody))))
	if err != nil {
		return err
	}
	startReq.Header.Set("Content-Type", "application/json")
	startReq.Header.Set("Connection", "Upgrade")
	startReq.Header.Set("Upgrade", "tcp")

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	proto, addr := "unix", strings.TrimPrefix(os.Getenv("DOCKER_HOST"), "unix://")
	if addr == "" {
		addr = "/var/run/docker.sock"
	}
	if strings.HasPrefix(os.Getenv("DOCKER_HOST"), "tcp://") {
		proto = "tcp"
		addr = strings.TrimPrefix(os.Getenv("DOCKER_HOST"), "tcp://")
	}

	conn, err := dialer.DialContext(context.Background(), proto, addr)
	if err != nil {
		return fmt.Errorf("dial docker: %w", err)
	}
	defer conn.Close()

	if err := startReq.Write(conn); err != nil {
		return fmt.Errorf("write exec start: %w", err)
	}

	// Read HTTP response
	var respBuf []byte
	b := make([]byte, 1)
	for {
		if _, err := io.ReadFull(conn, b); err != nil {
			return fmt.Errorf("read response: %w", err)
		}
		respBuf = append(respBuf, b[0])
		if len(respBuf) >= 4 && respBuf[len(respBuf)-4] == '\r' && respBuf[len(respBuf)-3] == '\n' && respBuf[len(respBuf)-2] == '\r' && respBuf[len(respBuf)-1] == '\n' {
			break
		}
	}
	respLine := string(respBuf)
	if !strings.Contains(respLine, "200") && !strings.Contains(respLine, "101") {
		return fmt.Errorf("exec start: %s", strings.TrimSpace(strings.SplitN(respLine, "\r\n", 2)[0]))
	}

	// Bidirectional stream with Docker multiplex protocol
	errCh := make(chan error, 2)

	// stdin -> Docker
	if stdin != nil {
		go func() {
			buf := make([]byte, 32*1024)
			for {
				n, err := stdin.Read(buf)
				if n > 0 {
					header := []byte{0, 0, 0, 0, 0, 0, 0, 0}
					binary.BigEndian.PutUint32(header[4:], uint32(n))
					if _, werr := conn.Write(header); werr != nil {
						errCh <- werr
						return
					}
					if _, werr := conn.Write(buf[:n]); werr != nil {
						errCh <- werr
						return
					}
				}
				if err != nil {
					errCh <- nil
					return
				}
			}
		}()
	}

	// Docker -> stdout/stderr
	go func() {
		headerBuf := make([]byte, 8)
		for {
			if _, err := io.ReadFull(conn, headerBuf); err != nil {
				errCh <- nil
				return
			}
			size := binary.BigEndian.Uint32(headerBuf[4:])
			if size > 0 {
				if _, err := io.CopyN(stdout, conn, int64(size)); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	// Resize handler
	if resize != nil {
		go func() {
			for dims := range resize {
				w, h := dims[0], dims[1]
				resizeBody, _ := json.Marshal(map[string]int{"Height": h, "Width": w})
				req, _ := http.NewRequest("POST", "http://localhost/v1.43/exec/"+createResult.ID+"/resize", io.NopCloser(strings.NewReader(string(resizeBody))))
				req.Header.Set("Content-Type", "application/json")
				client.Do(req)
			}
		}()
	}

	return <-errCh
}
