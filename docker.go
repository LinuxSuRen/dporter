package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
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

// ContainerStats holds CPU and memory metrics for a single container.
type ContainerStats struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	State          string  `json:"state"`
	CPUPercent     float64 `json:"cpuPercent"`
	MemoryUsage    int64   `json:"memoryUsage"`
	MemoryLimit    int64   `json:"memoryLimit"`
	MemoryPercent  float64 `json:"memoryPercent"`
	ComposeProject string  `json:"composeProject,omitempty"`
}

// dockerStats mirrors the Docker Engine API response for GET /containers/{id}/stats.
type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
}

// getContainerStats fetches CPU and memory stats for a single container.
func getContainerStats(httpClient *http.Client, containerID string) (*ContainerStats, error) {
	var ds dockerStats
	if err := dockerGet(httpClient, "containers/"+containerID+"/stats?stream=false", &ds); err != nil {
		return nil, err
	}

	// CPU percent: delta of container usage / delta of system usage * online cpus * 100
	cpuDelta := float64(ds.CPUStats.CPUUsage.TotalUsage - ds.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(ds.CPUStats.SystemCPUUsage - ds.PreCPUStats.SystemCPUUsage)
	var cpuPercent float64
	if systemDelta > 0 && cpuDelta > 0 {
		cpuPercent = (cpuDelta / systemDelta) * float64(ds.CPUStats.OnlineCPUs) * 100
		if cpuPercent > 100*float64(ds.CPUStats.OnlineCPUs) {
			cpuPercent = 0
		}
	}

	var memPercent float64
	if ds.MemoryStats.Limit > 0 {
		memPercent = float64(ds.MemoryStats.Usage) / float64(ds.MemoryStats.Limit) * 100
	}

	return &ContainerStats{
		CPUPercent:    math.Round(cpuPercent*100) / 100,
		MemoryUsage:   int64(ds.MemoryStats.Usage),
		MemoryLimit:   int64(ds.MemoryStats.Limit),
		MemoryPercent: math.Round(memPercent*100) / 100,
	}, nil
}

// listContainerStats fetches stats for all containers returned by the Docker API.
func listContainerStats() ([]ContainerStats, error) {
	httpClient, _, err := newDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to docker: %w", err)
	}

	var summaries []containerSummary
	if err := dockerGet(httpClient, "containers/json?all=true", &summaries); err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	result := make([]ContainerStats, 0, len(summaries))
	for _, cs := range summaries {
		stat, err := getContainerStats(httpClient, cs.ID)
		if err != nil {
			continue
		}
		stat.ID = cs.ID[:12]
		if len(cs.Names) > 0 {
			stat.Name = strings.TrimPrefix(cs.Names[0], "/")
		}
		stat.State = cs.State

		var inspect containerInspect
		if err := dockerGet(httpClient, "containers/"+cs.ID+"/json", &inspect); err == nil {
			if inspect.Config != nil && inspect.Config.Labels != nil {
				stat.ComposeProject = inspect.Config.Labels["com.docker.compose.project"]
			}
		}

		result = append(result, *stat)
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

	// Bidirectional stream — TTY mode uses raw PTY data (no multiplex headers).
	// Docker API docs: "When the TTY setting is enabled, the stream is not
	// multiplexed. The data exchanged is simply the raw data from the process PTY."
	errCh := make(chan error, 2)

	var closeOnce sync.Once
	closeConn := func() {
		closeOnce.Do(func() { conn.Close() })
	}

	if stdin != nil {
		go func() {
			_, err := io.Copy(conn, stdin)
			closeConn()
			errCh <- err
		}()
	}

	go func() {
		_, err := io.Copy(stdout, conn)
		closeConn()
		errCh <- err
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

type volumeUsageData struct {
	RefCount int `json:"RefCount"`
	Size     int `json:"Size"`
}

type dockerVolume struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	Scope      string            `json:"Scope"`
	CreatedAt  string            `json:"CreatedAt"`
	Labels     map[string]string `json:"Labels,omitempty"`
	UsageData  *volumeUsageData  `json:"UsageData,omitempty"`
}

type systemDfResponse struct {
	Volumes []dockerVolume `json:"Volumes"`
}

type VolumeInfo struct {
	Name           string `json:"name"`
	Driver         string `json:"driver"`
	Mountpoint     string `json:"mountpoint"`
	Scope          string `json:"scope"`
	CreatedAt      string `json:"createdAt"`
	RefCount       int    `json:"refCount"`
	SizeBytes      int    `json:"sizeBytes"`
	ComposeProject string `json:"composeProject,omitempty"`
}

func listVolumes() ([]VolumeInfo, error) {
	httpClient, _, err := newDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to docker: %w", err)
	}

	var resp systemDfResponse
	if err := dockerGet(httpClient, "system/df", &resp); err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	result := make([]VolumeInfo, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		info := VolumeInfo{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Scope:      v.Scope,
			CreatedAt:  v.CreatedAt,
			RefCount:   -1,
			SizeBytes:  -1,
		}
		if v.UsageData != nil {
			info.RefCount = v.UsageData.RefCount
			info.SizeBytes = v.UsageData.Size
		}
		if v.Labels != nil {
			info.ComposeProject = v.Labels["com.docker.compose.project"]
		}
		result = append(result, info)
	}

	return result, nil
}

type volumeDetail struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	Scope      string            `json:"Scope"`
	CreatedAt  string            `json:"CreatedAt"`
	Labels     map[string]string `json:"Labels,omitempty"`
	Options    map[string]string `json:"Options,omitempty"`
}

type volumeContainerRef struct {
	ContainerID   string `json:"containerId"`
	ContainerName string `json:"containerName"`
	Destination   string `json:"destination"`
	Mode          string `json:"mode"`
}

type VolumeDetail struct {
	Name           string               `json:"name"`
	Driver         string               `json:"driver"`
	Mountpoint     string               `json:"mountpoint"`
	Scope          string               `json:"scope"`
	CreatedAt      string               `json:"createdAt"`
	Labels         map[string]string    `json:"labels"`
	Options        map[string]string    `json:"options"`
	Containers     []volumeContainerRef `json:"containers"`
	ComposeProject string               `json:"composeProject,omitempty"`
}

func inspectVolumeWithContainers(name string) (*VolumeDetail, error) {
	httpClient, _, err := newDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to docker: %w", err)
	}

	var vd volumeDetail
	if err := dockerGet(httpClient, "volumes/"+name, &vd); err != nil {
		return nil, fmt.Errorf("failed to inspect volume %s: %w", name, err)
	}

	type containerMount struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Mode        string `json:"Mode"`
		RW          bool   `json:"RW"`
	}

	type containerListItem struct {
		ID     string           `json:"Id"`
		Names  []string         `json:"Names"`
		Mounts []containerMount `json:"Mounts"`
	}

	var containers []containerListItem
	if err := dockerGet(httpClient, "containers/json?all=true", &containers); err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var refs []volumeContainerRef
	for _, c := range containers {
		cName := ""
		if len(c.Names) > 0 {
			cName = strings.TrimPrefix(c.Names[0], "/")
		}
		for _, m := range c.Mounts {
			if m.Type == "volume" && m.Name == name {
				refs = append(refs, volumeContainerRef{
					ContainerID:   c.ID[:12],
					ContainerName: cName,
					Destination:   m.Destination,
					Mode:          m.Mode,
				})
			}
		}
	}

	result := &VolumeDetail{
		Name:       vd.Name,
		Driver:     vd.Driver,
		Mountpoint: vd.Mountpoint,
		Scope:      vd.Scope,
		CreatedAt:  vd.CreatedAt,
		Labels:     vd.Labels,
		Options:    vd.Options,
		Containers: refs,
	}
	if vd.Labels != nil {
		result.ComposeProject = vd.Labels["com.docker.compose.project"]
	}
	return result, nil
}
