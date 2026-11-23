package core

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// GradeJPEG dipakai video.Prepare (lewat VideoPrepareFn) buat mewarnai frame pratinjau
// video dengan LUT video — fitur yang tadinya cuma ada di pengaturan tapi tak pernah
// benar-benar diterapkan ke video.
func TestGradeJPEGAppliesLUT(t *testing.T) {
	dir := t.TempDir()
	cubePath := filepath.Join(dir, "invert.cube")
	if err := os.WriteFile(cubePath, []byte(cube(invert)), 0o644); err != nil {
		t.Fatal(err)
	}

	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{10, 20, 30, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}

	graded := GradeJPEG(buf.Bytes(), LUT{Enabled: true, File: cubePath, Strength: 100})
	if bytes.Equal(graded, buf.Bytes()) {
		t.Fatalf("GradeJPEG tidak mengubah apa pun — LUT tidak diterapkan")
	}
	out, err := jpeg.Decode(bytes.NewReader(graded))
	if err != nil {
		t.Fatalf("hasil GradeJPEG bukan JPEG valid: %v", err)
	}
	r, g, b, _ := out.At(4, 4).RGBA()
	r8, g8, b8 := r>>8, g>>8, b>>8
	// 10→~245, 20→~235, 30→~225 (toleransi lebar karena re-encode JPEG lossy)
	if r8 < 200 || g8 < 190 || b8 < 180 {
		t.Fatalf("warna tidak terlihat dibalik: got (%d,%d,%d)", r8, g8, b8)
	}
}

func TestGradeJPEGNoopWhenDisabled(t *testing.T) {
	raw := []byte("bukan jpeg beneran, cuma placeholder")
	out := GradeJPEG(raw, LUT{Enabled: false})
	if !bytes.Equal(out, raw) {
		t.Fatalf("LUT mati seharusnya tidak mengubah buffer")
	}
}

func TestWatermarkPreviewImage(t *testing.T) {
	plain, err := WatermarkPreviewImage(Watermark{Enabled: false})
	if err != nil {
		t.Fatalf("WatermarkPreviewImage (mati): %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(plain)); err != nil {
		t.Fatalf("hasil (mati) bukan JPEG valid: %v", err)
	}

	withWm, err := WatermarkPreviewImage(Watermark{Enabled: true, Type: "text", Text: "fotonaik", Pos: "br", Size: 22, Opacity: 80})
	if err != nil {
		t.Fatalf("WatermarkPreviewImage (nyala): %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(withWm)); err != nil {
		t.Fatalf("hasil (nyala) bukan JPEG valid: %v", err)
	}
	if bytes.Equal(plain, withWm) {
		t.Fatalf("watermark nyala harus menghasilkan gambar berbeda dari yang mati")
	}
}

func TestGradeJPEGNoopOnBrokenLUTFile(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	out := GradeJPEG(buf.Bytes(), LUT{Enabled: true, File: "/tidak/ada/begini.cube", Strength: 100})
	if !bytes.Equal(out, buf.Bytes()) {
		t.Fatalf("LUT yang gagal dimuat harus mengembalikan buffer apa adanya, bukan rusak")
	}
}
