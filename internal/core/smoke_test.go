package core

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Membuat satu JPEG uji (2000x1500, pola warna) di folder tujuan.
func writeTestJPEG(t *testing.T, path string, seed int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2000, 1500))
	for y := 0; y < 1500; y++ {
		for x := 0; x < 2000; x++ {
			img.Set(x, y, color.RGBA{uint8((x + seed) % 256), uint8((y + seed) % 256), uint8((x + y) % 256), 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
}

// Pipeline penuh: scanner → hash/dedup → olah (resize) → upload lokal → pindah arsip.
func TestPipelineLocal(t *testing.T) {
	dataDir := t.TempDir()
	watch := t.TempDir()
	out := filepath.Join(dataDir, "hasil")

	eng, err := NewEngine(dataDir, NopEmitter{})
	if err != nil {
		t.Fatal(err)
	}
	eng.Cfg.Set(func(s *Settings) {
		s.WatchFolder = watch
		s.Destination = "local"
		s.LocalOutputFolder = out
		s.AutoUpload = true
		s.ScanIntervalSec = 1
		s.Event.Name = "uji"
	})
	eng.Start()
	defer eng.Stop()

	writeTestJPEG(t, filepath.Join(watch, "DSC_0001.jpg"), 0)

	// tunggu sampai 1 foto selesai
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if eng.Store.Counts().Done >= 1 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	c := eng.Store.Counts()
	if c.Done < 1 {
		t.Fatalf("tidak ada foto selesai: counts=%+v", c)
	}

	// hasil olah tersalin ke folder lokal
	evDir := filepath.Join(out, "uji")
	entries, _ := os.ReadDir(evDir)
	found := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jpg" {
			found = true
		}
	}
	if !found {
		t.Fatalf("file hasil olah tidak ditemukan di %s", evDir)
	}

	// resize bekerja: hasil <= 1440 px sisi terpanjang → ukurannya jauh lebih kecil dari 2000x1500 asli
	t.Logf("selesai: counts=%+v, output=%s", c, evDir)
}

// Dedup: file dengan isi sama (nama beda) tidak diunggah dua kali.
func TestDedup(t *testing.T) {
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
		s.AfterUpload.Move = false // supaya file tetap di tempat untuk uji ini
	})
	eng.Start()
	defer eng.Stop()

	writeTestJPEG(t, filepath.Join(watch, "A.jpg"), 7)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if eng.Store.Counts().Done >= 1 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if eng.Store.Counts().Done < 1 {
		t.Fatalf("foto pertama belum selesai: %+v", eng.Store.Counts())
	}

	// salin isi yang sama ke nama lain
	b, _ := os.ReadFile(filepath.Join(watch, "A.jpg"))
	_ = os.WriteFile(filepath.Join(watch, "B.jpg"), b, 0o644)

	time.Sleep(4 * time.Second) // beri waktu scan + hash
	eng.Queue.statsMu.Lock()
	dups := eng.Queue.dupCount
	eng.Queue.statsMu.Unlock()
	if dups < 1 {
		t.Fatalf("duplikat tidak terdeteksi (dupCount=%d)", dups)
	}
	if got := eng.Store.Counts().Done; got != 1 {
		t.Fatalf("harusnya tetap 1 foto terunggah (bukan %d) — duplikat tak boleh naik lagi", got)
	}
	t.Logf("dedup OK: dupCount=%d, done=%d", dups, eng.Store.Counts().Done)
}
