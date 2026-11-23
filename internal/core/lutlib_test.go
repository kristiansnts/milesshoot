package core

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCubeFile(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(cube(identity)), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Import menyalin file .cube yang valid ke pustaka, dan daftar/muncul lewat ListLUTs.
func TestImportAndListLUTs(t *testing.T) {
	dataDir := t.TempDir()
	srcDir := t.TempDir()
	src := writeCubeFile(t, srcDir, "Kodak Portra.cube")

	got, err := ImportLUT(dataDir, src)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Kodak Portra.cube" {
		t.Errorf("nama hasil impor salah: %q", got.Name)
	}
	if _, err := os.Stat(got.Path); err != nil {
		t.Fatalf("file hasil impor tidak ada di pustaka: %v", err)
	}
	// sumber asli tidak boleh ikut terhapus/berubah
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("file sumber seharusnya tetap ada: %v", err)
	}

	list := ListLUTs(dataDir)
	if len(list) != 1 || list[0].Name != "Kodak Portra.cube" {
		t.Fatalf("ListLUTs tidak sesuai: %+v", list)
	}
}

// Mengimpor file dengan nama yang sudah ada dapat sufiks angka, tidak menimpa yang lama.
func TestImportLUTDuplicateNameSuffixed(t *testing.T) {
	dataDir := t.TempDir()
	srcDir := t.TempDir()
	src := writeCubeFile(t, srcDir, "Warna.cube")

	first, err := ImportLUT(dataDir, src)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ImportLUT(dataDir, src)
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path {
		t.Fatal("impor kedua seharusnya tidak menimpa file pertama")
	}
	if len(ListLUTs(dataDir)) != 2 {
		t.Fatalf("pustaka seharusnya punya 2 entri, dapat %d", len(ListLUTs(dataDir)))
	}
}

// File yang bukan .cube valid (header LUT_3D_SIZE rusak) harus ditolak, tidak ikut masuk pustaka.
func TestImportLUTRejectsInvalidFile(t *testing.T) {
	dataDir := t.TempDir()
	srcDir := t.TempDir()
	bad := filepath.Join(srcDir, "bukan-lut.cube")
	if err := os.WriteFile(bad, []byte("ini bukan file LUT sama sekali\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportLUT(dataDir, bad); err == nil {
		t.Fatal("file .cube tidak valid seharusnya ditolak, bukan diterima")
	}
	if len(ListLUTs(dataDir)) != 0 {
		t.Fatal("file tidak valid tidak boleh masuk pustaka")
	}
}

func TestDeleteLUT(t *testing.T) {
	dataDir := t.TempDir()
	srcDir := t.TempDir()
	src := writeCubeFile(t, srcDir, "Sunset.cube")
	got, err := ImportLUT(dataDir, src)
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteLUT(dataDir, got.Name); err != nil {
		t.Fatal(err)
	}
	if len(ListLUTs(dataDir)) != 0 {
		t.Fatal("LUT seharusnya sudah hilang dari pustaka setelah dihapus")
	}
}

// Anti path-traversal: nama yang menyelinap keluar dari folder pustaka harus ditolak,
// bukan diam-diam menghapus file sembarang di luar data/luts/.
func TestDeleteLUTRejectsPathTraversal(t *testing.T) {
	dataDir := t.TempDir()
	// file "korban" di luar folder luts, yang seharusnya TIDAK boleh terhapus
	victim := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(victim, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../config.json", "/etc/passwd", "sub/../../config.json"} {
		if err := DeleteLUT(dataDir, bad); err == nil {
			t.Fatalf("nama %q seharusnya ditolak, bukan diterima", bad)
		}
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("file di luar pustaka LUT tidak boleh ikut terhapus")
	}
}
