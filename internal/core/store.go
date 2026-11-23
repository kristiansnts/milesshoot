package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record: catatan satu foto/video. Kunci = MD5 isi file, jadi file yang sama tak pernah naik dua kali.
// Port dari store.js (riwayat.json).
type Record struct {
	MD5         string `json:"md5"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Kind        string `json:"kind"` // photo|video
	MTime       int64  `json:"mtime"`
	Status      string `json:"status"` // pending|uploading|done|failed|duplicate
	Stage       string `json:"stage,omitempty"`
	Attempts    int    `json:"attempts"`
	LocalTries  int    `json:"localTries,omitempty"`
	Error       string `json:"error,omitempty"`
	Event       string `json:"event,omitempty"`
	Destination string `json:"destination,omitempty"`
	Account     string `json:"account,omitempty"`
	Tree        string `json:"tree,omitempty"`
	Rule        string `json:"rule,omitempty"`
	Dup         string `json:"dup,omitempty"` // own|other|recycle
	RemoteID    string `json:"remoteId,omitempty"`
	Price       int    `json:"price,omitempty"`
	BytesOut    int64  `json:"bytesOut,omitempty"`
	Thumb       bool   `json:"thumb,omitempty"`
	OriOK       bool   `json:"oriOk,omitempty"`
	Requeue     bool   `json:"requeue,omitempty"`
	AddedAt     int64  `json:"addedAt"`
	DoneAt      int64  `json:"doneAt,omitempty"`
	NextAttempt int64  `json:"nextAttemptAt"`
}

type Store struct {
	mu        sync.RWMutex
	records   map[string]*Record // md5 -> record
	pathIndex map[string]string  // path -> md5
	file      string

	saveTimer  *time.Timer
	firstDirty time.Time
}

func LoadStore(dataDir string) *Store {
	s := &Store{
		records:   map[string]*Record{},
		pathIndex: map[string]string{},
		file:      filepath.Join(dataDir, "riwayat.json"),
	}
	_ = os.MkdirAll(dataDir, 0o755)
	if b, err := os.ReadFile(s.file); err == nil {
		var arr []*Record
		if json.Unmarshal(b, &arr) == nil {
			for _, r := range arr {
				if r.Status == "uploading" { // mati saat proses -> antre lagi
					r.Status = "pending"
				}
				s.records[r.MD5] = r
				s.pathIndex[r.Path] = r.MD5
			}
		}
	}
	return s
}

func (s *Store) Has(md5 string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.records[md5]
	return ok
}

func (s *Store) Get(md5 string) *Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.records[md5]; ok {
		cp := *r
		return &cp
	}
	return nil
}

func (s *Store) KnownPath(p string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.pathIndex[p]
	return ok
}

func (s *Store) Add(r *Record) {
	s.mu.Lock()
	s.records[r.MD5] = r
	s.pathIndex[r.Path] = r.MD5
	s.mu.Unlock()
	s.saveSoon()
}

// Update menerapkan perubahan sebagian. Bila path berubah (file dipindah ke "Sudah upload"),
// nama lama dilepas dari indeks supaya file BARU bernama sama tetap terdeteksi.
func (s *Store) Update(md5 string, patch func(*Record)) *Record {
	s.mu.Lock()
	r, ok := s.records[md5]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	oldPath := r.Path
	patch(r)
	if r.Path != oldPath {
		if s.pathIndex[oldPath] == md5 {
			delete(s.pathIndex, oldPath)
		}
		s.pathIndex[r.Path] = md5
	}
	cp := *r
	s.mu.Unlock()
	s.saveSoon()
	return &cp
}

func (s *Store) Remove(md5 string) bool {
	s.mu.Lock()
	r, ok := s.records[md5]
	if !ok {
		s.mu.Unlock()
		return false
	}
	if s.pathIndex[r.Path] == md5 {
		delete(s.pathIndex, r.Path)
	}
	delete(s.records, md5)
	s.mu.Unlock()
	s.saveSoon()
	return true
}

func (s *Store) All() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, *r)
	}
	return out
}

// ForEach menjalankan fn untuk tiap record tanpa menyalin seluruh map (antrean besar).
func (s *Store) ForEach(fn func(*Record)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.records {
		fn(r)
	}
}

type Counts struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Uploading int `json:"uploading"`
	Done      int `json:"done"`
	Failed    int `json:"failed"`
	Duplicate int `json:"duplicate"`
}

func (s *Store) Counts() Counts {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := Counts{}
	for _, r := range s.records {
		c.Total++
		switch r.Status {
		case "pending":
			c.Pending++
		case "uploading":
			c.Uploading++
		case "done":
			c.Done++
		case "failed":
			c.Failed++
		case "duplicate":
			c.Duplicate++
		}
	}
	return c
}

// Simpan tertunda (dikumpulkan), tapi paling lambat tiap 3 detik saat terus berubah,
// supaya riwayat tidak hilang bila app mati mendadak. Port dari saveSoon() di store.js.
func (s *Store) saveSoon() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.firstDirty.IsZero() {
		s.firstDirty = time.Now()
	}
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	delay := 500 * time.Millisecond
	if time.Since(s.firstDirty) > 3*time.Second {
		delay = 0
	}
	s.saveTimer = time.AfterFunc(delay, s.saveNow)
}

func (s *Store) saveNow() {
	s.mu.Lock()
	s.firstDirty = time.Time{}
	arr := make([]*Record, 0, len(s.records))
	for _, r := range s.records {
		arr = append(arr, r)
	}
	file := s.file
	s.mu.Unlock()

	b, err := json.Marshal(arr)
	if err != nil {
		return
	}
	tmp := file + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, file)
	}
}

// Flush memaksa simpan sekarang (dipanggil saat app ditutup).
func (s *Store) Flush() { s.saveNow() }
