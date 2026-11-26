package core

import (
	"testing"
	"time"

	"milesshoot/internal/fotoyu"
)

// StartAutoLoop/StopAutoLoop: harus benar-benar berhenti (tidak terus tick setelah
// Stop), dan boleh dipanggil berkali-kali tanpa panic (mis. saat app.Stop() dipanggil
// dua kali, atau engine di-restart dalam test).
func TestHiresAutoLoopStartStop(t *testing.T) {
	dataDir := t.TempDir()
	cfg, err := LoadConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := LoadStore(dataDir)
	fy := fotoyu.New(dataDir) // tanpa akun — Refresh() akan gagal cepat, bukan hang
	h := NewHiresManager(cfg, store, fy, NopEmitter{})

	old := hiresAutoInterval
	hiresAutoInterval = 50 * time.Millisecond
	defer func() { hiresAutoInterval = old }()

	h.StartAutoLoop()
	time.Sleep(180 * time.Millisecond) // beri waktu beberapa tick terjadi

	h.StopAutoLoop()
	loadedAtAfterStop := h.State().LoadedAt
	time.Sleep(180 * time.Millisecond) // kalau loop masih jalan, loadedAt akan berubah lagi

	if got := h.State().LoadedAt; got != loadedAtAfterStop {
		t.Fatalf("loop masih jalan setelah Stop(): loadedAt berubah dari %d ke %d", loadedAtAfterStop, got)
	}

	// panggil Stop() dua kali tidak boleh panic (mis. close pada channel yang sudah ditutup)
	h.StopAutoLoop()
}

// tick(): Refresh() selalu dipanggil; SendAllExact() hanya dipanggil bila
// cfg.Hires.Auto menyala. Tanpa akun, SendAllExact tidak melakukan apa-apa
// (tidak ada item), jadi yang diuji di sini murni bahwa tick() tidak panik
// pada kedua kondisi toggle.
func TestHiresTickRespectsAutoToggle(t *testing.T) {
	dataDir := t.TempDir()
	cfg, err := LoadConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := LoadStore(dataDir)
	fy := fotoyu.New(dataDir)
	h := NewHiresManager(cfg, store, fy, NopEmitter{})

	cfg.Set(func(s *Settings) { s.Hires.Auto = false })
	h.tick() // tidak boleh panik; Refresh gagal (belum login) tapi tick tetap selesai

	cfg.Set(func(s *Settings) { s.Hires.Auto = true })
	h.tick() // juga tidak boleh panik walau Auto menyala
}
