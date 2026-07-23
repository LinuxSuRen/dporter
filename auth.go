package main

import (
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"hash"
	"net/http"
	"os"
	"strings"
)

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
	expectedHash := parts[3]

	hashFn := func() hash.Hash { return nil }
	switch id {
	case "6":
		hashFn = sha512.New
	case "5":
		hashFn = sha256.New
	default:
		return false
	}
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
		fields := strings.SplitN(line, ":", 2)
		if len(fields) < 2 || fields[0] != user {
			continue
		}
		return verifyShadowPassword(fields[1], password)
	}
	return false
}

func basicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || !authenticate(user, pass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="dporter"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"unauthorized"}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}
