package core

import (
	"path/filepath"
	"testing"
	"time"
)

// Saringan nomor eksklusif ("hanya ganjil"/"hanya genap"): begitu semua file yang cocok
// selesai diunggah, unggah otomatis harus berhenti sendiri (bukan diam-diam lanjut
// menunggu file baru — itu biasanya tanggung jawab laptop/akun lain).
func TestParityOnlyAutoPauseWhenDrained(t *testing.T) {
	dataDir := t.TempDir()
	watch := t.TempDir()

	eng, err := NewEngine(dataDir, NopEmitter{})
	if err != nil {
		t.Fatal(err)
	}
	eng.Cfg.Set(func(s *Settings) {
		s.WatchFolder = watch
		s.Destination = "local"
		s.LocalOutputFolder = filepath.Join(dataDir, "hasil")
		s.AutoUpload = true
		s.ScanIntervalSec = 1
		s.Parity = Parity{Mode: "odd", Only: true}
	})
	eng.Start()
	defer eng.Stop()

	writeTestJPEG(t, filepath.Join(watch, "DSC_0001.jpg"), 1) // ganjil — harus diunggah
	writeTestJPEG(t, filepath.Join(watch, "DSC_0002.jpg"), 2) // genap — harus dilewati sama sekali

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if eng.Store.Counts().Done >= 1 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got := eng.Store.Counts().Done; got < 1 {
		t.Fatalf("file ganjil tidak selesai diunggah: counts=%+v", eng.Store.Counts())
	}

	// Setelah satu-satunya file yang cocok selesai, unggah otomatis harus berhenti sendiri.
	deadline = time.Now().Add(5 * time.Second)
	paused := false
	for time.Now().Before(deadline) {
		if p, _, _ := eng.Queue.Paused(); p {
			paused = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !paused {
		t.Fatalf("unggah otomatis harus berhenti sendiri setelah antrean ganjil habis")
	}
	if eng.Cfg.Get().AutoUpload {
		t.Fatalf("AutoUpload di config harus ikut dimatikan setelah auto-jeda")
	}

	// File genap tidak pernah ikut masuk ke store (dilewati di AddFile, bukan diunggah).
	if c := eng.Store.Counts(); c.Total != 1 {
		t.Fatalf("hanya file ganjil yang boleh masuk store, dapat total=%d", c.Total)
	}
}
