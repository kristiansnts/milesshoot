package core

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func blackCanvas(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 0, 0, 0, 255
	}
	return img
}

// Watermark yang dinyalakan tanpa isi (teks kosong / logo kosong) harus jatuh ke
// Enabled=false — supaya switch "menyala" di UI tidak diam-diam tidak berefek apa pun.
func TestWatermarkNormalizeDisablesWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Watermark{
		{Enabled: true, Type: "text", Text: ""},
		{Enabled: true, Type: "text", Text: "   "},
		{Enabled: true, Type: "logo", LogoPath: ""},
	} {
		got := cfg.Set(func(s *Settings) { s.Watermark = bad })
		if got.Watermark.Enabled {
			t.Errorf("%+v seharusnya jadi Enabled=false, tetap true", bad)
		}
	}
	// Dengan isi, Enabled harus tetap jalan + nilai aneh dinormalisasi ke batas aman.
	got := cfg.Set(func(s *Settings) {
		s.Watermark = Watermark{Enabled: true, Type: "aneh", Text: "fotonaik", Pos: "tengah-bawah", Size: 999, Opacity: -5}
	})
	if !got.Watermark.Enabled {
		t.Fatal("watermark dengan teks terisi seharusnya tetap Enabled")
	}
	if got.Watermark.Type != "text" {
		t.Errorf("tipe tak dikenal seharusnya jatuh ke 'text', dapat %q", got.Watermark.Type)
	}
	if got.Watermark.Pos != "br" {
		t.Errorf("posisi tak dikenal seharusnya jatuh ke 'br', dapat %q", got.Watermark.Pos)
	}
	if got.Watermark.Size != 60 {
		t.Errorf("ukuran 999 seharusnya di-clamp ke 60, dapat %d", got.Watermark.Size)
	}
	if got.Watermark.Opacity != 5 {
		t.Errorf("opacity -5 seharusnya di-clamp ke 5, dapat %d", got.Watermark.Opacity)
	}
}

// Teks digambar di pojok yang benar, dan piksel di luar area watermark tidak tersentuh.
func TestWatermarkTextPosition(t *testing.T) {
	img := blackCanvas(400, 300)
	wm := Watermark{Enabled: true, Type: "text", Text: "fotonaik", Pos: "br", Size: 20, Opacity: 100}
	if !applyWatermark(img, wm) {
		t.Fatal("applyWatermark mengembalikan false, mau true")
	}
	// pojok kiri-atas harus tetap hitam murni — watermark "br" tidak boleh menyentuhnya
	if r, g, b, _ := img.At(0, 0).RGBA(); r != 0 || g != 0 || b != 0 {
		t.Fatalf("pojok kiri-atas berubah padahal watermark di kanan-bawah: rgb=%d,%d,%d", r, g, b)
	}
	// area kanan-bawah harus ada piksel putih (dari teks) dengan opacity 100%
	found := false
	for y := 250; y < 300; y++ {
		for x := 300; x < 400; x++ {
			if r, g, b, _ := img.At(x, y).RGBA(); r > 0x8000 && g > 0x8000 && b > 0x8000 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("tidak ada piksel putih di area kanan-bawah — teks tak tergambar")
	}
}

// Opacity rendah harus menghasilkan piksel yang jauh lebih redup daripada opacity penuh.
func TestWatermarkOpacityDimsPixel(t *testing.T) {
	full := blackCanvas(400, 300)
	applyWatermark(full, Watermark{Enabled: true, Type: "text", Text: "AAAA", Pos: "c", Size: 30, Opacity: 100})

	dim := blackCanvas(400, 300)
	applyWatermark(dim, Watermark{Enabled: true, Type: "text", Text: "AAAA", Pos: "c", Size: 30, Opacity: 10})

	maxFull, maxDim := 0, 0
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			if r, _, _, _ := full.At(x, y).RGBA(); int(r) > maxFull {
				maxFull = int(r)
			}
			if r, _, _, _ := dim.At(x, y).RGBA(); int(r) > maxDim {
				maxDim = int(r)
			}
		}
	}
	if maxDim >= maxFull {
		t.Fatalf("opacity 10%% (puncak=%d) tidak lebih redup dari opacity 100%% (puncak=%d)", maxDim, maxFull)
	}
}

// Teks kosong (mode text) tidak boleh menggambar apa pun, meski Enabled true (seharusnya
// sudah dicegah oleh normalize(), tapi applyWatermark sendiri juga harus aman dipanggil).
func TestWatermarkEmptyTextNoop(t *testing.T) {
	img := blackCanvas(100, 100)
	if applyWatermark(img, Watermark{Enabled: true, Type: "text", Text: "   "}) {
		t.Fatal("teks kosong/spasi seharusnya tidak menggambar apa pun")
	}
}

// Logo watermark: file tidak ada → tidak panik, tidak menggambar.
func TestWatermarkLogoMissingFile(t *testing.T) {
	img := blackCanvas(100, 100)
	if applyWatermark(img, Watermark{Enabled: true, Type: "logo", LogoPath: "/tidak/ada/logo.png"}) {
		t.Fatal("logo yang filenya hilang seharusnya tidak menggambar apa pun")
	}
}

// Logo watermark sungguhan: file PNG kecil, pastikan tergambar di posisi "tl".
func TestWatermarkLogoDraws(t *testing.T) {
	dir := t.TempDir()
	logoPath := filepath.Join(dir, "logo.png")
	logo := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for i := range logo.Pix {
		logo.Pix[i] = 255
	}
	f, err := os.Create(logoPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, logo); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	img := blackCanvas(400, 300)
	if !applyWatermark(img, Watermark{Enabled: true, Type: "logo", LogoPath: logoPath, Pos: "tl", Size: 20, Opacity: 100}) {
		t.Fatal("logo valid seharusnya tergambar")
	}
	if r, g, b, _ := img.At(15, 15).RGBA(); r < 0x8000 || g < 0x8000 || b < 0x8000 {
		t.Fatalf("pojok kiri-atas (tempat logo 'tl') harus putih, dapat rgb=%d,%d,%d", r, g, b)
	}
	if r, g, b, _ := img.At(395, 295).RGBA(); r != 0 || g != 0 || b != 0 {
		t.Fatalf("pojok kanan-bawah seharusnya tetap hitam: rgb=%d,%d,%d", r, g, b)
	}
}
