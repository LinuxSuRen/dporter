package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/gorilla/websocket"
)

type ForwardInfo struct {
	ID            string `json:"id"`
	LocalPort     int    `json:"localPort"`
	ContainerID   string `json:"containerId"`
	ContainerName string `json:"containerName"`
	ContainerPort int    `json:"containerPort"`
	ContainerIP   string `json:"containerIP"`
	Protocol      string `json:"protocol"`
}

type forward struct {
	info     ForwardInfo
	listener net.Listener
	cancel   chan struct{}
}

type ForwardManager struct {
	mu        sync.Mutex
	forwards  map[string]*forward
	nextID    int
	wsClients map[*websocket.Conn]struct{}
	wsMu      sync.Mutex
}

func NewForwardManager() *ForwardManager {
	return &ForwardManager{
		forwards:  make(map[string]*forward),
		wsClients: make(map[*websocket.Conn]struct{}),
	}
}

func (fm *ForwardManager) Subscribe(conn *websocket.Conn) {
	fm.wsMu.Lock()
	fm.wsClients[conn] = struct{}{}
	fm.wsMu.Unlock()
	conn.WriteJSON(fm.List())
}

func (fm *ForwardManager) Unsubscribe(conn *websocket.Conn) {
	fm.wsMu.Lock()
	delete(fm.wsClients, conn)
	fm.wsMu.Unlock()
}

func (fm *ForwardManager) broadcast() {
	list := fm.List()
	fm.wsMu.Lock()
	defer fm.wsMu.Unlock()
	for conn := range fm.wsClients {
		if err := conn.WriteJSON(list); err != nil {
			delete(fm.wsClients, conn)
		}
	}
}

func (fm *ForwardManager) Start(info ForwardInfo) (string, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", info.LocalPort))
	if err != nil {
		return "", fmt.Errorf("failed to listen on port %d: %w", info.LocalPort, err)
	}

	fm.mu.Lock()
	fm.nextID++
	info.ID = fmt.Sprintf("fwd-%d", fm.nextID)
	f := &forward{
		info:     info,
		listener: listener,
		cancel:   make(chan struct{}),
	}
	fm.forwards[info.ID] = f
	fm.mu.Unlock()

	log.Printf("forward: localhost:%d -> %s:%d (%s)", info.LocalPort, info.ContainerIP, info.ContainerPort, info.ContainerName)

	go fm.acceptLoop(f)
	fm.broadcast()
	return info.ID, nil
}

func (fm *ForwardManager) Stop(id string) {
	fm.mu.Lock()
	f, ok := fm.forwards[id]
	if ok {
		delete(fm.forwards, id)
	}
	fm.mu.Unlock()

	if !ok {
		return
	}

	close(f.cancel)
	f.listener.Close()
	log.Printf("forward stopped: %s", id)
	fm.broadcast()
}

func (fm *ForwardManager) StopAll() {
	fm.mu.Lock()
	ids := make([]string, 0, len(fm.forwards))
	for id := range fm.forwards {
		ids = append(ids, id)
	}
	fm.mu.Unlock()

	for _, id := range ids {
		fm.Stop(id)
	}
}

func (fm *ForwardManager) List() []ForwardInfo {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	result := make([]ForwardInfo, 0, len(fm.forwards))
	for _, f := range fm.forwards {
		result = append(result, f.info)
	}
	return result
}

func (fm *ForwardManager) acceptLoop(f *forward) {
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			select {
			case <-f.cancel:
				return
			default:
			}
			continue
		}

		go fm.handleConn(conn, f)
	}
}

func (fm *ForwardManager) handleConn(clientConn net.Conn, f *forward) {
	defer clientConn.Close()

	target := fmt.Sprintf("%s:%d", f.info.ContainerIP, f.info.ContainerPort)
	targetConn, err := net.DialTimeout("tcp", target, dialTimeout)
	if err != nil {
		log.Printf("forward %s: dial %s failed: %v", f.info.ID, target, err)
		return
	}
	defer targetConn.Close()

	done := make(chan struct{}, 2)
	go func() {
		io.Copy(targetConn, clientConn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(clientConn, targetConn)
		done <- struct{}{}
	}()

	<-done
}
