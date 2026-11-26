package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"milesshoot/internal/core"
	"milesshoot/internal/video"

	wrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// appVersion: nomor versi yang tampil di log & laporan. Bisa ditimpa saat build:
//
//	wails build -ldflags "-X main.appVersion=1.2.0"
var appVersion = "1.0.0"

func buildRevision() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "?"
	}
	rev, mod := "", ""
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				mod = "+perubahan-lokal"
			}
		}
	}
	if len(rev) > 9 {
		rev = rev[:9]
	}
	if rev == "" {
		return bi.GoVersion
	}
	return rev + mod + " · " + bi.GoVersion
}

func ffmpegLine() string {
	p, v := video.FfmpegInfo()
	if p == "" {
		return "TIDAK DITEMUKAN (fitur video & Video → Foto tidak jalan)"
	}
	return p + " · " + v
}

// logStartup mencatat keadaan awal sesi: dengan ini satu berkas log harian sudah cukup
// untuk tahu versi, OS, ffmpeg, dan setelan yang dipakai saat masalah terjadi.
func (a *App) logStartup() {
	c := a.eng.Cfg.Get()
	a.eng.Log("info", fmt.Sprintf("===== milesshoot v%s dimulai (%s) =====", appVersion, buildRevision()))
	a.eng.Log("info", fmt.Sprintf("Sistem: %s/%s · %d CPU · data: %s", runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), a.eng.Cfg.DataDir()))
	a.eng.Log("info", "ffmpeg: "+ffmpegLine())
	if !video.Available() {
		a.eng.Log("warn", "ffmpeg tidak ada — video tidak bisa diunggah dan Video → Foto tidak jalan")
	}
	a.eng.Log("info", fmt.Sprintf("Setelan: tujuan=%s · unggahOtomatis=%v · ganjilGenap=%s · video=%v · subfolder=%v · kompres=%d · performa=%s · folder=%q",
		c.Destination, c.AutoUpload, c.Parity.Mode, c.Video.Enabled, c.IncludeSubfolders, c.Compress.Size, c.Performance, c.WatchFolder))
	acct := a.eng.Fotoyu.Session()
	a.eng.Log("info", fmt.Sprintf("Akun FotoYu: masuk=%v · @%s · %d akun tersimpan", acct.LoggedIn, acct.Username, len(a.eng.Fotoyu.Accounts())))
}

// DiagnosticReport menyusun satu teks lengkap untuk dikirim saat minta bantuan:
// versi & sistem, setelan, akun (tanpa token/password), antrean, foto bermasalah, dan log terbaru.
func (a *App) DiagnosticReport() string {
	var b strings.Builder
	w := func(f string, args ...any) { fmt.Fprintf(&b, f+"\n", args...) }
	sec := func(t string) { w("\n=== %s ===", t) }

	now := time.Now()
	w("LAPORAN MASALAH — milesshoot v%s", appVersion)
	w("Dibuat   : %s (UTC %s)", now.Format("2006-01-02 15:04:05 -0700"), now.UTC().Format("15:04"))
	w("Build    : %s", buildRevision())
	w("Sistem   : %s/%s · %d CPU", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	w("Data     : %s", a.eng.Cfg.DataDir())
	w("ffmpeg   : %s", ffmpegLine())

	sec("STATUS")
	st := a.eng.Status()
	w("Dijeda   : %v (alasan=%q, pesan=%q)", st.Paused, st.PauseReason, st.PauseMsg)
	w("Antrean  : total=%d menunggu=%d diunggah=%d selesai=%d gagal=%d dobel=%d",
		st.Counts.Total, st.Counts.Pending, st.Counts.Uploading, st.Counts.Done, st.Counts.Failed, st.Counts.Duplicate)
	w("Kecepatan: %d foto/menit", st.PerMinute)
	w("Pemindai : folder=%q media=%d dikenal=%d menunggu=%d raw=%d", st.Scan.Folder, st.Scan.Media, st.Scan.Known, st.Scan.Waiting, st.Scan.Raw)

	sec("AKUN FOTOYU (tanpa token & password)")
	for _, ac := range a.eng.Fotoyu.Accounts() {
		w("- @%s · %s · aktif=%v", ac.Username, ac.Name, ac.Active)
	}
	if !st.Account.LoggedIn {
		w("Belum masuk / sesi habis")
	}

	sec("SETELAN")
	if js, err := json.MarshalIndent(a.eng.Cfg.Get(), "", "  "); err == nil {
		w("%s", js)
	}

	sec("VIDEO → FOTO")
	vf := a.vfSnapshot()
	w("Berjalan=%v total=%d foto=%d masukAntrean=%d", vf.Running, vf.Total, vf.Photos, vf.Queued)
	for _, it := range vf.Items {
		w("- %s → %d foto, diperbesar=%d, mirip=%d, dilewati=%v, galat=%q", filepath.Base(it.Video), len(it.Files), it.Enlarged, it.Similar, it.Skipped, it.Error)
	}

	sec("FOTO BERMASALAH (terbaru, maks 25)")
	recs := a.eng.Store.All()
	var bad []core.Record
	for _, r := range recs {
		if r.Status == "failed" || (r.Status == "pending" && r.Error != "") {
			bad = append(bad, r)
		}
	}
	sort.Slice(bad, func(i, j int) bool { return bad[i].AddedAt > bad[j].AddedAt })
	if len(bad) > 25 {
		bad = bad[:25]
	}
	if len(bad) == 0 {
		w("(tidak ada)")
	}
	for _, r := range bad {
		w("- %s · %s · %s · %dKB · tahap=%q · percobaan=%d · galat=%q", r.Name, r.Kind, r.Status, r.Size/1024, r.Stage, r.Attempts, r.Error)
	}

	sec("LOG TERBARU (maks 500 baris, terlama di atas)")
	if a.logs != nil {
		lines := a.logs.Tail(500)
		for i := len(lines) - 1; i >= 0; i-- {
			l := lines[i]
			w("%s  %-5s  %s", time.UnixMilli(l.T).Format("2006-01-02 15:04:05"), strings.ToUpper(l.Level), l.Msg)
		}
	}
	return b.String()
}

// SaveDiagnosticReport menyimpan laporan ke <data>/laporan/ dan membukanya di Finder/Explorer.
func (a *App) SaveDiagnosticReport() (string, error) {
	dir := filepath.Join(a.eng.Cfg.DataDir(), "laporan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "laporan-"+time.Now().Format("20060102-150405")+".txt")
	if err := os.WriteFile(path, []byte(a.DiagnosticReport()), 0o644); err != nil {
		return "", err
	}
	// simpan 10 laporan terakhir
	if ents, err := os.ReadDir(dir); err == nil {
		var names []string
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), "laporan-") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for len(names) > 10 {
			_ = os.Remove(filepath.Join(dir, names[0]))
			names = names[1:]
		}
	}
	wrt.BrowserOpenURL(a.ctx, "file://"+dir)
	a.eng.Log("info", "Laporan masalah disimpan: "+path)
	return path, nil
}
