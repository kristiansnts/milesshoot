package core

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"milesshoot/internal/fotoyu"
)

// Hi-res: foto/video yang TERJUAL di FotoYu menunggu dikirimi file aslinya.
// Selama belum dikirim, transaksinya menggantung — jadi ini jalur yang langsung
// berhubungan dengan uang masuk.
//
// Pencocokan (port dari src/hires.js, disederhanakan):
//  1. "cocok pasti" — FotoYu memberi `signature` = MD5 file ASLI, dan store kita
//     memang berkunci MD5 file asli → sekali lihat langsung ketemu path-nya.
//  2. cadangan: cari file bernama sama di folder sumber (termasuk "Sudah upload")
//     lalu verifikasi MD5-nya.
//  3. "cocok nama" — nama sama tapi isi beda (foto sudah diedit) → perlu dicek user.

// VideoProbeFn disuntik di main (ffmpeg) karena core tidak boleh mengimpor package video.
var VideoProbeFn = func(path string) (durationSec float64, w, h int, err error) {
	return 0, 0, 0, fmt.Errorf("pemeriksa video belum tersedia")
}

type HiresMatch struct {
	Type string `json:"type"` // md5 | name | none
	Path string `json:"path"`
}

type HiresItem struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Kind      string     `json:"kind"` // photo | video
	Signature string     `json:"signature"`
	Match     HiresMatch `json:"match"`
	Sent      bool       `json:"sent"`
	Error     string     `json:"error"`
}

type HiresState struct {
	Items    []HiresItem `json:"items"`
	Exact    int         `json:"exact"`
	ByName   int         `json:"byName"`
	None     int         `json:"none"`
	Sent     int         `json:"sent"`
	Loading  bool        `json:"loading"`
	Error    string      `json:"error"`
	LoadedAt int64       `json:"loadedAt"`
}

type HiresManager struct {
	mu    sync.Mutex
	cfg   *Config
	store *Store
	fy    *fotoyu.Client
	emit  Emitter

	items    []HiresItem
	loading  bool
	lastErr  string
	loadedAt int64

	md5cache map[string]string          // path|size|mtime -> md5
	verified map[string]string          // signature -> path yang sudah terbukti
	mismatch map[string]map[string]bool // signature -> path yang sudah terbukti BEDA

	stop chan struct{}
}

func NewHiresManager(cfg *Config, store *Store, fy *fotoyu.Client, emit Emitter) *HiresManager {
	return &HiresManager{
		cfg: cfg, store: store, fy: fy, emit: emit,
		md5cache: map[string]string{}, verified: map[string]string{},
		mismatch: map[string]map[string]bool{},
		stop:     make(chan struct{}),
	}
}

// 5 menit (bukan 3 menit seperti aslinya) — supaya tidak terlalu sering membebani
// server FotoYu dengan polling. Bentuknya var (bukan const) supaya test bisa
// mempersingkatnya tanpa menunggu 5 menit sungguhan.
var hiresAutoInterval = 5 * time.Minute

// StartAutoLoop menjalankan port dari hiresTick() di server.js: tiap hiresAutoInterval,
// ambil daftar pesanan terbaru dari FotoYu, lalu — bila "kirim otomatis" nyala
// (cfg.Hires.Auto) — kirim semua yang "cocok pasti" (aman, sidik file identik).
// "Cocok nama" (isi beda) TIDAK PERNAH dikirim otomatis, harus dicek user dulu.
func (h *HiresManager) StartAutoLoop() {
	go func() {
		t := time.NewTicker(hiresAutoInterval)
		defer t.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-t.C:
				h.tick()
			}
		}
	}()
}

func (h *HiresManager) StopAutoLoop() {
	select {
	case <-h.stop: // sudah ditutup
	default:
		close(h.stop)
	}
}

func (h *HiresManager) tick() {
	before := h.State()
	h.Refresh()
	after := h.State()
	if after.Exact+after.ByName+after.None > before.Exact+before.ByName+before.None {
		h.emit.Log("info", fmt.Sprintf("Hi-res: %d pesanan baru masuk", (after.Exact+after.ByName+after.None)-(before.Exact+before.ByName+before.None)))
	}
	if h.cfg.Get().Hires.Auto {
		h.SendAllExact()
	}
}

func (h *HiresManager) State() HiresState {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := HiresState{Items: append([]HiresItem{}, h.items...), Loading: h.loading, Error: h.lastErr, LoadedAt: h.loadedAt}
	for _, it := range st.Items {
		switch {
		case it.Sent:
			st.Sent++
		case it.Match.Type == "md5":
			st.Exact++
		case it.Match.Type == "name":
			st.ByName++
		default:
			st.None++
		}
	}
	return st
}

// Refresh mengambil daftar "kirim ulang" dari FotoYu lalu mencocokkannya ke file lokal.
func (h *HiresManager) Refresh() HiresState {
	h.mu.Lock()
	if h.loading {
		h.mu.Unlock()
		return h.State()
	}
	h.loading = true
	h.lastErr = ""
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		h.loading = false
		h.loadedAt = time.Now().UnixMilli()
		h.mu.Unlock()
		h.emit.Emit("hires", h.State())
	}()

	list, err := h.fy.ListResend()
	if err != nil {
		h.mu.Lock()
		h.lastErr = err.Error()
		h.mu.Unlock()
		return h.State()
	}

	idx := h.nameIndex()
	items := make([]HiresItem, 0, len(list))
	for _, r := range list {
		kind := "photo"
		if r.ContentType == "video" {
			kind = "video"
		}
		it := HiresItem{ID: r.ContentID, Title: r.Title, Kind: kind, Signature: r.Signature}
		it.Match = h.matchOne(r.Signature, r.Title, idx)
		items = append(items, it)
	}

	h.mu.Lock()
	h.items = items
	h.mu.Unlock()
	h.emit.Log("info", fmt.Sprintf("Hi-res: %d pesanan menunggu file asli", len(items)))
	return h.State()
}

// nameIndex: nama file (huruf kecil) -> daftar path. Folder "Sudah upload" IKUT dipindai,
// karena file yang sudah diunggah justru dipindah ke sana.
func (h *HiresManager) nameIndex() map[string][]string {
	cfg := h.cfg.Get()
	idx := map[string][]string{}
	dirs := append([]string{}, AllWatchFolders(cfg)...)
	dirs = append(dirs, cfg.Hires.Folders...)

	seen := map[string]bool{}
	for _, d := range dirs {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		for _, p := range ListFiles(d, true, nil, nil, nil) { // nil skip = ikut "Sudah upload"
			k := strings.ToLower(filepath.Base(p))
			if len(idx[k]) < 60 { // batasi, nama file kamera sering berulang
				idx[k] = append(idx[k], p)
			}
		}
	}
	return idx
}

func (h *HiresManager) matchOne(sig, title string, idx map[string][]string) HiresMatch {
	// 1. riwayat kita sendiri: kunci store = MD5 file asli = signature FotoYu
	if rec := h.store.Get(sig); rec != nil && rec.Path != "" && fileExists(rec.Path) {
		return HiresMatch{Type: "md5", Path: rec.Path}
	}
	h.mu.Lock()
	v, okV := h.verified[sig]
	h.mu.Unlock()
	if okV && fileExists(v) {
		return HiresMatch{Type: "md5", Path: v}
	}

	// 2. cari yang namanya sama, lalu buktikan dengan MD5
	cands := idx[strings.ToLower(title)]
	var remaining []string
	for _, p := range cands {
		if !fileExists(p) || h.isMismatch(sig, p) {
			continue
		}
		if sum, err := h.md5Of(p); err == nil && sum == sig {
			h.mu.Lock()
			h.verified[sig] = p
			h.mu.Unlock()
			return HiresMatch{Type: "md5", Path: p}
		}
		h.markMismatch(sig, p)
		remaining = append(remaining, p)
	}
	// 3. nama sama tapi isi beda (mis. foto sudah diedit) → perlu dicek user
	if len(remaining) > 0 {
		return HiresMatch{Type: "name", Path: remaining[0]}
	}
	return HiresMatch{Type: "none"}
}

func (h *HiresManager) isMismatch(sig, p string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.mismatch[sig][p]
}

func (h *HiresManager) markMismatch(sig, p string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.mismatch[sig] == nil {
		h.mismatch[sig] = map[string]bool{}
	}
	h.mismatch[sig][p] = true
}

func (h *HiresManager) md5Of(p string) (string, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	key := p + "|" + strconv.FormatInt(fi.Size(), 10) + "|" + strconv.FormatInt(fi.ModTime().UnixNano(), 10)
	h.mu.Lock()
	if s, ok := h.md5cache[key]; ok {
		h.mu.Unlock()
		return s, nil
	}
	h.mu.Unlock()

	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	d := md5.New()
	if _, err := io.Copy(d, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(d.Sum(nil))
	h.mu.Lock()
	if len(h.md5cache) > 5000 {
		h.md5cache = map[string]string{}
	}
	h.md5cache[key] = sum
	h.mu.Unlock()
	return sum, nil
}

// Send mengirim satu file asli ke FotoYu.
func (h *HiresManager) Send(id string) HiresState {
	h.mu.Lock()
	var target *HiresItem
	for i := range h.items {
		if h.items[i].ID == id {
			target = &h.items[i]
			break
		}
	}
	if target == nil || target.Sent || target.Match.Path == "" {
		h.mu.Unlock()
		return h.State()
	}
	item := *target
	h.mu.Unlock()

	note, err := h.sendOne(item)

	h.mu.Lock()
	for i := range h.items {
		if h.items[i].ID != id {
			continue
		}
		if err != nil {
			h.items[i].Error = err.Error()
		} else {
			h.items[i].Sent = true
			h.items[i].Error = ""
		}
	}
	h.mu.Unlock()

	if err != nil {
		h.emit.Log("error", "Hi-res gagal: "+item.Title+" — "+err.Error())
	} else {
		msg := "Hi-res terkirim: " + item.Title
		if note != "" {
			msg += " · " + note
		}
		h.emit.Log("ok", msg)
	}
	st := h.State()
	h.emit.Emit("hires", st)
	return st
}

// SendAllExact mengirim semua yang "cocok pasti" (aman, tanpa perlu dicek manual).
func (h *HiresManager) SendAllExact() HiresState {
	h.mu.Lock()
	ids := []string{}
	for _, it := range h.items {
		if !it.Sent && it.Match.Type == "md5" {
			ids = append(ids, it.ID)
		}
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.Send(id)
	}
	return h.State()
}

// sendOne mengirim satu file asli. note = keterangan pengolahan (LUT/diperbesar) untuk log.
func (h *HiresManager) sendOne(it HiresItem) (note string, err error) {
	ct := "photo"
	var f fotoyu.ResendFile
	if it.Kind == "video" {
		ct = "video"
		f, err = resendFileInfo(it.Match.Path, it.Kind)
	} else {
		var cleanup func()
		f, cleanup, note, err = prepareHiresPhoto(it.Match.Path, h.cfg.Get().Hires)
		defer cleanup()
	}
	if err != nil {
		return "", err
	}
	_, err = h.fy.SendResend(fotoyu.ResendItem{
		ContentID: it.ID, ContentType: ct, Signature: it.Signature, Title: it.Title,
	}, f)
	return note, err
}

// resendFileInfo mengumpulkan ukuran/dimensi/mime yang diminta FotoYu.
func resendFileInfo(path, kind string) (fotoyu.ResendFile, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fotoyu.ResendFile{}, fmt.Errorf("file asli tidak ditemukan")
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	out := fotoyu.ResendFile{Path: path, Size: fi.Size(), Extension: ext, Mime: mimeFor(ext)}

	if kind == "video" {
		dur, w, hh, err := VideoProbeFn(path)
		if err != nil {
			return out, fmt.Errorf("video tidak bisa dibaca: %v", err)
		}
		out.DurationSec, out.Width, out.Height = dur, w, hh
		return out, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return out, fmt.Errorf("ukuran gambar tidak terbaca")
	}
	out.Width, out.Height = cfg.Width, cfg.Height
	return out, nil
}

func mimeFor(ext string) string {
	switch ext {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "heic":
		return "image/heic"
	case "webp":
		return "image/webp"
	case "tif", "tiff":
		return "image/tiff"
	case "mp4":
		return "video/mp4"
	case "mov":
		return "video/quicktime"
	}
	return "application/octet-stream"
}

// AddFolder/RemoveFolder: folder tambahan tempat file asli disimpan.
func (h *HiresManager) AddFolder(p string) Settings {
	return h.cfg.Set(func(s *Settings) {
		for _, x := range s.Hires.Folders {
			if x == p {
				return
			}
		}
		if p != "" && len(s.Hires.Folders) < 20 {
			s.Hires.Folders = append(s.Hires.Folders, p)
		}
	})
}

func (h *HiresManager) RemoveFolder(p string) Settings {
	return h.cfg.Set(func(s *Settings) {
		out := s.Hires.Folders[:0]
		for _, x := range s.Hires.Folders {
			if x != p {
				out = append(out, x)
			}
		}
		s.Hires.Folders = out
	})
}
