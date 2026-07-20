package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type Server struct {
	fm *ForwardManager
}

type CreateForwardRequest struct {
	ContainerID   string `json:"containerId"`
	ContainerPort int    `json:"containerPort"`
	LocalPort     int    `json:"localPort"`
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

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
