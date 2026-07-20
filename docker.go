package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Image      string              `json:"image"`
	State      string              `json:"state"`
	NetworkIPs map[string]string   `json:"networkIps"`
	Ports      []ContainerPortInfo `json:"ports"`
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

func newDockerClient() (*http.Client, string, error) {
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

	return &http.Client{Transport: transport, Timeout: 15 * time.Second}, socketAddr, nil
}

func dockerGet(httpClient *http.Client, path string, v interface{}) error {
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost/v1.43/%s", path), nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("docker api request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("docker api error: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func listContainers() ([]ContainerInfo, error) {
	httpClient, _, err := newDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to docker: %w", err)
	}

	var summaries []containerSummary
	if err := dockerGet(httpClient, "containers/json", &summaries); err != nil {
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
		}

		result = append(result, info)
	}

	return result, nil
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
