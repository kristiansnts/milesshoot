package core

import (
	"path/filepath"
	"testing"
	"time"
)

// Memilih Ganjil/Genap selalu berarti "hanya itu", bukan sekadar didahulukan.
func TestParityModeImpliesOnly(t *testing.T) {
	eng, err := NewEngine(t.TempDir(), NopEmitter{})
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := eng.Cfg.Update([]byte(`{"parity":{"mode":"odd","only":false}}`)); !s.Parity.Only {
		t.Fatalf("mode ganjil harus otomatis only=true, dapat %+v", s.Parity)
	}
	if s, _ := eng.Cfg.Update([]byte(`{"parity":{"mode":"all"}}`)); s.Parity.Only {
		t.Fatalf("mode semua harus only=false, dapat %+v", s.Parity)
	}
}

// File genap yang sudah telanjur masuk antrean (saat masih "Semua") tidak boleh ikut
// terunggah setelah saringan diganti ke Ganjil, dan tidak boleh menahan auto-jeda.
func TestParityFiltersAlreadyQueued(t *testing.T) {
	dataDir := t.TempDir()
	watch := t.TempDir()

	eng, err := NewEngine(dataDir, NopEmitter{})
	if err != nil {
		t.Fatal(err)
	}
	eng.Queue.Pause("manual")
	eng.Cfg.Set(func(s *Settings) {
		s.WatchFolder = watch
		s.Destination = "local"
		s.LocalOutputFolder = filepath.Join(dataDir, "hasil")
		s.ScanIntervalSec = 1
		s.Parity = Parity{Mode: "all"}
	})
	eng.Start()
	defer eng.Stop()

	writeTestJPEG(t, filepath.Join(watch, "DSC_0001.jpg"), 1)
	writeTestJPEG(t, filepath.Join(watch, "DSC_0002.jpg"), 2)

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && eng.Store.Counts().Pending < 2 {
		time.Sleep(200 * time.Millisecond)
	}
	if c := eng.Store.Counts(); c.Pending != 2 {
		t.Fatalf("kedua file harus menunggu di antrean dulu: counts=%+v", c)
	}

	eng.Cfg.Update([]byte(`{"parity":{"mode":"odd"}}`))
	eng.Queue.Resume()

	deadline = time.Now().Add(20 * time.Second)
	paused := false
	for time.Now().Before(deadline) {
		if p, _, _ := eng.Queue.Paused(); p && eng.Store.Counts().Done >= 1 {
			paused = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	c := eng.Store.Counts()
	if c.Done != 1 {
		t.Fatalf("hanya file ganjil yang boleh terunggah: counts=%+v", c)
	}
	if c.Pending != 1 {
		t.Fatalf("file genap harus tetap menunggu, bukan terunggah: counts=%+v", c)
	}
	if !paused {
		t.Fatalf("file genap yang tertinggal tidak boleh menahan auto-jeda")
	}
	for _, r := range eng.Store.All() {
		if r.Status == "done" && ParityOf(r.Name) != "odd" {
			t.Fatalf("file %s (bukan ganjil) ikut terunggah", r.Name)
		}
	}
}
