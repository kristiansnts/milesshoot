// Package server menyajikan UI + status ke HP di Wi-Fi yang sama (fitur "pantau dari HP").
// Memakai Engine yang sama dengan jendela desktop. Hub mengimplementasikan core.Emitter:
// setiap event engine (status/log/change) didorong ke semua HP lewat SSE.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"fotonaik/internal/core"
)

type sseMsg struct {
	event string
	data  []byte
}

type Hub struct {
	mu      sync.Mutex
	clients map[chan sseMsg]bool
}

func NewHub() *Hub { return &Hub{clients: map[chan sseMsg]bool{}} }

// Emit & Log memenuhi core.Emitter.
func (h *Hub) Emit(event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		b = []byte("null")
	}
	h.broadcast(sseMsg{event: event, data: b})
}

func (h *Hub) Log(level, msg string) {
	h.Emit("log", map[string]any{"level": level, "msg": msg, "t": time.Now().UnixMilli()})
}

func (h *Hub) broadcast(m sseMsg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- m:
		default: // klien lambat: lewati, jangan blokir engine
		}
	}
}

func (h *Hub) add() chan sseMsg {
	ch := make(chan sseMsg, 64)
	h.mu.Lock()
	h.clients[ch] = true
	h.mu.Unlock()
	return ch
}

func (h *Hub) remove(ch chan sseMsg) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

type Server struct {
	eng    *core.Engine
	hub    *Hub
	assets fs.FS

	// AfterConfig dipanggil setelah config diubah dari HP, supaya efek sampingnya
	// sama dengan perubahan dari jendela desktop (pindai ulang, nyalakan ulang FTP, dll).
	AfterConfig func()
}

func New(eng *core.Engine, hub *Hub, assets fs.FS) *Server {
	return &Server{eng: eng, hub: hub, assets: assets}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/account", s.handleAccount)
	mux.HandleFunc("/api/ping", s.handlePing)
	mux.HandleFunc("/api/watermark/preview", s.handleWatermarkPreview)
	mux.HandleFunc("/api/pause", s.post(func() { s.eng.Queue.Pause("manual"); s.eng.BroadcastStatus() }))
	mux.HandleFunc("/api/resume", s.post(func() { s.eng.Queue.Resume(); s.eng.BroadcastStatus() }))

	fileServer := http.FileServer(http.FS(s.assets))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// SPA: file statis bila ada, selain itu index.html
		if r.URL.Path != "/" {
			if f, err := s.assets.Open(trimSlash(r.URL.Path)); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		s.serveIndex(w, r)
	})
	return mux
}

func (s *Server) Listen(addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	f, err := s.assets.Open("index.html")
	if err != nil {
		http.Error(w, "UI belum dibangun", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, f)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming tidak didukung", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.hub.add()
	defer s.hub.remove(ch)

	// kirim status awal supaya HP langsung terisi
	if b, err := json.Marshal(s.eng.Status()); err == nil {
		writeSSE(w, "status", b)
		fl.Flush()
	}
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-ch:
			writeSSE(w, m.event, m.data)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func writeSSE(w io.Writer, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.eng.Status())
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.eng.Fotoyu.Session())
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	ms, err := s.eng.Fotoyu.Ping()
	if err != nil {
		ms = -1
	}
	writeJSON(w, map[string]any{"ms": ms})
}

// handleWatermarkPreview: pratinjau hidup watermark (gaya MAS EDY) — GET /api/watermark/preview?wm=<json>.
// wm = patch parsial opsional (boleh belum disimpan) di atas watermark yang sedang tersimpan.
func (s *Server) handleWatermarkPreview(w http.ResponseWriter, r *http.Request) {
	wm := s.eng.Cfg.Get().Watermark
	if q := r.URL.Query().Get("wm"); q != "" {
		_ = json.Unmarshal([]byte(q), &wm)
	}
	wm.Enabled = true
	buf, err := core.WatermarkPreviewImage(wm)
	if err != nil {
		http.Error(w, "pratinjau gagal", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		cfg, _ := s.eng.Cfg.Update(body)
		if s.AfterConfig != nil {
			s.AfterConfig()
		}
		s.eng.BroadcastStatus()
		writeJSON(w, cfg)
		return
	}
	writeJSON(w, s.eng.Cfg.Get())
}

func (s *Server) post(fn func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "pakai POST", http.StatusMethodNotAllowed)
			return
		}
		fn()
		writeJSON(w, map[string]bool{"ok": true})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func trimSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}
