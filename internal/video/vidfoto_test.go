package video

import (
	"image"
	_ "image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"milesshoot/internal/core"
)

func TestCropRect(t *testing.T) {
	cases := []struct{ w, h, cw, ch int }{
		{1920, 1080, 720, 1080},  // lebar → tengah, 2:3
		{1080, 1920, 1080, 1620}, // tegak 9:16 → tinggi dipotong
		{1200, 1800, 1200, 1800}, // sudah 2:3
	}
	for _, c := range cases {
		cw, ch, x, y := cropRect(c.w, c.h)
		if cw != c.cw || ch != c.ch {
			t.Errorf("%dx%d: dapat %dx%d, mau %dx%d", c.w, c.h, cw, ch, c.cw, c.ch)
		}
		if x < 0 || y < 0 || x+cw > c.w || y+ch > c.h {
			t.Errorf("%dx%d: potongan keluar frame (%d,%d %dx%d)", c.w, c.h, x, y, cw, ch)
		}
	}
	// Video 9:16: 70% bagian yang dibuang diambil dari atas.
	_, ch, _, y := cropRect(1080, 1920)
	if want := int(float64(1920-ch) * 0.7); y != want&^1 {
		t.Errorf("offset y = %d, mau %d", y, want&^1)
	}
}

// Frame tajam harus menang atas frame buram; frame gelap kalah dari frame normal.
func TestPickFramesPrefersSharpAndLit(t *testing.T) {
	mk := func(sharp, light float64) frameScore { return frameScore{sharp: sharp, light: light} }
	sc := make([]frameScore, 20)
	for i := range sc {
		sc[i] = mk(10, 1)
		sc[i].thumb[0] = float32(i * 50) // tiap frame berbeda
	}
	sc[3] = mk(100, 1)    // tajam, segmen 1
	sc[13] = mk(100, 0.4) // tajam tapi gelap
	sc[14] = mk(60, 1)    // kurang tajam tapi terang → harus menang di segmennya
	got, _ := pickFrames(sc, 2, 10, false)
	if len(got) != 2 || got[0] != 3 || got[1] != 14 {
		t.Fatalf("terpilih %v, mau [3 14]", got)
	}
}

func TestPickFramesSkipsSimilar(t *testing.T) {
	sc := make([]frameScore, 8) // semua thumbnail sama → hanya 1 yang lolos
	for i := range sc {
		sc[i] = frameScore{sharp: 10, light: 1}
	}
	got, similar := pickFrames(sc, 4, 10, true)
	if len(got) != 1 || similar != 3 {
		t.Fatalf("terpilih %v similar=%d, mau 1 foto & 3 dilewati", got, similar)
	}
	if got, _ := pickFrames(sc, 4, 10, false); len(got) != 4 {
		t.Fatalf("tanpa unique harus 4 foto, dapat %v", got)
	}
}

func TestExifDateRoundTrip(t *testing.T) {
	// JPEG minimal: SOI + APP0 (JFIF) + EOI
	jpg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00, 0xFF, 0xD9}
	want := time.Date(2026, 11, 26, 14, 30, 5, 0, time.Local)
	out := withExifDate(jpg, want)
	f := filepath.Join(t.TempDir(), "a.jpg")
	if err := os.WriteFile(f, out, 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := core.ExifDate(f)
	if !ok || !got.Equal(want) {
		t.Fatalf("EXIF terbaca %v ok=%v, mau %v", got, ok, want)
	}
	if out[2] != 0xFF || out[3] != 0xE0 {
		t.Fatal("APP0 harus tetap di depan")
	}
}

func makeTestVideo(t *testing.T, dir, name string, w, h int, secs int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	cmd := exec.Command(findFfmpeg(), "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size="+strconv.Itoa(w)+"x"+strconv.Itoa(h)+":rate=25",
		"-t", strconv.Itoa(secs), "-pix_fmt", "yuv420p", "-c:v", "libx264", p)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg tidak bisa membuat video uji: %v %s", err, out)
	}
	return p
}

func TestExtractPhotosEndToEnd(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg tidak ada")
	}
	dir := t.TempDir()
	v := makeTestVideo(t, dir, "klip.mp4", 1280, 720, 6)

	res, err := ExtractPhotos(v, VFOptions{Count: 5, Crop23: true, Unique: false}, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 5 {
		t.Fatalf("mau 5 foto, dapat %v", res.Files)
	}
	if res.Dir != filepath.Join(dir, ResultDirName) {
		t.Fatalf("folder hasil salah: %s", res.Dir)
	}
	for _, n := range res.Files {
		f, err := os.Open(filepath.Join(res.Dir, n))
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s bukan JPEG valid: %v", n, err)
		}
		if cfg.Width*3 != cfg.Height*2 {
			t.Errorf("%s: %dx%d bukan 2:3", n, cfg.Width, cfg.Height)
		}
		if _, ok := core.ExifDate(filepath.Join(res.Dir, n)); !ok {
			t.Errorf("%s: EXIF waktu jepret tidak ada", n)
		}
	}
	if res.Files[0] != "klip_01.jpg" || res.Files[4] != "klip_05.jpg" {
		t.Errorf("penamaan: %v", res.Files)
	}

	// Diproses ulang dengan skipDone → dilewati; tanpa skipDone → nomor lanjut, tidak menimpa.
	if r, _ := ExtractPhotos(v, VFOptions{Count: 5}, true, nil, nil); !r.Skipped {
		t.Error("video yang sudah diproses harus dilewati")
	}
	r2, err := ExtractPhotos(v, VFOptions{Count: 3}, false, nil, nil)
	if err != nil || len(r2.Files) != 3 || r2.Files[0] != "klip_06.jpg" {
		t.Fatalf("proses ulang: %v %v", r2.Files, err)
	}
}

func TestExtractPhotosCancel(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg tidak ada")
	}
	v := makeTestVideo(t, t.TempDir(), "x.mp4", 640, 360, 4)
	_, err := ExtractPhotos(v, VFOptions{Count: 5}, false, nil, func() bool { return true })
	if err == nil {
		t.Fatal("harus berhenti saat dibatalkan")
	}
}

func TestFindVideosSkipsResultDir(t *testing.T) {
	d := t.TempDir()
	for _, p := range []string{"a.mp4", "sub/b.MOV", ResultDirName + "/c.mp4", "foto.jpg", ".h.mp4"} {
		full := filepath.Join(d, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte("x"), 0o644)
	}
	got := FindVideos(d)
	if len(got) != 2 {
		t.Fatalf("mau 2 video (a.mp4, sub/b.MOV), dapat %v", got)
	}
}

func jpegSize(t *testing.T, path string) (int, int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}

// Video mendatar + "tegak" → hasil tegak; video yang sudah tegak tidak diputar lagi; tanpa pilihan
// tetap mendatar. Foto kecil ikut diperbesar, jadi yang dicek arah & rasio, bukan ukuran piksel.
func TestExtractPhotosForceVertical(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg tidak ada")
	}
	d := t.TempDir()
	wide := makeTestVideo(t, d, "lebar.mp4", 640, 360, 3)
	tall := makeTestVideo(t, d, "tegak.mp4", 360, 640, 3)

	near := func(a, b float64) bool { return math.Abs(a-b) < 0.01*b }
	check := func(label string, v string, o VFOptions, wantW, wantH float64) {
		t.Helper()
		r, err := ExtractPhotos(v, o, false, nil, nil)
		if err != nil || len(r.Files) == 0 {
			t.Fatalf("%s: %v %v", label, r.Files, err)
		}
		w, h := jpegSize(t, filepath.Join(r.Dir, r.Files[0]))
		if !near(float64(w)/float64(h), wantW/wantH) {
			t.Errorf("%s: rasio %dx%d, mau %v:%v", label, w, h, wantW, wantH)
		}
		_ = os.RemoveAll(r.Dir)
	}
	check("cw", wide, VFOptions{Count: 2, Tegak: "cw"}, 9, 16)
	check("ccw", wide, VFOptions{Count: 2, Tegak: "ccw"}, 9, 16)
	check("sudah tegak tidak diputar", tall, VFOptions{Count: 2, Tegak: "cw"}, 9, 16)
	check("tanpa pilihan tetap mendatar", wide, VFOptions{Count: 2}, 16, 9)
	check("putar lalu potong 2:3", wide, VFOptions{Count: 2, Tegak: "cw", Crop23: true}, 2, 3)
}

// Foto kecil (<1 MB) selalu diperbesar sampai melewati batas hi-res FotoYu.
func TestExtractPhotosAlwaysEnlargesSmall(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg tidak ada")
	}
	v := makeTestVideo(t, t.TempDir(), "kecil.mp4", 640, 360, 3)
	r, err := ExtractPhotos(v, VFOptions{Count: 2, Crop23: true}, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(r.Dir, r.Files[0])
	fi, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.Enlarged != len(r.Files) || fi.Size() < int64(core.MinHiresBytes) {
		t.Fatalf("harus diperbesar >=1 MB: %d byte, enlarged=%d/%d", fi.Size(), r.Enlarged, len(r.Files))
	}
	if w, _ := jpegSize(t, f); w <= 240 {
		t.Errorf("resolusi harus naik dari 240 px, dapat %d", w)
	}
	if _, ok := core.ExifDate(f); !ok {
		t.Error("EXIF waktu jepret harus tetap ada setelah diperbesar")
	}
}

// Proses Video → Foto harus meninggalkan jejak di log: mulai, info video, frame terpilih,
// tiap foto, dan ringkasan akhir; galat juga tercatat dengan nama video.
func TestExtractPhotosLogsTrail(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg tidak ada")
	}
	var lines []string
	old := Logf
	Logf = func(level, msg string) { lines = append(lines, level+" "+msg) }
	defer func() { Logf = old }()

	v := makeTestVideo(t, t.TempDir(), "jejak.mp4", 640, 360, 3)
	if _, err := ExtractPhotos(v, VFOptions{Count: 2, Crop23: true}, false, nil, nil); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(lines, "\n")
	for _, want := range []string{"[VideoFoto] mulai jejak.mp4", "durasi 3.0 dtk", "terpilih di detik", "filter=", "jejak_01.jpg", "diperbesar dari", "selesai dalam"} {
		if !strings.Contains(all, want) {
			t.Errorf("log tidak memuat %q:\n%s", want, all)
		}
	}

	lines = nil
	bad := filepath.Join(t.TempDir(), "rusak.mp4")
	_ = os.WriteFile(bad, []byte("bukan video"), 0o644)
	if _, err := ExtractPhotos(bad, VFOptions{Count: 2}, false, nil, nil); err == nil {
		t.Fatal("file rusak harus galat")
	}
	if all := strings.Join(lines, "\n"); !strings.Contains(all, "error [VideoFoto] rusak.mp4 GAGAL") {
		t.Errorf("galat harus tercatat dengan nama video:\n%s", all)
	}
}
