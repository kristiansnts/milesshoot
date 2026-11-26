package video

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"milesshoot/internal/core"
)

func TestPrepareVideo(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg tidak ada")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "klip.mp4")
	// buat video uji 3 detik 320x240 (pola testsrc) pakai ffmpeg
	cmd := exec.Command(findFfmpeg(), "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "testsrc=duration=3:size=640x360:rate=24", "-pix_fmt", "yuv420p", "-y", src)
	if err := cmd.Run(); err != nil {
		t.Fatalf("gagal membuat video uji: %v", err)
	}

	pv, err := Prepare(src, "auto", 2, core.LUT{})
	if err != nil {
		t.Fatalf("Prepare gagal: %v", err)
	}
	if pv.Frames != 17 {
		t.Fatalf("harus 17 frame, dapat %d", pv.Frames)
	}
	if pv.Width == 0 || pv.Height == 0 {
		t.Fatalf("dimensi frame kosong: %dx%d", pv.Width, pv.Height)
	}
	if len(pv.FirstFrame) < 100 {
		t.Fatalf("frame pertama kosong")
	}
	// zip harus berisi 17 entri "0.jpeg".."16.jpeg"
	zr, err := zip.NewReader(bytes.NewReader(pv.Zip), int64(len(pv.Zip)))
	if err != nil {
		t.Fatalf("zip tidak valid: %v", err)
	}
	if len(zr.File) != 17 {
		t.Fatalf("zip harus 17 file, dapat %d", len(zr.File))
	}
	if zr.File[0].Name != "0.jpeg" {
		t.Fatalf("nama file pertama salah: %s", zr.File[0].Name)
	}
	// sisi terpanjang frame = 1440 (640x360 → 1440x810)
	if pv.Width != 1440 {
		t.Fatalf("sisi terpanjang harus 1440, dapat %d", pv.Width)
	}
	t.Logf("video OK: %dx%d, %d frame, zip %d KB", pv.Width, pv.Height, pv.Frames, len(pv.Zip)/1024)
	_ = os.Remove(src)
}

// LUT video (cfg.LUT.Video) harus benar-benar diwarnai ke frame pratinjau — sebelumnya
// setelan ini ada di UI tapi tidak pernah dipakai di jalur video sama sekali.
func TestPrepareVideoAppliesLUT(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg tidak ada")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "klip.mp4")
	cmd := exec.Command(findFfmpeg(), "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "testsrc=duration=2:size=320x240:rate=12", "-pix_fmt", "yuv420p", "-y", src)
	if err := cmd.Run(); err != nil {
		t.Fatalf("gagal membuat video uji: %v", err)
	}

	cubePath := filepath.Join(dir, "invert.cube")
	cube := "LUT_3D_SIZE 2\n" +
		"1.0 1.0 1.0\n0.0 1.0 1.0\n1.0 0.0 1.0\n0.0 0.0 1.0\n" +
		"1.0 1.0 0.0\n0.0 1.0 0.0\n1.0 0.0 0.0\n0.0 0.0 0.0\n"
	if err := os.WriteFile(cubePath, []byte(cube), 0o644); err != nil {
		t.Fatal(err)
	}

	plain, err := Prepare(src, "auto", 2, core.LUT{})
	if err != nil {
		t.Fatalf("Prepare (tanpa LUT) gagal: %v", err)
	}
	graded, err := Prepare(src, "auto", 2, core.LUT{Enabled: true, File: cubePath, Strength: 100})
	if err != nil {
		t.Fatalf("Prepare (dengan LUT) gagal: %v", err)
	}
	if bytes.Equal(plain.FirstFrame, graded.FirstFrame) {
		t.Fatalf("frame pertama tidak berubah — LUT video tidak diterapkan ke frame")
	}
}
