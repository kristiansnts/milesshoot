package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"milesshoot/internal/update"

	wrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// updateRepo: repo GitHub tempat rilis dipublikasikan ("pemilik/nama"). Diisi saat build oleh
// scripts/release.sh:  -ldflags "-X main.updateRepo=pemilik/nama".  Kosong = pembaruan nonaktif.
var updateRepo = ""

const (
	updateFirstDelay = 25 * time.Second
	updateEvery      = 6 * time.Hour
)

// UpdateInfo: keadaan pembaruan untuk UI (di-poll).
type UpdateInfo struct {
	Current     string `json:"current"`
	Configured  bool   `json:"configured"`
	Auto        bool   `json:"auto"`
	Checking    bool   `json:"checking"`
	Downloading bool   `json:"downloading"`
	Pct         int    `json:"pct"`
	Available   bool   `json:"available"` // ada versi lebih baru
	Ready       bool   `json:"ready"`     // sudah diunduh & tanda tangannya cocok
	Installing  bool   `json:"installing"`
	Latest      string `json:"latest"`
	Notes       string `json:"notes"`
	Published   int64  `json:"published"`
	LastCheck   int64  `json:"lastCheck"`
	Error       string `json:"error"`
	CanInstall  bool   `json:"canInstall"`
	InstallNote string `json:"installNote"`
	Uploading   int    `json:"uploading"` // foto yang sedang diunggah (info untuk konfirmasi)
}

type updJob struct {
	mu     sync.Mutex
	st     UpdateInfo
	staged string // payload terverifikasi siap dipasang
	busy   bool
}

func (a *App) updDir() string      { return filepath.Join(a.eng.Cfg.DataDir(), "update") }
func (a *App) updApplyLog() string { return filepath.Join(a.eng.Cfg.DataDir(), "update-apply.log") }

// UpdateState: keadaan terkini (aman dipanggil sering).
func (a *App) UpdateState() UpdateInfo {
	a.upd.mu.Lock()
	s := a.upd.st
	a.upd.mu.Unlock()
	s.Current = appVersion
	s.Configured = updateRepo != ""
	s.Auto = a.eng.Cfg.Get().AutoUpdate
	s.Uploading = a.eng.Store.Counts().Uploading
	return s
}

// UpdateCheck: periksa sekarang (tombol "Cek pembaruan"). Berjalan di latar; hasilnya lewat UpdateState.
func (a *App) UpdateCheck() UpdateInfo {
	go a.updCheck(true)
	return a.UpdateState()
}

func (a *App) updSet(f func(*UpdateInfo)) {
	a.upd.mu.Lock()
	f(&a.upd.st)
	a.upd.mu.Unlock()
}

// startUpdates: catatan hasil pemasangan sebelumnya, bersih-bersih, lalu pemeriksaan berkala.
func (a *App) startUpdates(ctx context.Context) {
	if s := update.TakeApplyLog(a.updApplyLog()); s != "" {
		a.eng.Log("info", "[Update] hasil pemasangan sebelumnya: "+trimNL(s))
	}
	update.CleanupOld()
	_ = os.RemoveAll(a.updDir()) // sisa unduhan lama; selalu diunduh ulang & diverifikasi bila perlu

	if updateRepo == "" {
		a.eng.Log("info", "[Update] nonaktif: alamat repo pembaruan belum diatur di build ini")
		return
	}
	go func() {
		t := time.NewTimer(updateFirstDelay)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if a.eng.Cfg.Get().AutoUpdate {
				a.updCheck(false)
			}
			t.Reset(updateEvery)
		}
	}()
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func (a *App) updCheck(manual bool) {
	a.upd.mu.Lock()
	if a.upd.busy || a.upd.st.Installing {
		a.upd.mu.Unlock()
		return
	}
	a.upd.busy = true
	a.upd.st.Checking, a.upd.st.Error = true, ""
	a.upd.mu.Unlock()
	defer a.updSet(func(s *UpdateInfo) { s.Checking, s.Downloading = false, false })
	defer func() { a.upd.mu.Lock(); a.upd.busy = false; a.upd.mu.Unlock() }()

	fail := func(err error) {
		a.eng.Log("warn", "[Update] "+err.Error())
		a.updSet(func(s *UpdateInfo) { s.Error = err.Error(); s.LastCheck = time.Now().UnixMilli() })
	}

	cl, err := update.New(updateRepo)
	if err != nil {
		fail(err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	rel, err := cl.Check(ctx, appVersion)
	if err != nil {
		fail(err)
		return
	}
	now := time.Now().UnixMilli()
	if rel == nil {
		a.eng.Log("info", fmt.Sprintf("[Update] v%s sudah yang terbaru", appVersion))
		a.updSet(func(s *UpdateInfo) { s.Available, s.Ready, s.LastCheck = false, false, now })
		return
	}
	a.eng.Log("info", fmt.Sprintf("[Update] versi baru tersedia: v%s (terpasang v%s)", rel.Version, appVersion))
	a.updSet(func(s *UpdateInfo) {
		s.Available, s.Latest, s.Notes, s.Published, s.LastCheck = true, rel.Version, rel.Notes, rel.Published.UnixMilli(), now
		s.Ready, s.CanInstall, s.InstallNote = false, false, ""
	})

	if cerr := update.CanReplace(); cerr != nil {
		a.updSet(func(s *UpdateInfo) { s.InstallNote = cerr.Error() })
		a.eng.Log("warn", "[Update] tidak bisa dipasang otomatis: "+cerr.Error())
		return
	}

	// Unduh + verifikasi + ekstrak ke folder sementara.
	_ = os.RemoveAll(a.updDir())
	a.updSet(func(s *UpdateInfo) { s.Downloading, s.Pct = true, 0 })
	last := -1
	zipPath, err := cl.Download(ctx, rel, a.updDir(), func(done, total int64) {
		if total > 0 {
			if p := int(done * 100 / total); p != last {
				last = p
				a.updSet(func(s *UpdateInfo) { s.Pct = p })
			}
		}
	})
	if err != nil {
		fail(fmt.Errorf("unduhan pembaruan gagal: %w", err))
		return
	}
	newDir := filepath.Join(a.updDir(), "new")
	if err := update.Extract(zipPath, newDir); err != nil {
		fail(err)
		return
	}
	payload, err := update.FindPayload(newDir, runtime.GOOS)
	if err != nil {
		fail(err)
		return
	}
	a.upd.mu.Lock()
	a.upd.staged = payload
	a.upd.mu.Unlock()
	a.updSet(func(s *UpdateInfo) { s.Ready, s.CanInstall, s.Downloading, s.Pct = true, true, false, 100 })
	a.eng.Log("ok", fmt.Sprintf("[Update] v%s sudah diunduh & tanda tangannya terverifikasi — siap dipasang", rel.Version))
	_ = manual
}

// UpdateInstall memasang versi yang sudah diunduh lalu memulai ulang aplikasi.
// Foto yang sedang diunggah kembali ke antrean otomatis dan dilanjutkan setelah mulai ulang.
func (a *App) UpdateInstall() error {
	a.upd.mu.Lock()
	staged, ready, installing := a.upd.staged, a.upd.st.Ready, a.upd.st.Installing
	a.upd.mu.Unlock()
	if installing {
		return errors.New("pemasangan sedang berjalan")
	}
	if !ready || staged == "" {
		return errors.New("belum ada pembaruan yang siap dipasang")
	}
	if _, err := os.Stat(staged); err != nil {
		return errors.New("berkas pembaruan hilang — cek pembaruan lagi")
	}
	a.updSet(func(s *UpdateInfo) { s.Installing, s.Error = true, "" })
	a.eng.Log("info", fmt.Sprintf("[Update] memasang v%s dan memulai ulang… (%d foto sedang diunggah akan dilanjutkan)",
		a.UpdateState().Latest, a.eng.Store.Counts().Uploading))
	if err := update.Apply(staged, "", os.Getpid(), a.updApplyLog(), true); err != nil {
		a.updSet(func(s *UpdateInfo) { s.Installing, s.Error = false, err.Error() })
		a.eng.Log("error", "[Update] pemasangan gagal: "+err.Error())
		return err
	}
	// Tutup lewat jalur normal (shutdown menyimpan antrean). Sedikit jeda supaya balasan ke UI sempat terkirim.
	go func() {
		time.Sleep(400 * time.Millisecond)
		wrt.Quit(a.ctx)
		// Bila aplikasi masih hidup 10 dtk kemudian, jangan biarkan tombol "Memasang…" terkunci
		// selamanya: versi baru sudah di tempatnya, tinggal ditutup & dibuka manual.
		time.Sleep(10 * time.Second)
		msg := "Versi baru sudah terpasang tapi aplikasi belum menutup sendiri — tutup aplikasi lalu buka lagi."
		a.updSet(func(s *UpdateInfo) { s.Installing, s.Error = false, msg })
		a.eng.Log("warn", "[Update] "+msg)
	}()
	return nil
}
