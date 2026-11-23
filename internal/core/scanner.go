package core

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Pantau folder dengan polling berkala (lebih andal lintas Mac/Windows/kartu SD daripada fs watch).
// File baru dianggap "siap" bila ukurannya tidak berubah di dua pemindaian berturut
// (kamera/tether selesai menulis). Port dari scanner.js.

var photoExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".tif": true, ".tiff": true, ".heic": true, ".mp4": true, ".mov": true,
}

var rawExt = map[string]bool{
	".nef": true, ".nrw": true, ".cr2": true, ".cr3": true, ".crw": true, ".arw": true,
	".srf": true, ".sr2": true, ".raf": true, ".dng": true, ".orf": true, ".rw2": true,
	".pef": true, ".x3f": true, ".3fr": true, ".iiq": true,
}

func isMedia(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~") {
		return false
	}
	return photoExt[strings.ToLower(filepath.Ext(name))]
}

type ScanTally struct {
	Raw, Other, Subdirs int
}

// ListFiles mengumpulkan path media, melewati folder yang di-skip (mis. "Sudah upload").
func ListFiles(dir string, recursive bool, out []string, skip func(string) bool, tally *ScanTally) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "_") {
			continue
		}
		full := filepath.Join(dir, name)
		if e.IsDir() {
			if recursive && !(skip != nil && skip(full)) {
				out = ListFiles(full, recursive, out, skip, tally)
			} else if tally != nil && !recursive && !(skip != nil && skip(full)) {
				tally.Subdirs++
			}
			continue
		}
		if isMedia(name) {
			out = append(out, full)
		} else if tally != nil && !strings.HasPrefix(name, ".") {
			if rawExt[strings.ToLower(filepath.Ext(name))] {
				tally.Raw++
			} else {
				tally.Other++
			}
		}
	}
	return out
}

type ScanSummary struct {
	Folder  string `json:"folder"`
	Media   int    `json:"media"`
	Known   int    `json:"known"`
	Waiting int    `json:"waiting"`
	Raw     int    `json:"raw"`
	Other   int    `json:"other"`
	Subdirs int    `json:"subdirs"`
}

// FolderScan: ringkasan pindai untuk satu folder sumber — dipakai di panel multi-folder.
type FolderScan struct {
	Folder string `json:"folder"`
	OK     bool   `json:"ok"` // false = folder tidak ada / bukan direktori
	Media  int    `json:"media"`
}

type Scanner struct {
	cfg       *Config
	isKnown   func(string) bool
	onReady   func(path string, size int64)
	skipDir   func(string) bool
	intervalS func() int

	mu        sync.Mutex
	sumMu     sync.RWMutex // melindungi FolderOK/Summary/PerFolder (ditulis goroutine scan, dibaca Engine.Status)
	sizes     map[string]int64
	stop      chan struct{}
	running   bool
	FolderOK  bool
	Summary   ScanSummary
	PerFolder []FolderScan // ringkasan tiap folder sumber — berisi >1 entri saat multi-folder aktif
}

type ScannerOpts struct {
	Config    *Config
	IsKnown   func(string) bool
	OnReady   func(path string, size int64)
	SkipDir   func(string) bool
	IntervalS func() int
}

func NewScanner(o ScannerOpts) *Scanner {
	return &Scanner{
		cfg: o.Config, isKnown: o.IsKnown, onReady: o.OnReady,
		skipDir: o.SkipDir, intervalS: o.IntervalS, sizes: map[string]int64{},
	}
}

func (s *Scanner) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stop = make(chan struct{})
	s.mu.Unlock()
	go s.loop()
}

func (s *Scanner) Stop() {
	s.mu.Lock()
	if s.running {
		s.running = false
		close(s.stop)
	}
	s.mu.Unlock()
}

func (s *Scanner) loop() {
	for {
		s.Scan()
		sec := 2
		if s.intervalS != nil {
			if v := s.intervalS(); v > 0 {
				sec = v
			}
		}
		if sec < 1 {
			sec = 1
		}
		select {
		case <-time.After(time.Duration(sec) * time.Second):
		case <-s.stop:
			return
		}
	}
}

func (s *Scanner) Scan() {
	cfg := s.cfg.Get()
	roots := AllWatchFolders(cfg)
	if len(roots) == 0 {
		s.sumMu.Lock()
		s.FolderOK = false
		s.Summary = ScanSummary{}
		s.PerFolder = nil
		s.sumMu.Unlock()
		return
	}

	anyOK := false
	tally := &ScanTally{}
	var allFiles []string
	perFolder := make([]FolderScan, 0, len(roots))
	for _, root := range roots {
		fi, err := os.Stat(root)
		if err != nil || !fi.IsDir() {
			perFolder = append(perFolder, FolderScan{Folder: root, OK: false})
			continue
		}
		anyOK = true
		ft := &ScanTally{}
		files := ListFiles(root, cfg.IncludeSubfolders, nil, s.skipDir, ft)
		allFiles = append(allFiles, files...)
		tally.Raw += ft.Raw
		tally.Other += ft.Other
		tally.Subdirs += ft.Subdirs
		perFolder = append(perFolder, FolderScan{Folder: root, OK: true, Media: len(files)})
	}
	s.sumMu.Lock()
	s.FolderOK = anyOK
	s.PerFolder = perFolder
	s.sumMu.Unlock()

	s.mu.Lock()
	for _, f := range allFiles {
		if s.isKnown(f) {
			continue
		}
		fi, err := os.Stat(f)
		if err != nil || fi.Size() == 0 {
			continue
		}
		size := fi.Size()
		if prev, ok := s.sizes[f]; ok && prev == size {
			delete(s.sizes, f)
			go s.onReady(f, size) // ukuran stabil: siap diproses
		} else {
			s.sizes[f] = size
		}
	}
	waiting := len(s.sizes)
	s.mu.Unlock()

	known := 0
	for _, f := range allFiles {
		if s.isKnown(f) {
			known++
		}
	}
	s.sumMu.Lock()
	s.Summary = ScanSummary{
		Folder: cfg.WatchFolder, Media: len(allFiles), Known: known, Waiting: waiting,
		Raw: tally.Raw, Other: tally.Other, Subdirs: tally.Subdirs,
	}
	s.sumMu.Unlock()
}

// SummarySnapshot mengembalikan salinan ringkasan terakhir, aman dipanggil dari goroutine lain.
func (s *Scanner) SummarySnapshot() ScanSummary {
	s.sumMu.RLock()
	defer s.sumMu.RUnlock()
	return s.Summary
}
