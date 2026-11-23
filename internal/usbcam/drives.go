package usbcam

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kartu memori / card reader / kamera mode "flashdisk".
//
// Berbeda dengan tether (PTP): di sini kamera atau card reader muncul sebagai DRIVE
// yang punya folder DCIM. Jadi tidak perlu program pembantu — cukup dipantau
// kemunculannya, lalu foldernya dijadikan sumber foto.
//
// Disengaja TIDAK otomatis mengunggah: kartu bisa berisi ribuan foto lama, jadi
// pemakaiannya harus dipilih sendiri oleh pengguna (lihat app.UseCardDrive).

type Drive struct {
	Path  string `json:"path"`  // folder DCIM
	Root  string `json:"root"`  // akar drive (mis. /Volumes/NIKON Z6)
	Label string `json:"label"` // nama drive
	Files int    `json:"files"` // perkiraan jumlah foto di dalam DCIM
}

type DriveWatcher struct {
	mu       sync.Mutex
	drives   []Drive
	stop     chan struct{}
	running  bool
	log      func(level, msg string)
	onChange func()
	known    map[string]bool // drive yang sudah pernah dilaporkan
}

func NewDriveWatcher(log func(level, msg string), onChange func()) *DriveWatcher {
	if log == nil {
		log = func(string, string) {}
	}
	if onChange == nil {
		onChange = func() {}
	}
	return &DriveWatcher{log: log, onChange: onChange, known: map[string]bool{}}
}

func (w *DriveWatcher) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.stop = make(chan struct{})
	stop := w.stop
	w.mu.Unlock()

	go func() {
		w.scan()
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				w.scan()
			}
		}
	}()
}

func (w *DriveWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		w.running = false
		close(w.stop)
	}
}

func (w *DriveWatcher) Drives() []Drive {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Drive{}, w.drives...)
}

func (w *DriveWatcher) scan() {
	found := detectDrives()

	w.mu.Lock()
	old := w.known
	now := map[string]bool{}
	var added, removed []string
	for _, d := range found {
		now[d.Path] = true
		if !old[d.Path] {
			added = append(added, d.Label)
		}
	}
	for p := range old {
		if !now[p] {
			removed = append(removed, filepath.Base(filepath.Dir(p)))
		}
	}
	w.drives = found
	w.known = now
	w.mu.Unlock()

	for _, l := range added {
		w.log("ok", "Kartu/kamera terdeteksi: "+l+" — pilih di Pengaturan → Sumber untuk memakainya")
	}
	for _, l := range removed {
		w.log("info", "Kartu/kamera dilepas: "+l)
	}
	if len(added) > 0 || len(removed) > 0 {
		w.onChange()
	}
}

// detectDrives mencari drive yang punya folder DCIM.
func detectDrives() []Drive {
	var roots []string
	if runtime.GOOS == "windows" {
		for c := 'D'; c <= 'Z'; c++ { // C: biasanya disk sistem
			roots = append(roots, string(c)+":\\")
		}
	} else {
		ents, _ := os.ReadDir("/Volumes")
		for _, e := range ents {
			roots = append(roots, filepath.Join("/Volumes", e.Name()))
		}
	}

	var out []Drive
	for _, root := range roots {
		dcim := findDCIM(root)
		if dcim == "" {
			continue
		}
		out = append(out, Drive{
			Path: dcim, Root: root, Label: filepath.Base(root), Files: countPhotos(dcim),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// findDCIM: folder DCIM di akar drive (nama bisa beda besar-kecil hurufnya).
func findDCIM(root string) string {
	ents, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if e.IsDir() && strings.EqualFold(e.Name(), "DCIM") {
			return filepath.Join(root, e.Name())
		}
	}
	return ""
}

// countPhotos menghitung kasar isi DCIM (dibatasi supaya kartu besar tidak bikin lambat).
func countPhotos(dcim string) int {
	const max = 20000
	n := 0
	subs, _ := os.ReadDir(dcim)
	for _, s := range subs {
		if n >= max {
			break
		}
		if !s.IsDir() {
			if isPhotoName(s.Name()) {
				n++
			}
			continue
		}
		files, _ := os.ReadDir(filepath.Join(dcim, s.Name()))
		for _, f := range files {
			if !f.IsDir() && isPhotoName(f.Name()) {
				n++
				if n >= max {
					break
				}
			}
		}
	}
	return n
}

func isPhotoName(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".heic", ".webp", ".tif", ".tiff", ".mp4", ".mov":
		return true
	}
	return false
}
