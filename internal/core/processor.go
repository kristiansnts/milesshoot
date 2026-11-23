package core

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Olah foto sebelum diunggah: resize ke sisi terpanjang cfg.Compress.Size, encode JPEG.
// Port ringkas dari processor.js (LUT & watermark menyusul — TODO). Hanya JPEG/PNG yang di-resize;
// format lain dikirim apa adanya.
type Prepared struct {
	Buffer     []byte
	Width      int
	Height     int
	BytesIn    int64
	BytesOut   int64
	OriginalAt time.Time
	Compressed bool
	LUTApplied bool
}

type Processor struct {
	thumbDir string
}

func NewProcessor(dataDir string) *Processor {
	return &Processor{thumbDir: filepath.Join(dataDir, "thumbnails")}
}

var jpegExt = map[string]bool{".jpg": true, ".jpeg": true}

func (p *Processor) Prepare(path string, compress Compress, lut LUT, wm Watermark) (Prepared, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Prepared{}, &UploadError{Msg: "File asli sudah tidak ada", Permanent: true, Missing: true}
	}
	in := int64(len(raw))
	mtime := FileDate(path) // tanggal jepret (EXIF) bila ada, kalau tidak waktu file

	ext := strings.ToLower(filepath.Ext(path))
	// Format yang tak bisa kita decode (heic/tiff/webp/raw) dikirim apa adanya untuk sekarang.
	if !jpegExt[ext] && ext != ".png" {
		w, h := jpegDims(raw)
		return Prepared{Buffer: raw, Width: w, Height: h, BytesIn: in, BytesOut: in, OriginalAt: mtime}, nil
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return Prepared{Buffer: raw, BytesIn: in, BytesOut: in, OriginalAt: mtime}, nil
	}
	// Re-encode membuang EXIF, jadi orientasi (tag 0x0112) harus dipanggang ke piksel —
	// kalau tidak, foto portrait dari kamera terunggah terbaring.
	if jpegExt[ext] {
		if o := exifOrientation(raw); o > 1 {
			img = orientRGBA(img, o)
		}
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	max := compress.Size
	if max != 4096 {
		max = 1440
	}

	compressed := false
	out := img
	nw, nh := w, h
	if w > max || h > max {
		if w >= h {
			nw, nh = max, h*max/w
		} else {
			nw, nh = w*max/h, max
		}
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
		out = dst
		compressed = true
	}

	// LUT (.cube): terapkan ke piksel RGBA sebelum encode.
	lutApplied := false
	if lut.Enabled && lut.File != "" {
		rgba, ok := out.(*image.RGBA)
		if !ok {
			rgba = image.NewRGBA(out.Bounds())
			draw.Draw(rgba, rgba.Bounds(), out, out.Bounds().Min, draw.Src)
			out = rgba
		}
		if l, lerr := loadLUT(lut.File); lerr == nil {
			applyLUT(rgba.Pix, l, float64(lut.Strength)/100)
			lutApplied = true
			compressed = true // hasil berubah → kirim hasil olah, bukan file asli
		}
	}

	// Watermark: teks atau logo, digambar setelah resize+LUT supaya posisi/ukuran konsisten.
	if wm.Enabled {
		rgba, ok := out.(*image.RGBA)
		if !ok {
			rgba = image.NewRGBA(out.Bounds())
			draw.Draw(rgba, rgba.Bounds(), out, out.Bounds().Min, draw.Src)
			out = rgba
		}
		if applyWatermark(rgba, wm) {
			compressed = true // hasil berubah → kirim hasil olah, bukan file asli
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 90}); err != nil {
		return Prepared{}, &UploadError{Msg: "gagal mengolah foto: " + err.Error(), Local: true}
	}
	// Bila hasil JPEG malah lebih besar dari aslinya dan tak di-resize/LUT, kirim aslinya.
	if !compressed && ext != ".png" && int64(buf.Len()) >= in {
		return Prepared{Buffer: raw, Width: w, Height: h, BytesIn: in, BytesOut: in, OriginalAt: mtime}, nil
	}
	return Prepared{
		Buffer: buf.Bytes(), Width: nw, Height: nh, BytesIn: in, BytesOut: int64(buf.Len()),
		OriginalAt: mtime, Compressed: compressed || ext == ".png", LUTApplied: lutApplied,
	}, nil
}

func (p *Processor) Available() bool { return true }

// GradeJPEG menerapkan LUT video ke satu frame JPEG (dipakai video.Prepare lewat
// VideoPrepareFn — exported karena internal/video tak bisa memakai loadLUT/applyLUT
// yang tak diekspor). Buffer apa adanya dikembalikan bila LUT mati/gagal dimuat —
// jangan sampai satu frame rusak menggagalkan seluruh unggahan video.
func GradeJPEG(buf []byte, lut LUT) []byte {
	if !lut.Enabled || lut.File == "" {
		return buf
	}
	img, _, err := image.Decode(bytes.NewReader(buf))
	if err != nil {
		return buf
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(img.Bounds())
		draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	}
	l, lerr := loadLUT(lut.File)
	if lerr != nil {
		return buf
	}
	applyLUT(rgba.Pix, l, float64(lut.Strength)/100)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, rgba, &jpeg.Options{Quality: 90}); err != nil {
		return buf
	}
	return out.Bytes()
}

// SaveThumb menyimpan pratinjau kecil (md5 sebagai nama) dari buffer hasil olah.
func (p *Processor) SaveThumb(md5hex string, src []byte) bool {
	if len(src) == 0 {
		return false
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return false
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	const tmax = 240
	nw, nh := w, h
	if w >= h {
		nw, nh = tmax, h*tmax/w
	} else {
		nw, nh = w*tmax/h, tmax
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	_ = os.MkdirAll(p.thumbDir, 0o755)
	f, err := os.Create(p.ThumbPath(md5hex))
	if err != nil {
		return false
	}
	defer f.Close()
	return jpeg.Encode(f, dst, &jpeg.Options{Quality: 70}) == nil
}

func (p *Processor) ThumbPath(md5hex string) string {
	clean := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			return r
		}
		return -1
	}, md5hex)
	return filepath.Join(p.thumbDir, clean+".jpg")
}

func fileMTime(path string) time.Time {
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime()
	}
	return time.Now()
}

// jpegDims membaca lebar/tinggi tanpa men-decode seluruh gambar (cukup header).
func jpegDims(raw []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// MD5Hex: hash isi buffer (dipakai bila md5 belum dihitung).
func MD5Hex(b []byte) string {
	h := md5.Sum(b)
	return hex.EncodeToString(h[:])
}

// applyWatermark menggambar teks atau logo ke pojok/tengah foto. Mengembalikan
// true bila sesuatu benar-benar digambar (supaya caller tahu hasil berubah).
func applyWatermark(dst *image.RGBA, wm Watermark) bool {
	switch wm.Type {
	case "logo":
		return drawLogoWatermark(dst, wm)
	default:
		return drawTextWatermark(dst, wm)
	}
}

func drawTextWatermark(dst *image.RGBA, wm Watermark) bool {
	text := strings.TrimSpace(wm.Text)
	if text == "" {
		return false
	}
	// basicfont.Face7x13 selalu 7x13px per karakter — gambar dulu di kanvas native,
	// baru skala ke ukuran target (persen lebar foto) supaya tulisan tajam di segala ukuran.
	const cw, ch = 7, 13
	pad := 6
	tw, th := cw*len(text)+pad*2, ch+pad*2
	tile := image.NewRGBA(image.Rect(0, 0, tw, th))
	d := &font.Drawer{
		Dst:  tile,
		Src:  image.NewUniform(color.White),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(pad, th-pad-3),
	}
	d.DrawString(text)

	targetW := dst.Bounds().Dx() * clamp(wm.Size, 5, 60) / 100
	if targetW < 20 {
		targetW = 20
	}
	targetH := th * targetW / tw
	if targetH < 1 {
		targetH = 1
	}
	scaled := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), tile, tile.Bounds(), xdraw.Over, nil)
	drawWatermarkTile(dst, scaled, wm.Pos, wm.Opacity)
	return true
}

func drawLogoWatermark(dst *image.RGBA, wm Watermark) bool {
	if wm.LogoPath == "" {
		return false
	}
	raw, err := os.ReadFile(wm.LogoPath)
	if err != nil {
		return false
	}
	logo, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return false
	}
	lb := logo.Bounds()
	targetW := dst.Bounds().Dx() * clamp(wm.Size, 5, 60) / 100
	if targetW < 10 {
		targetW = 10
	}
	targetH := lb.Dy() * targetW / lb.Dx()
	if targetH < 1 {
		targetH = 1
	}
	scaled := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), logo, lb, xdraw.Over, nil)
	drawWatermarkTile(dst, scaled, wm.Pos, wm.Opacity)
	return true
}

// WatermarkPreviewImage merender foto contoh (gradasi sintetis, bukan foto asli siapa pun)
// dengan watermark diterapkan — dipakai pratinjau hidup di UI (gaya MAS EDY: endpoint
// /api/watermark/preview). wm.Enabled dipaksa true oleh pemanggil sebelum memanggil ini.
func WatermarkPreviewImage(wm Watermark) ([]byte, error) {
	const w, h = 960, 640
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	c1 := [3]float64{0x5a, 0x7d, 0x9a}
	c2 := [3]float64{0x9a, 0x7b, 0x5a}
	c3 := [3]float64{0x3d, 0x5a, 0x3d}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := (float64(x)/w + float64(y)/h) / 2
			var r, g, b float64
			if t < 0.55 {
				tt := t / 0.55
				r, g, b = lerp3(c1, c2, tt)
			} else {
				tt := (t - 0.55) / 0.45
				r, g, b = lerp3(c2, c3, tt)
			}
			img.SetRGBA(x, y, color.RGBA{uint8(r), uint8(g), uint8(b), 255})
		}
	}
	// lingkaran terang lembut, posisi & opasitas meniru contoh pratinjau MAS EDY
	cx, cy, rad := 480, 300, 120
	circ := color.RGBA{0xe8, 0xd2, 0xb0, 0xb3}
	for y := cy - rad; y <= cy+rad; y++ {
		for x := cx - rad; x <= cx+rad; x++ {
			if x < 0 || x >= w || y < 0 || y >= h {
				continue
			}
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= rad*rad {
				blendPixel(img, x, y, circ)
			}
		}
	}

	if wm.Enabled {
		applyWatermark(img, wm)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func lerp3(a, b [3]float64, t float64) (float64, float64, float64) {
	return a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t
}

func blendPixel(img *image.RGBA, x, y int, c color.RGBA) {
	bg := img.RGBAAt(x, y)
	a := float64(c.A) / 255
	blend := func(dst, src uint8) uint8 { return uint8(float64(dst) + (float64(src)-float64(dst))*a) }
	img.SetRGBA(x, y, color.RGBA{blend(bg.R, c.R), blend(bg.G, c.G), blend(bg.B, c.B), 255})
}

// drawWatermarkTile menempelkan tile (teks/logo, sudah RGBA) ke pojok/tengah dst dengan
// opasitas global (persen, 0-100) lewat mask seragam — jadi tidak perlu ubah alpha tile-nya.
func drawWatermarkTile(dst *image.RGBA, tile *image.RGBA, pos string, opacity int) {
	db := dst.Bounds()
	tb := tile.Bounds()
	margin := db.Dx() / 50
	if margin < 10 {
		margin = 10
	}
	var x, y int
	switch pos {
	case "tl":
		x, y = db.Min.X+margin, db.Min.Y+margin
	case "tr":
		x, y = db.Max.X-margin-tb.Dx(), db.Min.Y+margin
	case "bl":
		x, y = db.Min.X+margin, db.Max.Y-margin-tb.Dy()
	case "c":
		x, y = db.Min.X+(db.Dx()-tb.Dx())/2, db.Min.Y+(db.Dy()-tb.Dy())/2
	default: // br
		x, y = db.Max.X-margin-tb.Dx(), db.Max.Y-margin-tb.Dy()
	}
	mask := image.NewUniform(color.Alpha{A: uint8(clamp(opacity, 0, 100) * 255 / 100)})
	draw.DrawMask(dst, image.Rect(x, y, x+tb.Dx(), y+tb.Dy()), tile, tb.Min, mask, image.Point{}, draw.Over)
}
