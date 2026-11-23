package usbcam

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindDCIM(t *testing.T) {
	root := t.TempDir()
	if got := findDCIM(root); got != "" {
		t.Fatalf("drive tanpa DCIM tidak boleh terdeteksi, dapat %q", got)
	}
	// nama folder bisa beda besar-kecil huruf (kamera bervariasi)
	d := filepath.Join(root, "Dcim")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findDCIM(root); got != d {
		t.Fatalf("DCIM (huruf campur) harus ketemu, dapat %q", got)
	}
}

func TestCountPhotos(t *testing.T) {
	dcim := filepath.Join(t.TempDir(), "DCIM")
	sub := filepath.Join(dcim, "100NZ_6")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"DSC_0001.JPG", "DSC_0002.NEF", "DSC_0003.MOV", ".hidden.jpg", "catatan.txt"} {
		if err := os.WriteFile(filepath.Join(sub, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// JPG + MOV dihitung; NEF (RAW), berkas tersembunyi, dan .txt tidak
	if got := countPhotos(dcim); got != 2 {
		t.Fatalf("harus 2 foto terhitung, dapat %d", got)
	}
}

func TestIsPhotoName(t *testing.T) {
	yes := []string{"A.JPG", "a.jpeg", "b.HEIC", "c.mp4", "d.MOV", "e.png"}
	no := []string{".DS_Store", "x.nef", "y.txt", "z.cr3", ".a.jpg"}
	for _, n := range yes {
		if !isPhotoName(n) {
			t.Errorf("%q harusnya dihitung", n)
		}
	}
	for _, n := range no {
		if isPhotoName(n) {
			t.Errorf("%q tidak boleh dihitung", n)
		}
	}
}
