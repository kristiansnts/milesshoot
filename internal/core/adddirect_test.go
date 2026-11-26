package core

import (
	"path/filepath"
	"testing"
)

// Foto dari luar folder pantau (mis. hasil Video → Foto) harus bisa masuk antrean, tidak kena
// saringan ganjil/genap, dan tidak dimasukkan dua kali.
func TestAddDirect(t *testing.T) {
	eng, err := NewEngine(t.TempDir(), NopEmitter{})
	if err != nil {
		t.Fatal(err)
	}
	eng.Queue.Pause("manual")
	eng.Cfg.Set(func(s *Settings) {
		s.WatchFolder = t.TempDir() // folder lain: file di bawah ada di luar folder pantau
		s.Parity = Parity{Mode: "odd"}
	})
	out := t.TempDir()
	f := filepath.Join(out, "klip_02.jpg") // genap: akan disaring bila lewat jalur scanner
	writeTestJPEG(t, f, 7)

	ok, err := eng.Queue.AddDirect(f)
	if err != nil || !ok {
		t.Fatalf("harus masuk antrean: ok=%v err=%v", ok, err)
	}
	if c := eng.Store.Counts(); c.Pending != 1 {
		t.Fatalf("mau 1 pending, dapat %+v", c)
	}
	var rec Record
	for _, r := range eng.Store.All() {
		rec = r
	}
	if !rec.Requeue {
		t.Error("harus Requeue supaya lolos cek folder pantau dan tidak dipindah")
	}
	if parityBlocked(&rec, eng.Cfg.Get()) || !eng.Queue.inCurrent(&rec, eng.Cfg.Get()) {
		t.Error("record tidak boleh diblokir saringan nomor/folder")
	}
	if ok, _ := eng.Queue.AddDirect(f); ok {
		t.Error("file yang sama tidak boleh masuk dua kali")
	}
	if _, err := eng.Queue.AddDirect(filepath.Join(out, "tidak-ada.jpg")); err == nil {
		t.Error("file hilang harus error")
	}
}
