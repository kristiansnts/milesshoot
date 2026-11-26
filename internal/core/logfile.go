package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileLogger menyimpan semua catatan ke berkas harian di <dataDir>/log/,
// sekaligus menyimpan yang terbaru di memori supaya layar Log cepat dibuka.
// Dipasang sebagai Emitter tambahan, jadi apa pun yang muncul di UI ikut tercatat.
type LogLine struct {
	T     int64  `json:"t"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

const logRing = 2000

type FileLogger struct {
	mu   sync.Mutex
	dir  string
	day  string
	f    *os.File
	ring []LogLine
}

func NewFileLogger(dataDir string) *FileLogger {
	l := &FileLogger{dir: filepath.Join(dataDir, "log")}
	_ = os.MkdirAll(l.dir, 0o755)
	return l
}

func (l *FileLogger) Emit(string, any) {} // hanya Log yang dicatat

func (l *FileLogger) Log(level, msg string) {
	now := time.Now()
	line := LogLine{T: now.UnixMilli(), Level: level, Msg: msg}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.ring = append(l.ring, line)
	if len(l.ring) > logRing {
		l.ring = l.ring[len(l.ring)-logRing:]
	}

	day := now.Format("2006-01-02")
	if l.f == nil || l.day != day {
		if l.f != nil {
			_ = l.f.Close()
		}
		f, err := os.OpenFile(filepath.Join(l.dir, "milesshoot-"+day+".log"),
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		l.f, l.day = f, day
		l.cleanOld()
	}
	fmt.Fprintf(l.f, "%s  %-5s  %s\n", now.Format("15:04:05"), strings.ToUpper(level), msg)
}

// Tail mengembalikan n catatan terbaru (terbaru di atas).
func (l *FileLogger) Tail(n int) []LogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.ring) {
		n = len(l.ring)
	}
	out := make([]LogLine, 0, n)
	for i := len(l.ring) - 1; i >= len(l.ring)-n; i-- {
		out = append(out, l.ring[i])
	}
	return out
}

func (l *FileLogger) Dir() string { return l.dir }

// File hari ini — untuk ditunjukkan di UI.
func (l *FileLogger) Today() string {
	return filepath.Join(l.dir, "milesshoot-"+time.Now().Format("2006-01-02")+".log")
}

// Simpan 14 hari terakhir saja.
func (l *FileLogger) cleanOld() {
	ents, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -14)
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), "milesshoot-") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(l.dir, e.Name()))
		}
	}
}
