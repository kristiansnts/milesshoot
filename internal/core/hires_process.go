package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strings"

	"fotonaik/internal/fotoyu"

	xdraw "golang.org/x/image/draw"
)

// Pengolahan foto hi-res sebelum dikirim ke FotoYu. Port dari hires.js (MAS EDY):
//
//  1. LUT hi-res (Hires.LUT + LUTStrength) → render resolusi penuh, JPEG q95.
//  2. Foto di bawah 1 MB diperbesar sampai ±2 MB (resolusi dinaikkan, kualitas JPEG tinggi),
//     karena FotoYu menolak/menganggap hi-res yang terlalu kecil. File di disk TIDAK diubah —
//     hasilnya ditulis ke file sementara yang dihapus setelah terkirim.
//
// Bila tak ada LUT dan ukuran sudah ≥ 1 MB, file asli dikirim apa adanya (jalur lama).
const (
	hiresMinBytes    = 1 << 20 // di bawah ini diperbesar
	hiresTargetBytes = 2 << 20 // sasaran ukuran setelah diperbesar
	hiresMaxSide     = 6000    // batas sisi terpanjang saat memperbesar
	hiresLUTQuality  = 95
)

// prepareHiresPhoto menyiapkan file foto untuk dikirim. cleanup WAJIB dipanggil (aman bila no-op).
// note berisi keterangan pengolahan untuk log ("" bila file dikirim apa adanya).
func prepareHiresPhoto(path string, hc Hires) (f fotoyu.ResendFile, cleanup func(), note string, err error) {
	cleanup = func() {}
	f, err = resendFileInfo(path, "photo")
	if err != nil {
		return f, cleanup, "", err
	}
	lutFile := strings.TrimSpace(hc.LUT)
	if lutFile == "" && f.Size >= hiresMinBytes {
		return f, cleanup, "", nil
	}
	// Hanya JPEG/PNG yang bisa kita decode; format lain (HEIC/TIFF/WebP) dikirim apa adanya.
	ext := strings.ToLower(filepath.Ext(path))
	if !jpegExt[ext] && ext != ".png" {
		return f, cleanup, "", nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return f, cleanup, "", fmt.Errorf("file asli tidak ditemukan")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return f, cleanup, "", fmt.Errorf("foto tidak bisa dibaca: %v", err)
	}
	// Re-encode membuang EXIF, jadi orientasi harus "dipanggang" ke piksel — kalau tidak,
	// foto portrait terkirim terbaring.
	orient := 1
	if jpegExt[ext] {
		orient = exifOrientation(raw)
	}
	rgba := orientRGBA(img, orient)
	raw, img = nil, nil

	var out []byte
	var notes []string
	curSize := f.Size

	if lutFile != "" {
		l, lerr := loadLUT(lutFile)
		if lerr != nil {
			return f, cleanup, "", fmt.Errorf("LUT hi-res tidak bisa dimuat: %v", lerr)
		}
		applyLUT(rgba.Pix, l, float64(hc.LUTStrength)/100)
		var b bytes.Buffer
		if e := jpeg.Encode(&b, rgba, &jpeg.Options{Quality: hiresLUTQuality}); e != nil {
			return f, cleanup, "", fmt.Errorf("gagal menyimpan hasil LUT: %v", e)
		}
		out = b.Bytes()
		curSize = int64(len(out))
		notes = append(notes, "LUT "+strings.TrimSuffix(filepath.Base(lutFile), filepath.Ext(lutFile)))
	}

	if curSize < hiresMinBytes {
		big, e := enlargeToTarget(rgba, curSize)
		if e != nil {
			return f, cleanup, "", e
		}
		out = big
		notes = append(notes, fmt.Sprintf("diperbesar %.2f → %.2f MB",
			float64(f.Size)/(1<<20), float64(len(big))/(1<<20)))
	}
	if out == nil { // tak mungkin sampai sini, tapi jangan pernah kirim buffer kosong
		return f, cleanup, "", nil
	}

	tmp, err := os.CreateTemp("", "fotonaik-hires-*.jpg")
	if err != nil {
		return f, cleanup, "", fmt.Errorf("gagal membuat file sementara: %v", err)
	}
	cleanup = func() { os.Remove(tmp.Name()) }
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		cleanup()
		return f, func() {}, "", fmt.Errorf("gagal menulis file sementara: %v", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return f, func() {}, "", fmt.Errorf("gagal menulis file sementara: %v", err)
	}

	w, h := jpegDims(out)
	if w == 0 || h == 0 {
		w, h = rgba.Bounds().Dx(), rgba.Bounds().Dy()
	}
	return fotoyu.ResendFile{
		Path: tmp.Name(), Size: int64(len(out)), Width: w, Height: h,
		Extension: "jpeg", Mime: "image/jpeg",
	}, cleanup, strings.Join(notes, " · "), nil
}

// enlargeToTarget menaikkan resolusi src sampai hasil JPEG ±hiresTargetBytes (rentang
// 0.9×–1.25× sasaran), min hiresMinBytes. Port dari fungsi Q() di hires.js.
func enlargeToTarget(src *image.RGBA, curSize int64) ([]byte, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("foto kosong")
	}
	target := float64(hiresTargetBytes)
	maxScale := math.Max(1.01, hiresMaxSide/float64(max(w, h)))
	scale := math.Min(maxScale, math.Max(1.05, math.Sqrt(target/math.Max(1, float64(curSize)))*1.05))
	q := 95

	var best []byte
	for iter := 0; iter < 10; iter++ {
		nw, nh := int(math.Round(float64(w)*scale)), int(math.Round(float64(h)*scale))
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: q}); err != nil {
			return nil, fmt.Errorf("gagal memperbesar foto: %v", err)
		}
		n := float64(buf.Len())
		if buf.Len() >= hiresMinBytes {
			best = buf.Bytes()
		}
		if n >= target*0.9 && n <= target*1.25 {
			break
		}
		if n > target*1.25 {
			if scale <= 1.02 { // sudah paling kecil, turunkan kualitas
				q -= 5
				if q < 80 {
					break
				}
				continue
			}
			scale = math.Max(1.01, scale*math.Sqrt(target/n))
		} else if scale >= maxScale-1e-6 { // sudah mentok ukuran, naikkan kualitas
			if q >= 100 || n >= target*0.75 {
				break
			}
			q = min(100, q+2)
		} else {
			scale = math.Min(maxScale, scale*math.Sqrt(target/n)*1.02)
		}
	}
	if best == nil {
		return nil, fmt.Errorf("foto tidak bisa diperbesar sampai 1 MB")
	}
	return best, nil
}

// exifOrientation membaca tag Orientation (0x0112) dari JPEG di memori. 1 bila tidak ada.
func exifOrientation(raw []byte) int {
	if len(raw) < 4 || raw[0] != 0xFF || raw[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(raw) && i < maxExifScan {
		if raw[i] != 0xFF {
			return 1
		}
		m := raw[i+1]
		if m == 0xDA || m == 0xD9 {
			return 1
		}
		if m == 0x01 || (m >= 0xD0 && m <= 0xD7) {
			i += 2
			continue
		}
		size := int(binary.BigEndian.Uint16(raw[i+2 : i+4]))
		if size < 2 || i+2+size > len(raw) {
			return 1
		}
		body := raw[i+4 : i+2+size]
		if m == 0xE1 && len(body) > 6 && string(body[:4]) == "Exif" {
			return tiffOrientation(body[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(b []byte) int {
	if len(b) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(b[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(b[2:4]) != 0x2A {
		return 1
	}
	typ, cnt, raw, ok := entry(b, bo, int(bo.Uint32(b[4:8])), 0x0112)
	if !ok || typ != 3 || cnt != 1 {
		return 1
	}
	if v := int(bo.Uint16(raw[:2])); v >= 1 && v <= 8 {
		return v
	}
	return 1
}

// orientRGBA mengubah img ke RGBA dan memutar/membalik sesuai nilai EXIF Orientation (1–8).
func orientRGBA(img image.Image, o int) *image.RGBA {
	b := img.Bounds()
	src, ok := img.(*image.RGBA)
	if !ok || src.Stride != b.Dx()*4 || b.Min != (image.Point{}) {
		src = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	}
	if o <= 1 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			si := (sy*w + sx) * 4
			di := (y*dw + x) * 4
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
