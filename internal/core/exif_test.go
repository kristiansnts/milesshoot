package core

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Menyusun segmen APP1 EXIF berisi satu tag tanggal, lalu menyisipkannya ke JPEG asli.
// bigEndian=true memakai susunan "MM" (kamera Nikon/Canon ada yang pakai ini).
func jpegWithExif(t *testing.T, path, dateStr string, tag uint16, bigEndian bool) {
	t.Helper()

	var bo binary.ByteOrder = binary.LittleEndian
	order := []byte("II")
	if bigEndian {
		bo = binary.BigEndian
		order = []byte("MM")
	}
	val := append([]byte(dateStr), 0) // ASCII diakhiri NUL
	n := len(val)

	tiff := make([]byte, 44+n)
	copy(tiff[0:2], order)
	bo.PutUint16(tiff[2:4], 0x2A)
	bo.PutUint32(tiff[4:8], 8) // IFD0 mulai di offset 8

	// IFD0: satu entri = penunjuk ke sub-IFD Exif (offset 26)
	bo.PutUint16(tiff[8:10], 1)
	bo.PutUint16(tiff[10:12], tagExifIFDPointer)
	bo.PutUint16(tiff[12:14], 4) // LONG
	bo.PutUint32(tiff[14:18], 1)
	bo.PutUint32(tiff[18:22], 26)
	bo.PutUint32(tiff[22:26], 0) // tidak ada IFD berikutnya

	// sub-IFD Exif: satu entri = tag tanggal, nilainya di offset 44
	bo.PutUint16(tiff[26:28], 1)
	bo.PutUint16(tiff[28:30], tag)
	bo.PutUint16(tiff[30:32], 2) // ASCII
	bo.PutUint32(tiff[32:36], uint32(n))
	bo.PutUint32(tiff[36:40], 44)
	bo.PutUint32(tiff[40:44], 0)
	copy(tiff[44:], val)

	payload := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:4], uint16(len(payload)+2))
	app1 = append(app1, payload...)

	// JPEG asli, lalu sisipkan APP1 tepat setelah SOI
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 6), uint8(y * 8), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	out := append([]byte{}, raw[:2]...) // SOI
	out = append(out, app1...)
	out = append(out, raw[2:]...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExifDate(t *testing.T) {
	dir := t.TempDir()
	want := time.Date(2024, 3, 15, 10, 30, 0, 0, time.Local)

	cases := []struct {
		name      string
		tag       uint16
		bigEndian bool
	}{
		{"DateTimeOriginal (II)", tagDateTimeOriginal, false},
		{"DateTimeOriginal (MM)", tagDateTimeOriginal, true},
		{"DateTimeDigitized", tagDateTimeDigitized, false},
	}
	for _, c := range cases {
		p := filepath.Join(dir, c.name+".jpg")
		jpegWithExif(t, p, "2024:03:15 10:30:00", c.tag, c.bigEndian)
		got, ok := ExifDate(p)
		if !ok {
			t.Errorf("%s: EXIF tidak terbaca", c.name)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("%s: dapat %v, mau %v", c.name, got, want)
		}
	}
}

// Tanpa EXIF → jatuh ke waktu file, bukan error.
func TestFileDateFallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "polos.jpg")
	writeTestJPEG(t, p, 3)
	if _, ok := ExifDate(p); ok {
		t.Fatal("file tanpa EXIF seharusnya tidak melaporkan tanggal")
	}
	mt := time.Date(2022, 1, 2, 3, 4, 5, 0, time.Local)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	if got := FileDate(p); !got.Equal(mt) {
		t.Fatalf("fallback mtime salah: dapat %v, mau %v", got, mt)
	}
}

// EXIF harus MENANG atas mtime — ini inti perbaikannya.
func TestExifBeatsMtime(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "disalin.jpg")
	jpegWithExif(t, p, "2024:03:15 10:30:00", tagDateTimeOriginal, false)
	// seolah-olah baru disalin dari kartu: mtime = sekarang
	now := time.Now()
	if err := os.Chtimes(p, now, now); err != nil {
		t.Fatal(err)
	}
	got := FileDate(p)
	if got.Year() != 2024 || got.Month() != 3 || got.Day() != 15 {
		t.Fatalf("FileDate harus pakai tanggal jepret EXIF, dapat %v", got)
	}
}
