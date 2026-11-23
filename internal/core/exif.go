package core

import (
	"encoding/binary"
	"io"
	"os"
	"strings"
	"time"
)

// Pembaca EXIF seperlunya: hanya mengambil TANGGAL JEPRET dari JPEG.
//
// Kenapa penting: waktu file (mtime) berubah saat foto disalin dari kartu memori
// atau dikirim kamera lewat FTP — jadi mtime = waktu transfer, bukan waktu jepret.
// Tanggal ini dipakai untuk (1) tanggal yang dikirim ke FotoYu dan (2) saringan tanggal.
//
// Urutan tag yang dicoba: DateTimeOriginal → DateTimeDigitized → DateTime.

const (
	tagDateTime          = 0x0132 // IFD0
	tagExifIFDPointer    = 0x8769 // IFD0 → offset sub-IFD Exif
	tagDateTimeOriginal  = 0x9003 // Exif IFD
	tagDateTimeDigitized = 0x9004 // Exif IFD

	maxExifScan = 256 * 1024 // cukup; APP1 selalu di awal file
)

// ExifDate membaca tanggal jepret dari JPEG. ok=false bila tidak ada/tak terbaca.
func ExifDate(path string) (time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()

	head := make([]byte, 2)
	if _, err := io.ReadFull(f, head); err != nil || head[0] != 0xFF || head[1] != 0xD8 {
		return time.Time{}, false // bukan JPEG
	}

	read := 2
	seg := make([]byte, 4)
	for read < maxExifScan {
		if _, err := io.ReadFull(f, seg[:2]); err != nil {
			return time.Time{}, false
		}
		read += 2
		if seg[0] != 0xFF {
			return time.Time{}, false // rusak / bukan batas segmen
		}
		marker := seg[1]
		// SOS (mulai data gambar) atau EOI → sudah lewat area metadata
		if marker == 0xDA || marker == 0xD9 {
			return time.Time{}, false
		}
		// penanda tanpa panjang
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			continue
		}
		if _, err := io.ReadFull(f, seg[:2]); err != nil {
			return time.Time{}, false
		}
		read += 2
		size := int(binary.BigEndian.Uint16(seg[:2]))
		if size < 2 {
			return time.Time{}, false
		}
		body := make([]byte, size-2)
		if _, err := io.ReadFull(f, body); err != nil {
			return time.Time{}, false
		}
		read += len(body)

		if marker == 0xE1 && len(body) > 6 && string(body[:4]) == "Exif" {
			if t, ok := parseExifTIFF(body[6:]); ok {
				return t, true
			}
			return time.Time{}, false
		}
	}
	return time.Time{}, false
}

func parseExifTIFF(b []byte) (time.Time, bool) {
	if len(b) < 8 {
		return time.Time{}, false
	}
	var bo binary.ByteOrder
	switch string(b[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return time.Time{}, false
	}
	if bo.Uint16(b[2:4]) != 0x2A {
		return time.Time{}, false
	}
	ifd0 := int(bo.Uint32(b[4:8]))

	// DateTimeOriginal / Digitized ada di sub-IFD Exif; DateTime di IFD0.
	if exifOff, ok := ifdLong(b, bo, ifd0, tagExifIFDPointer); ok {
		for _, tag := range []uint16{tagDateTimeOriginal, tagDateTimeDigitized} {
			if s, ok := ifdASCII(b, bo, exifOff, tag); ok {
				if t, ok := parseExifTime(s); ok {
					return t, true
				}
			}
		}
	}
	if s, ok := ifdASCII(b, bo, ifd0, tagDateTime); ok {
		if t, ok := parseExifTime(s); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

// entry mencari satu tag di sebuah IFD dan mengembalikan (type, count, nilai/offset mentah).
func entry(b []byte, bo binary.ByteOrder, ifd int, tag uint16) (typ uint16, count int, raw []byte, ok bool) {
	if ifd < 0 || ifd+2 > len(b) {
		return 0, 0, nil, false
	}
	n := int(bo.Uint16(b[ifd : ifd+2]))
	if n < 0 || n > 4096 {
		return 0, 0, nil, false
	}
	for i := 0; i < n; i++ {
		off := ifd + 2 + i*12
		if off+12 > len(b) {
			return 0, 0, nil, false
		}
		if bo.Uint16(b[off:off+2]) != tag {
			continue
		}
		return bo.Uint16(b[off+2 : off+4]), int(bo.Uint32(b[off+4 : off+8])), b[off+8 : off+12], true
	}
	return 0, 0, nil, false
}

func ifdLong(b []byte, bo binary.ByteOrder, ifd int, tag uint16) (int, bool) {
	typ, _, raw, ok := entry(b, bo, ifd, tag)
	if !ok || (typ != 4 && typ != 3) {
		return 0, false
	}
	if typ == 3 {
		return int(bo.Uint16(raw[:2])), true
	}
	return int(bo.Uint32(raw)), true
}

func ifdASCII(b []byte, bo binary.ByteOrder, ifd int, tag uint16) (string, bool) {
	typ, count, raw, ok := entry(b, bo, ifd, tag)
	if !ok || typ != 2 || count <= 0 || count > 256 {
		return "", false
	}
	var s []byte
	if count <= 4 { // nilai pendek disimpan langsung di kolom nilai
		s = raw[:count]
	} else {
		off := int(bo.Uint32(raw))
		if off < 0 || off+count > len(b) {
			return "", false
		}
		s = b[off : off+count]
	}
	return strings.TrimRight(string(s), "\x00 "), true
}

// Format EXIF: "YYYY:MM:DD HH:MM:SS" (waktu lokal kamera, tanpa zona).
func parseExifTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "0000") {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006:01:02 15:04:05", "2006:01:02 15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil && t.Year() > 1900 {
			return t, true
		}
	}
	return time.Time{}, false
}

// FileDate: tanggal jepret bila ada di EXIF, kalau tidak pakai waktu file.
// Inilah satu-satunya sumber tanggal yang dipakai processor & saringan tanggal.
func FileDate(path string) time.Time {
	if t, ok := ExifDate(path); ok {
		return t
	}
	return fileMTime(path)
}
