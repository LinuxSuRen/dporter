package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	yescrypt "github.com/openwall/yescrypt-go"
)

type sessionEntry struct {
	username string
	expires  time.Time
}

type sessionStore struct {
	mu     sync.Mutex
	tokens map[string]sessionEntry
}

var sessions = &sessionStore{
	tokens: make(map[string]sessionEntry),
}

func (s *sessionStore) create(username string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)

	s.tokens[token] = sessionEntry{
		username: username,
		expires:  time.Now().Add(24 * time.Hour),
	}
	return token
}

func (s *sessionStore) validate(token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.tokens[token]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expires) {
		delete(s.tokens, token)
		return "", false
	}
	return entry.username, true
}

func (s *sessionStore) remove(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
}

func verifyShadowPassword(encrypted, password string) bool {
	if !strings.HasPrefix(encrypted, "$") {
		return false
	}
	parts := strings.Split(encrypted, "$")
	if len(parts) < 4 {
		return false
	}
	id := parts[1]
	salt := "$" + id + "$" + parts[2] + "$"

	switch id {
	case "y":
		computed, err := yescrypt.Hash([]byte(password), []byte(encrypted))
		if err != nil {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(encrypted), computed) == 1
	case "6":
		return verifySHAPassword(encrypted, password, sha512.New, salt)
	case "5":
		return verifySHAPassword(encrypted, password, sha256.New, salt)
	case "7":
		computed, err := yescrypt.Hash([]byte(password), []byte(encrypted))
		if err != nil {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(encrypted), computed) == 1
	default:
		return false
	}
}

func verifySHAPassword(encrypted, password string, hashFn func() hash.Hash, salt string) bool {
	parts := strings.Split(encrypted, "$")
	if len(parts) < 4 {
		return false
	}
	expectedHash := parts[3]
	h := hashFn()
	h.Write([]byte(password + salt))
	computed := base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString(h.Sum(nil))
	expectedBytes := []byte(strings.TrimSuffix(expectedHash, "\n"))
	computedBytes := []byte(strings.TrimSuffix(computed, "\n"))
	return subtle.ConstantTimeCompare(expectedBytes, computedBytes) == 1
}

func authenticate(user, password string) bool {
	data, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, ":", 3)
		if len(fields) < 2 || fields[0] != user {
			continue
		}
		return verifyShadowPassword(fields[1], password)
	}
	return false
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login.html" || r.URL.Path == "/api/login" || r.URL.Path == "/api/logout" {
			next.ServeHTTP(w, r)
			return
		}

		if cookie, err := r.Cookie("dporter_session"); err == nil {
			if _, ok := sessions.validate(cookie.Value); ok {
				next.ServeHTTP(w, r)
				return
			}
		}

		user, pass, ok := r.BasicAuth()
		if ok && authenticate(user, pass) {
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("WWW-Authenticate", `Basic realm="dporter"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"unauthorized"}`)
			return
		}

		http.Redirect(w, r, "/login.html", http.StatusFound)
	})
}
