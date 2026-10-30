// Package server exposes the collector over HTTP: a JSON API, an SSE stream
// and the embedded Svelte UI.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nodetrack/internal/collector"
	"nodetrack/internal/store"
)

type Server struct {
	C          *collector.Collector
	UI         fs.FS  // built Svelte app; nil serves a placeholder
	User, Pass string // basic auth; disabled when User is empty
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/overview", s.overview)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("GET /api/apps/{id}", s.detail)
	mux.HandleFunc("GET /api/apps/{id}/history", s.history)
	mux.HandleFunc("DELETE /api/apps/{id}", s.forget)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/", s.static())
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	if s.User == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(u), []byte(s.User)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(s.Pass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="nodetrack"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) static() http.Handler {
	if s.UI == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI not built: run `make ui`", http.StatusNotFound)
		})
	}
	files := http.FileServerFS(s.UI)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			// Vite fingerprints asset names, so they never change.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	ov := s.C.Overview()
	if ov == nil {
		writeErr(w, http.StatusServiceUnavailable, "warming up")
		return
	}
	writeJSON(w, ov)
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // tell nginx not to buffer the stream

	ch, cancel := s.C.Subscribe()
	defer cancel()

	if ov := s.C.Overview(); ov != nil {
		b, _ := json.Marshal(ov)
		writeEvent(w, b)
	}
	flusher.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			writeEvent(w, msg)
			flusher.Flush()
		case <-keepalive.C:
			w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func writeEvent(w http.ResponseWriter, data []byte) {
	w.Write([]byte("event: overview\ndata: "))
	w.Write(data)
	w.Write([]byte("\n\n"))
}

func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	d, err := s.C.Detail(id)
	if errors.Is(err, collector.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "app not found")
		return
	}
	if err != nil {
		slog.Error("detail", "id", id, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, d)
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "1h"
	}
	h, err := s.C.History(id, rng)
	if errors.Is(err, store.ErrBadRange) {
		writeErr(w, http.StatusBadRequest, "range must be one of 1h, 6h, 24h, 7d, 30d")
		return
	}
	if err != nil {
		slog.Error("history", "id", id, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, h)
}

func (s *Server) forget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	switch err := s.C.Forget(r.Context(), id); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, collector.ErrNotFound):
		writeErr(w, http.StatusNotFound, "app not found")
	case errors.Is(err, collector.ErrRunning):
		writeErr(w, http.StatusConflict, "app is running; stop it first")
	default:
		slog.Error("forget", "id", id, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("write json", "err", err)
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
