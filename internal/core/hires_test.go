package core

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func md5OfBytes(b []byte) string {
	s := md5.Sum(b)
	return hex.EncodeToString(s[:])
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return md5OfBytes([]byte(content))
}

func newHiresForTest(t *testing.T, watch string) *HiresManager {
	t.Helper()
	dataDir := t.TempDir()
	cfg, err := LoadConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Set(func(s *Settings) { s.WatchFolder = watch })
	return NewHiresManager(cfg, LoadStore(dataDir), nil, NopEmitter{})
}

// 1) Cocok pasti lewat riwayat: signature FotoYu = MD5 file asli = kunci store kita.
func TestHiresMatchFromStore(t *testing.T) {
	watch := t.TempDir()
	// file sudah dipindah ke "Sudah upload" setelah diunggah — jalur yang paling sering terjadi
	p := filepath.Join(watch, "Sudah upload", "DSC_0001.JPG")
	sig := writeFile(t, p, "isi-foto-asli")

	h := newHiresForTest(t, watch)
	h.store.Add(&Record{MD5: sig, Path: p, Name: "DSC_0001.JPG", Status: "done"})

	m := h.matchOne(sig, "DSC_0001.JPG", h.nameIndex())
	if m.Type != "md5" || m.Path != p {
		t.Fatalf("harus cocok pasti dari riwayat, dapat %+v", m)
	}
}

// 2) Riwayat hilang → cari nama yang sama lalu buktikan dengan MD5.
func TestHiresMatchByNameThenMD5(t *testing.T) {
	watch := t.TempDir()
	p := filepath.Join(watch, "Sudah upload", "DSC_0002.JPG")
	sig := writeFile(t, p, "isi-foto-kedua")

	h := newHiresForTest(t, watch) // store sengaja kosong
	m := h.matchOne(sig, "DSC_0002.JPG", h.nameIndex())
	if m.Type != "md5" || m.Path != p {
		t.Fatalf("harus terbukti lewat MD5 file bernama sama, dapat %+v", m)
	}
}

// 3) Nama sama tapi isi beda (foto sudah diedit) → "cocok nama", perlu dicek user.
func TestHiresMatchNameOnly(t *testing.T) {
	watch := t.TempDir()
	p := filepath.Join(watch, "DSC_0003.JPG")
	writeFile(t, p, "isi-yang-sudah-diedit")
	sigAsli := md5OfBytes([]byte("isi-asli-yang-dijual"))

	h := newHiresForTest(t, watch)
	m := h.matchOne(sigAsli, "DSC_0003.JPG", h.nameIndex())
	if m.Type != "name" || m.Path != p {
		t.Fatalf("harus 'cocok nama' (isi beda), dapat %+v", m)
	}
}

// 4) Tidak ada file sama sekali.
func TestHiresNoMatch(t *testing.T) {
	watch := t.TempDir()
	h := newHiresForTest(t, watch)
	m := h.matchOne(md5OfBytes([]byte("entah")), "HILANG.JPG", h.nameIndex())
	if m.Type != "none" {
		t.Fatalf("harus 'none', dapat %+v", m)
	}
}

// 5) Folder tambahan ikut dicari (file asli disimpan terpisah dari folder unggah).
func TestHiresExtraFolder(t *testing.T) {
	watch := t.TempDir()
	extra := t.TempDir()
	p := filepath.Join(extra, "RAW-ish", "DSC_0009.JPG")
	sig := writeFile(t, p, "asli-di-folder-lain")

	h := newHiresForTest(t, watch)
	h.AddFolder(extra)

	m := h.matchOne(sig, "DSC_0009.JPG", h.nameIndex())
	if m.Type != "md5" || m.Path != p {
		t.Fatalf("file di folder tambahan harus ketemu, dapat %+v", m)
	}
	// hapus folder → tidak ketemu lagi
	h.RemoveFolder(extra)
	h.verified = map[string]string{} // kosongkan cache supaya benar-benar mencari ulang
	if m2 := h.matchOne(sig, "DSC_0009.JPG", h.nameIndex()); m2.Type != "none" {
		t.Fatalf("setelah folder dihapus harusnya tidak ketemu, dapat %+v", m2)
	}
}

// Ringkasan status menghitung tiap kategori dengan benar.
func TestHiresStateCounts(t *testing.T) {
	h := newHiresForTest(t, t.TempDir())
	h.items = []HiresItem{
		{ID: "1", Match: HiresMatch{Type: "md5"}},
		{ID: "2", Match: HiresMatch{Type: "md5"}},
		{ID: "3", Match: HiresMatch{Type: "name"}},
		{ID: "4", Match: HiresMatch{Type: "none"}},
		{ID: "5", Match: HiresMatch{Type: "md5"}, Sent: true},
	}
	st := h.State()
	if st.Exact != 2 || st.ByName != 1 || st.None != 1 || st.Sent != 1 {
		t.Fatalf("hitungan salah: %+v", st)
	}
}
