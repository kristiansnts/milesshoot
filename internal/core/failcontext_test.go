package core

import (
	"strings"
	"testing"
)

// Satu baris log gagal harus cukup untuk memahami masalahnya: kode HTTP, tahap, percobaan,
// ukuran, balasan FotoYu — dan tidak boleh membocorkan apa pun yang rahasia.
func TestFailContext(t *testing.T) {
	job := Record{MD5: "0123456789abcdef", Stage: "mengunggah", Size: 2048 * 1024, Kind: "photo", Destination: "fotoyu"}
	ue := &UploadError{Msg: "ditolak", Status: 422, Detail: strings.Repeat("x", 600), Permanent: true, Duplicate: "own"}
	got := failContext(job, ue, 3, 5)
	for _, want := range []string{"tahap=mengunggah", "http=422", "percobaan=3/6", "ukuran=2048KB", "jenis=photo", "tujuan=fotoyu", "id=01234567", "permanen", "dobel=own", "fotoyu="} {
		if !strings.Contains(got, want) {
			t.Errorf("konteks tidak memuat %q: %s", want, got)
		}
	}
	if strings.Contains(got, strings.Repeat("x", 450)) {
		t.Error("detail FotoYu harus dipotong ±400 karakter")
	}
	// tanpa UploadError (galat biasa) tetap tidak panik
	if s := failContext(Record{}, nil, 1, 0); !strings.Contains(s, "tahap=-") {
		t.Errorf("galat polos: %s", s)
	}
}
