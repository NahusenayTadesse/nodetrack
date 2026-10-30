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
	"slices"
	"strconv"
	"strings"
	"time"

	"nodetrack/internal/collector"
	"nodetrack/internal/control"
	"nodetrack/internal/procfs"
	"nodetrack/internal/proxy"
	"nodetrack/internal/store"
)

type Server struct {
	C          *collector.Collector
	Ctl        *control.Controller
	Px         *proxy.Resolver
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
	mux.HandleFunc("POST /api/apps/{id}/action", s.action)
	mux.HandleFunc("POST /api/apps/{id}/jobs", s.startJob)
	mux.HandleFunc("POST /api/apps/{id}/domains", s.pointDomain)
	mux.HandleFunc("GET /api/jobs/{job}", s.job)
	mux.HandleFunc("GET /api/jobs/{job}/stream", s.jobStream)
	mux.HandleFunc("POST /api/jobs/{job}/cancel", s.cancelJob)
	mux.HandleFunc("GET /api/domains", s.domains)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/", s.static())
	return s.auth(csrf(mux))
}

// csrf requires a custom header on every state-changing request. Browsers
// can't add one cross-site without a CORS preflight, which is never granted,
// so a malicious page can't ride on the browser's cached basic-auth login.
func csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("X-Nodetrack") != "1" {
				writeErr(w, http.StatusForbidden, "missing X-Nodetrack header")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
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
	t, _ := s.C.Target(id)
	writeJSON(w, struct {
		*collector.AppDetail
		Control control.Availability `json:"control"`
		Jobs    []control.JobInfo    `json:"jobs"`
	}{d, s.Ctl.Available(t), s.Ctl.JobsFor(id, 10)})
}

func (s *Server) target(w http.ResponseWriter, r *http.Request) (collector.Target, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return collector.Target{}, false
	}
	t, ok := s.C.Target(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "app not found")
	}
	return t, ok
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON body")
		return false
	}
	return true
}

// controlErr maps controller errors to HTTP statuses.
func controlErr(w http.ResponseWriter, err error) {
	var refused control.Refused
	switch {
	case errors.Is(err, control.ErrDisabled):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, control.ErrBusy):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.As(err, &refused):
		writeErr(w, http.StatusUnprocessableEntity, refused.Msg)
	default:
		slog.Error("control", "err", err)
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	t, ok := s.target(w, r)
	if !ok {
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if !decode(w, r, &body) {
		return
	}
	out, err := s.Ctl.Action(t, body.Action)
	if err != nil {
		controlErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "output": out})
}

func (s *Server) startJob(w http.ResponseWriter, r *http.Request) {
	t, ok := s.target(w, r)
	if !ok {
		return
	}
	var body struct {
		Kind   string `json:"kind"`
		Script string `json:"script"`
	}
	if !decode(w, r, &body) {
		return
	}
	j, err := s.Ctl.StartJob(t, body.Kind, body.Script)
	if err != nil {
		controlErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(j.View(false))
}

func (s *Server) findJob(w http.ResponseWriter, r *http.Request) *control.Job {
	j := s.Ctl.Job(r.PathValue("job"))
	if j == nil {
		writeErr(w, http.StatusNotFound, "job not found")
	}
	return j
}

func (s *Server) job(w http.ResponseWriter, r *http.Request) {
	if j := s.findJob(w, r); j != nil {
		writeJSON(w, j.View(true))
	}
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	j := s.findJob(w, r)
	if j == nil {
		return
	}
	if _, err := s.Ctl.Cancel(j.ID); err != nil {
		controlErr(w, err)
		return
	}
	writeJSON(w, j.View(false))
}

func (s *Server) jobStream(w http.ResponseWriter, r *http.Request) {
	j := s.findJob(w, r)
	if j == nil {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")

	send := func(event string, v any) {
		b, _ := json.Marshal(v)
		w.Write([]byte("event: " + event + "\ndata: "))
		w.Write(b)
		w.Write([]byte("\n\n"))
		flusher.Flush()
	}
	var off int64
	first := true
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		data, next, changed := j.ReadFrom(off)
		if data != "" || first {
			send("output", data)
			first = false
		}
		off = next
		select {
		case <-j.Done():
			if data, _, _ := j.ReadFrom(off); data != "" {
				send("output", data)
			}
			send("done", j.View(false))
			return
		case <-changed:
		case <-keepalive.C:
			w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) entries() []proxy.Entry {
	targets := s.C.Targets()
	// Prefer running apps when two apps claim the same port.
	slices.SortFunc(targets, func(a, b collector.Target) int {
		if a.Up != b.Up {
			if a.Up {
				return -1
			}
			return 1
		}
		return int(a.ID - b.ID)
	})
	apps := make([]proxy.AppListeners, len(targets))
	for i, t := range targets {
		apps[i] = proxy.AppListeners{ID: t.ID, Name: t.Name, Ports: t.Ports, Sockets: t.Sockets}
	}
	es := s.Px.Entries(apps)
	if !s.Ctl.Enabled() {
		for i := range es {
			es[i].Editable, es[i].Reason = false, "controls are off on this server"
		}
	}
	return es
}

func (s *Server) domains(w http.ResponseWriter, r *http.Request) {
	es := s.entries()
	if es == nil {
		es = []proxy.Entry{}
	}
	writeJSON(w, es)
}

func (s *Server) pointDomain(w http.ResponseWriter, r *http.Request) {
	t, ok := s.target(w, r)
	if !ok {
		return
	}
	var body struct {
		Domain string `json:"domain"`
		Port   int    `json:"port"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !s.Ctl.Enabled() {
		controlErr(w, control.ErrDisabled)
		return
	}
	var entry *proxy.Entry
	for _, e := range s.entries() {
		if e.ID == body.Domain {
			entry = &e
			break
		}
	}
	vhost, backend, isOLS := proxy.OLSEntry(body.Domain)
	switch {
	case entry == nil:
		writeErr(w, http.StatusUnprocessableEntity, "unknown domain")
		return
	case !entry.Editable || !isOLS:
		writeErr(w, http.StatusUnprocessableEntity, "this domain can't be changed here: "+entry.Reason)
		return
	}
	var l *procfs.Listener
	for i := range t.Ports {
		if body.Port == 0 || t.Ports[i].Port == body.Port {
			l = &t.Ports[i]
			break
		}
	}
	if l == nil {
		writeErr(w, http.StatusUnprocessableEntity, "the app doesn't listen on that TCP port")
		return
	}
	var addr string
	switch {
	case l.Addr == "0.0.0.0" || l.Addr == "::" || l.Addr == "":
		addr = "127.0.0.1:" + strconv.Itoa(l.Port)
	case l.Addr == "::1":
		addr = "[::1]:" + strconv.Itoa(l.Port)
	case !strings.Contains(l.Addr, ":"):
		addr = l.Addr + ":" + strconv.Itoa(l.Port)
	default:
		writeErr(w, http.StatusUnprocessableEntity, "the app only listens on "+l.Addr+", which the proxy can't target")
		return
	}
	label := entry.Hosts[0]
	if entry.Path != "/" {
		label += entry.Path
	}
	out, err := s.Ctl.PointDomain(t, vhost, backend, addr, label)
	if err != nil {
		controlErr(w, err)
		return
	}
	s.Px.RefreshNow(r.Context())
	writeJSON(w, map[string]any{"ok": true, "output": out})
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
