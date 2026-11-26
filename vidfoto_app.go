package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"

	"milesshoot/internal/video"

	wrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Video → Foto: satu pekerjaan sekali jalan. Kemajuan di-poll frontend lewat VideoPhotoState.

type vfOpts struct {
	video.VFOptions
	SkipDone bool `json:"skipDone"` // lewati video yang sudah pernah diproses (mode folder)
}

type VFItem struct {
	video.VFResult
	Error string `json:"error,omitempty"`
}

type VFState struct {
	Running    bool     `json:"running"`
	Cancelling bool     `json:"cancelling"`
	Total      int      `json:"total"`
	Index      int      `json:"index"` // video yang sedang diproses (1-based)
	Current    string   `json:"current"`
	Stage      string   `json:"stage"`
	Items      []VFItem `json:"items"`
	Photos     int      `json:"photos"`
	Queued     int      `json:"queued"`    // foto yang sudah masuk antrean unggah
	Paused     bool     `json:"paused"`    // antrean unggah sedang dijeda
	Available  bool     `json:"available"` // ffmpeg ada
}

type vfJob struct {
	mu     sync.Mutex
	st     VFState
	cancel bool
}

func (a *App) vfSnapshot() VFState {
	a.vf.mu.Lock()
	defer a.vf.mu.Unlock()
	s := a.vf.st
	s.Items = append([]VFItem(nil), s.Items...)
	s.Available = video.Available()
	s.Paused, _, _ = a.eng.Queue.Paused()
	return s
}

// VideoPhotoState: keadaan pekerjaan terakhir (untuk di-poll).
func (a *App) VideoPhotoState() VFState { return a.vfSnapshot() }

// VideoPhotoPickFiles membuka dialog pilih video (boleh banyak).
func (a *App) VideoPhotoPickFiles() ([]string, error) {
	return wrt.OpenMultipleFilesDialog(a.ctx, wrt.OpenDialogOptions{
		Title:   "Pilih video",
		Filters: []wrt.FileFilter{{DisplayName: "Video (*.mp4;*.mov;*.m4v)", Pattern: "*.mp4;*.mov;*.m4v;*.MP4;*.MOV;*.M4V"}},
	})
}

// VideoPhotoPickFolder membuka dialog pilih folder dan mengembalikan semua video di dalamnya.
func (a *App) VideoPhotoPickFolder() ([]string, error) {
	dir, err := wrt.OpenDirectoryDialog(a.ctx, wrt.OpenDialogOptions{Title: "Pilih folder berisi video"})
	if err != nil || dir == "" {
		return nil, err
	}
	return video.FindVideos(dir), nil
}

// VideoPhotoStart memproses video berurutan di latar belakang.
func (a *App) VideoPhotoStart(files []string, optsJSON string) (VFState, error) {
	var o vfOpts
	if err := json.Unmarshal([]byte(optsJSON), &o); err != nil {
		return a.vfSnapshot(), errors.New("pilihan tidak valid")
	}
	if !video.Available() {
		return a.vfSnapshot(), errors.New("ffmpeg tidak ditemukan di komputer ini")
	}
	var list []string
	for _, f := range files {
		if video.IsVideoFile(f) {
			list = append(list, f)
		}
	}
	if len(list) == 0 {
		return a.vfSnapshot(), errors.New("tidak ada video yang dipilih")
	}

	a.vf.mu.Lock()
	if a.vf.st.Running {
		a.vf.mu.Unlock()
		return a.vfSnapshot(), errors.New("masih ada video yang diproses")
	}
	a.vf.cancel = false
	a.vf.st = VFState{Running: true, Total: len(list)}
	a.vf.mu.Unlock()

	go a.vfRun(list, o)
	return a.vfSnapshot(), nil
}

// VideoPhotoCancel menghentikan pekerjaan setelah langkah yang sedang berjalan.
func (a *App) VideoPhotoCancel() {
	a.vf.mu.Lock()
	if a.vf.st.Running {
		a.vf.cancel = true
		a.vf.st.Cancelling = true
	}
	a.vf.mu.Unlock()
}

func (a *App) vfRun(list []string, o vfOpts) {
	// Galat tak terduga (panic) jangan mematikan aplikasi & jangan meninggalkan layar "memproses" selamanya.
	defer func() {
		if r := recover(); r != nil {
			a.eng.Log("error", fmt.Sprintf("[VideoFoto] PANIC: %v\n%s", r, debug.Stack()))
			a.vf.mu.Lock()
			a.vf.st.Running, a.vf.st.Cancelling, a.vf.st.Stage, a.vf.st.Current = false, false, "", ""
			a.vf.st.Items = append(a.vf.st.Items, VFItem{Error: fmt.Sprintf("galat tak terduga: %v (lihat log)", r)})
			a.vf.mu.Unlock()
		}
	}()
	a.eng.Log("info", fmt.Sprintf("[VideoFoto] pekerjaan dimulai: %d video", len(list)))
	cancelled := func() bool {
		a.vf.mu.Lock()
		defer a.vf.mu.Unlock()
		return a.vf.cancel
	}
	for i, f := range list {
		if cancelled() {
			break
		}
		a.vf.mu.Lock()
		a.vf.st.Index, a.vf.st.Current, a.vf.st.Stage = i+1, filepath.Base(f), "Menyiapkan…"
		a.vf.mu.Unlock()

		res, err := video.ExtractPhotos(f, o.VFOptions, o.SkipDone, func(s string) {
			a.vf.mu.Lock()
			a.vf.st.Stage = s
			a.vf.mu.Unlock()
		}, cancelled)

		item := VFItem{VFResult: res}
		if err != nil {
			if cancelled() {
				break
			}
			item.Error = err.Error()
			a.eng.Log("warn", fmt.Sprintf("Video → Foto gagal: %s — %s", filepath.Base(f), err))
		} else if !res.Skipped {
			a.eng.Log("info", fmt.Sprintf("Video → Foto: %s → %d foto di %s", filepath.Base(f), len(res.Files), res.Dir))
		}
		queued := 0
		if err == nil {
			a.vf.mu.Lock()
			a.vf.st.Stage = "Memasukkan ke antrean unggah…"
			a.vf.mu.Unlock()
			for _, n := range res.Files {
				if ok, qerr := a.eng.Queue.AddDirect(filepath.Join(res.Dir, n)); qerr != nil {
					a.eng.Log("warn", fmt.Sprintf("Video → Foto: %s tidak bisa masuk antrean — %s", n, qerr))
				} else if ok {
					queued++
				}
			}
		}
		a.vf.mu.Lock()
		a.vf.st.Items = append(a.vf.st.Items, item)
		a.vf.st.Photos += len(res.Files)
		a.vf.st.Queued += queued
		a.vf.mu.Unlock()
	}
	a.vf.mu.Lock()
	a.vf.st.Running, a.vf.st.Cancelling, a.vf.st.Stage, a.vf.st.Current = false, false, "", ""
	a.vf.mu.Unlock()
}

// VideoPhotoReveal membuka folder hasil di Finder/Explorer.
func (a *App) VideoPhotoReveal(dir string) {
	if dir == "" {
		return
	}
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", dir).Start()
	case "windows":
		_ = exec.Command("explorer", dir).Start()
	default:
		_ = exec.Command("xdg-open", dir).Start()
	}
}
