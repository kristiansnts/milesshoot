package core

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// jpegBytesWithOrientation menyisipkan APP1 Exif berisi tag Orientation ke JPEG.
func jpegBytesWithOrientation(t *testing.T, img image.Image, orient int, bigEndian bool) []byte {
	t.Helper()
	var bo binary.ByteOrder = binary.LittleEndian
	order := []byte("II")
	if bigEndian {
		bo, order = binary.BigEndian, []byte("MM")
	}
	tiff := make([]byte, 26)
	copy(tiff[0:2], order)
	bo.PutUint16(tiff[2:4], 0x2A)
	bo.PutUint32(tiff[4:8], 8)
	bo.PutUint16(tiff[8:10], 1) // satu entri
	bo.PutUint16(tiff[10:12], 0x0112)
	bo.PutUint16(tiff[12:14], 3) // SHORT
	bo.PutUint32(tiff[14:18], 1)
	bo.PutUint16(tiff[18:20], uint16(orient))
	payload := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:4], uint16(len(payload)+2))
	app1 = append(app1, payload...)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	out := append([]byte{}, raw[:2]...)
	out = append(out, app1...)
	return append(out, raw[2:]...)
}

func noiseImage(w, h int, seed int64) *image.RGBA {
	r := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(r.Intn(256)), uint8(r.Intn(256)), uint8(r.Intn(256)), 255
	}
	return img
}

func TestExifOrientationRead(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for _, be := range []bool{false, true} {
		for o := 1; o <= 8; o++ {
			if got := exifOrientation(jpegBytesWithOrientation(t, img, o, be)); got != o {
				t.Errorf("bigEndian=%v orientasi %d terbaca %d", be, o, got)
			}
		}
	}
	// tanpa EXIF / bukan JPEG → 1
	var plain bytes.Buffer
	_ = jpeg.Encode(&plain, img, nil)
	if got := exifOrientation(plain.Bytes()); got != 1 {
		t.Errorf("tanpa EXIF: %d, mau 1", got)
	}
	if got := exifOrientation([]byte("bukan jpeg")); got != 1 {
		t.Errorf("bukan JPEG: %d, mau 1", got)
	}
	// nilai di luar 1..8 ditolak
	if got := exifOrientation(jpegBytesWithOrientation(t, img, 9, false)); got != 1 {
		t.Errorf("orientasi 9 harus jatuh ke 1, dapat %d", got)
	}
}

// Gambar 3×2 dengan piksel unik: tiap orientasi harus memetakan sudut ke tempat yang benar.
func TestOrientRGBAAllEight(t *testing.T) {
	// src (x,y) → warna unik: R = 10*(y*3+x)
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			src.SetRGBA(x, y, color.RGBA{uint8(10 * (y*3 + x)), 0, 0, 255})
		}
	}
	id := func(x, y int) uint8 { return uint8(10 * (y*3 + x)) } // R milik src(x,y)

	// Untuk tiap orientasi: ukuran hasil + piksel (0,0) dan sudut kanan-bawah.
	type want struct {
		w, h     int
		tl, brPx uint8
	}
	cases := map[int]want{
		1: {3, 2, id(0, 0), id(2, 1)},
		2: {3, 2, id(2, 0), id(0, 1)}, // balik horizontal
		3: {3, 2, id(2, 1), id(0, 0)}, // putar 180
		4: {3, 2, id(0, 1), id(2, 0)}, // balik vertikal
		5: {2, 3, id(0, 0), id(2, 1)}, // transpose
		6: {2, 3, id(0, 1), id(2, 0)}, // putar 90° searah jarum jam
		7: {2, 3, id(2, 1), id(0, 0)}, // transverse
		8: {2, 3, id(2, 0), id(0, 1)}, // putar 90° berlawanan
	}
	for o, w := range cases {
		got := orientRGBA(src, o)
		b := got.Bounds()
		if b.Dx() != w.w || b.Dy() != w.h {
			t.Errorf("orientasi %d: ukuran %dx%d, mau %dx%d", o, b.Dx(), b.Dy(), w.w, w.h)
			continue
		}
		if r := got.RGBAAt(0, 0).R; r != w.tl {
			t.Errorf("orientasi %d: kiri-atas R=%d, mau %d", o, r, w.tl)
		}
		if r := got.RGBAAt(w.w-1, w.h-1).R; r != w.brPx {
			t.Errorf("orientasi %d: kanan-bawah R=%d, mau %d", o, r, w.brPx)
		}
	}
	// sumber tidak boleh ikut berubah
	if src.RGBAAt(0, 0).R != 0 || src.RGBAAt(2, 1).R != 50 {
		t.Error("orientRGBA mengubah gambar sumber")
	}
}

func writeJPEG(t *testing.T, path string, img image.Image, q int) int64 {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return int64(buf.Len())
}

// File ≥ 1 MB tanpa LUT harus dikirim APA ADANYA (path asli, tanpa file sementara).
func TestHiresBigPhotoNoLUTUnchanged(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "besar.jpg")
	sz := writeJPEG(t, p, noiseImage(1400, 1000, 1), 92)
	if sz < hiresMinBytes {
		t.Skipf("gambar uji hanya %d byte, perlu ≥1MB", sz)
	}
	f, cleanup, note, err := prepareHiresPhoto(p, Hires{})
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != p || note != "" || f.Size != sz {
		t.Fatalf("harus apa adanya: path=%q note=%q size=%d (asli %d)", f.Path, note, f.Size, sz)
	}
}

// File < 1 MB diperbesar ke ±2 MB lewat file sementara; file asli tidak disentuh; cleanup menghapus.
func TestHiresSmallPhotoEnlarged(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "kecil.jpg")
	sz := writeJPEG(t, p, noiseImage(400, 300, 2), 85)
	if sz >= hiresMinBytes {
		t.Fatalf("gambar uji harus <1MB, dapat %d", sz)
	}
	origBefore, _ := os.ReadFile(p)

	f, cleanup, note, err := prepareHiresPhoto(p, Hires{})
	if err != nil {
		t.Fatal(err)
	}
	if f.Path == p {
		t.Fatal("harus menulis file sementara, bukan memakai file asli")
	}
	if f.Size < hiresMinBytes {
		t.Fatalf("hasil %d byte, harus ≥1MB", f.Size)
	}
	if f.Size > hiresTargetBytes*3/2 {
		t.Fatalf("hasil %d byte terlalu jauh dari sasaran ~2MB", f.Size)
	}
	if f.Extension != "jpeg" || f.Mime != "image/jpeg" {
		t.Fatalf("ext/mime salah: %s %s", f.Extension, f.Mime)
	}
	if note == "" {
		t.Error("catatan pengolahan kosong")
	}
	st, err := os.Stat(f.Path)
	if err != nil || st.Size() != f.Size {
		t.Fatalf("file sementara tidak sesuai: %v", err)
	}
	if f.Width <= 400 || f.Height <= 300 {
		t.Fatalf("resolusi harus naik, dapat %dx%d", f.Width, f.Height)
	}
	cleanup()
	if _, err := os.Stat(f.Path); !os.IsNotExist(err) {
		t.Error("cleanup tidak menghapus file sementara")
	}
	origAfter, _ := os.ReadFile(p)
	if !bytes.Equal(origBefore, origAfter) {
		t.Fatal("file asli ikut berubah!")
	}
}

// LUT hi-res: file besar tetap diolah (LUT dipakai), dan warnanya benar-benar berubah.
// Foto uji ber-noise (seperti foto sungguhan) supaya hasilnya ≥1MB dan tidak ikut diperbesar;
// gambar datar satu warna terkompresi nyaris nol sehingga memang tak bisa mencapai 1MB.
func TestHiresLUTApplied(t *testing.T) {
	dir := t.TempDir()
	cubePath := filepath.Join(dir, "invert.cube")
	if err := os.WriteFile(cubePath, []byte(cube(invert)), 0o644); err != nil {
		t.Fatal(err)
	}
	src := noiseImage(1400, 1000, 5)
	p := filepath.Join(dir, "foto.jpg")
	writeJPEG(t, p, src, 95)

	f, cleanup, note, err := prepareHiresPhoto(p, Hires{LUT: cubePath, LUTStrength: 100})
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if f.Path == p {
		t.Fatal("LUT aktif tapi file asli yang dikirim")
	}
	if note == "" {
		t.Error("catatan LUT kosong")
	}
	if f.Width != 1400 || f.Height != 1000 {
		t.Fatalf("LUT tidak boleh mengubah resolusi, dapat %dx%d", f.Width, f.Height)
	}
	raw, _ := os.ReadFile(f.Path)
	out, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("hasil bukan JPEG valid: %v", err)
	}
	// invert → rata-rata tiap kanal ≈ 255 - rata-rata sumber
	var sumIn, sumOut float64
	n := 0
	for y := 0; y < 1000; y += 7 {
		for x := 0; x < 1400; x += 7 {
			sumIn += float64(src.RGBAAt(x, y).R)
			r, _, _, _ := out.At(x, y).RGBA()
			sumOut += float64(r >> 8)
			n++
		}
	}
	mIn, mOut := sumIn/float64(n), sumOut/float64(n)
	if d := mOut - (255 - mIn); d > 4 || d < -4 {
		t.Fatalf("rata-rata R sumber %.1f → hasil %.1f, mau ≈%.1f", mIn, mOut, 255-mIn)
	}
}

// LUT rusak/hilang → ERROR jelas, bukan diam-diam mengirim file tanpa LUT.
func TestHiresBrokenLUTIsError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jpg")
	writeJPEG(t, p, noiseImage(200, 100, 3), 90)
	_, cleanup, _, err := prepareHiresPhoto(p, Hires{LUT: filepath.Join(dir, "tidak-ada.cube"), LUTStrength: 100})
	defer cleanup()
	if err == nil {
		t.Fatal("LUT hilang harus menghasilkan error")
	}
}

// Foto portrait (EXIF orientasi 6) harus keluar berdiri — re-encode membuang EXIF.
func TestHiresPreservesOrientation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "portrait.jpg")
	raw := jpegBytesWithOrientation(t, noiseImage(400, 300, 4), 6, false) // piksel 400×300, tag minta diputar
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	f, cleanup, _, err := prepareHiresPhoto(p, Hires{})
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if f.Path == p {
		t.Fatal("foto <1MB harus diolah")
	}
	if f.Height <= f.Width {
		t.Fatalf("orientasi 6 harus jadi portrait, dapat %dx%d", f.Width, f.Height)
	}
}

// PNG kecil (<1MB) juga diperbesar, dan keluarannya JPEG bertipe image/jpeg.
// (Format selain JPEG/PNG sudah ditolak resendFileInfo sejak awal — sama dengan MAS EDY,
// "File bukan JPG/PNG yang bisa dibaca".)
func TestHiresSmallPNGEnlargedToJPEG(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "kecil.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, noiseImage(300, 200, 6)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if buf.Len() >= hiresMinBytes {
		t.Fatalf("PNG uji harus <1MB, dapat %d", buf.Len())
	}
	f, cleanup, _, err := prepareHiresPhoto(p, Hires{})
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if f.Path == p || f.Mime != "image/jpeg" || f.Extension != "jpeg" || f.Size < hiresMinBytes {
		t.Fatalf("PNG kecil harus jadi JPEG ≥1MB: %+v", f)
	}
}

func TestHiresLUTStrengthClamped(t *testing.T) {
	c, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := c.Set(func(s *Settings) { s.Hires.LUTStrength = 900 })
	if s.Hires.LUTStrength != 100 {
		t.Fatalf("kekuatan 900 harus di-clamp ke 100, dapat %d", s.Hires.LUTStrength)
	}
	s = c.Set(func(s *Settings) { s.Hires.LUTStrength = -5 })
	if s.Hires.LUTStrength != 0 {
		t.Fatalf("kekuatan -5 harus di-clamp ke 0, dapat %d", s.Hires.LUTStrength)
	}
}

// Jalur unggah biasa (Prepare) juga harus menjaga orientasi: JPEG 3000×2000 ber-tag 6
// harus keluar portrait 960×1440, bukan landscape terbaring.
func TestPrepareKeepsPortraitOrientation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "portrait.jpg")
	if err := os.WriteFile(p, jpegBytesWithOrientation(t, noiseImage(3000, 2000, 9), 6, false), 0o644); err != nil {
		t.Fatal(err)
	}
	pr, err := NewProcessor(dir).Prepare(p, Compress{Size: 1440}, LUT{}, Watermark{})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(pr.Buffer))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 960 || cfg.Height != 1440 {
		t.Fatalf("hasil %dx%d, mau 960x1440 (portrait)", cfg.Width, cfg.Height)
	}
	if pr.Width != 960 || pr.Height != 1440 {
		t.Fatalf("Prepared melaporkan %dx%d, mau 960x1440", pr.Width, pr.Height)
	}
	// foto tanpa tag tetap landscape
	q := filepath.Join(dir, "lanskap.jpg")
	writeJPEG(t, q, noiseImage(3000, 2000, 10), 90)
	pr, err = NewProcessor(dir).Prepare(q, Compress{Size: 1440}, LUT{}, Watermark{})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Width != 1440 || pr.Height != 960 {
		t.Fatalf("tanpa tag harus 1440x960, dapat %dx%d", pr.Width, pr.Height)
	}
}
