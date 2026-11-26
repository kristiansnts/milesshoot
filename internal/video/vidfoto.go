package video

// Video → Foto: memilih momen terbaik dari sebuah video pendek lalu menyimpannya sebagai
// foto JPEG resolusi penuh. Pemilihan frame: frame dinilai dari ketajaman (varians Laplacian)
// dan pencahayaan pada versi kecil abu-abu, video dibagi rata jadi N segmen, dan tiap
// segmen diambil frame dengan skor tertinggi (opsional: yang terlalu mirip dengan foto
// yang sudah terpilih dilewati). Butuh ffmpeg.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"milesshoot/internal/core"
)

// ResultDirName: nama folder hasil, dibuat di samping video asli.
const ResultDirName = "Hasil Capture Video"

const (
	analysisSide  = 640  // sisi terpanjang gambar yang dinilai
	maxAnalysis   = 600  // target jumlah frame yang dinilai per video
	trimFrac      = 0.10 // buang 10% awal & akhir (bila videonya cukup panjang)
	similarThresh = 2.0  // selisih rata-rata thumbnail 16x16 di bawah ini = "terlalu mirip"
	cropTopBias   = 0.7  // video tegak: 70% bagian yang dibuang diambil dari atas
	MinCount      = 2
	MaxCount      = 30
)

// VFOptions: pilihan pemakai.
type VFOptions struct {
	Count  int  `json:"count"`  // foto per video
	Crop23 bool `json:"crop23"` // potong 2:3 tegak
	Unique bool `json:"unique"` // lewati momen yang terlalu mirip
	// Tegak: "" = ikut metadata video saja; "cw"/"ccw" = bila gambar masih mendatar setelah itu,
	// putar 90° searah/berlawanan jarum jam supaya hasilnya selalu tegak.
	Tegak string `json:"tegak"`
}

// VFResult: hasil satu video.
type VFResult struct {
	Video    string   `json:"video"`
	Dir      string   `json:"dir"`
	Files    []string `json:"files"`    // nama file hasil
	Enlarged int      `json:"enlarged"` // foto yang diperbesar
	Similar  int      `json:"similar"`  // segmen yang dilewati karena terlalu mirip
	Asked    int      `json:"asked"`
	Skipped  bool     `json:"skipped"` // sudah pernah diproses
}

var errVFCancelled = errors.New("dibatalkan")

// IsVideoFile: ekstensi yang didukung fitur ini.
func IsVideoFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".mov", ".m4v":
		return true
	}
	return false
}

// FindVideos mengumpulkan video di sebuah folder (tanpa masuk ke folder hasil & folder tersembunyi).
func FindVideos(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		n := d.Name()
		if d.IsDir() {
			if p != dir && (n == ResultDirName || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasPrefix(n, ".") && IsVideoFile(n) {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func vfBase(path string) string {
	b := filepath.Base(path)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// vfAlreadyDone: ada foto hasil bernama <video>_NN.jpg di folder hasil.
func vfAlreadyDone(path string) bool {
	m, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ResultDirName, globEscape(vfBase(path))+"_??.jpg"))
	return len(m) > 0
}

func globEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`).Replace(s)
}

// ExtractPhotos memilih & menyimpan foto dari satu video. onStage dipanggil dengan teks
// kemajuan; cancelled (boleh nil) dicek berkala. skipDone=true melewati video yang sudah diproses.
func ExtractPhotos(path string, o VFOptions, skipDone bool, onStage func(string), cancelled func() bool) (VFResult, error) {
	t0 := time.Now()
	name := filepath.Base(path)
	Logf("info", fmt.Sprintf("[VideoFoto] mulai %s · foto=%d crop23=%v unik=%v tegak=%q skipDone=%v",
		name, o.Count, o.Crop23, o.Unique, o.Tegak, skipDone))
	res, err := extractPhotos(path, o, skipDone, onStage, cancelled)
	dur := time.Since(t0).Round(10 * time.Millisecond)
	switch {
	case errors.Is(err, errVFCancelled):
		Logf("warn", fmt.Sprintf("[VideoFoto] %s dibatalkan setelah %s (%d foto sudah tersimpan)", name, dur, len(res.Files)))
	case err != nil:
		Logf("error", fmt.Sprintf("[VideoFoto] %s GAGAL setelah %s: %v", name, dur, err))
	case res.Skipped:
		Logf("info", fmt.Sprintf("[VideoFoto] %s dilewati (sudah pernah diproses)", name))
	default:
		Logf("ok", fmt.Sprintf("[VideoFoto] %s selesai dalam %s: %d/%d foto, %d diperbesar, %d momen dilewati → %s",
			name, dur, len(res.Files), res.Asked, res.Enlarged, res.Similar, res.Dir))
	}
	return res, err
}

func extractPhotos(path string, o VFOptions, skipDone bool, onStage func(string), cancelled func() bool) (VFResult, error) {
	res := VFResult{Video: path}
	if !Available() {
		return res, errors.New("ffmpeg tidak ditemukan di komputer ini")
	}
	if onStage == nil {
		onStage = func(string) {}
	}
	if cancelled == nil {
		cancelled = func() bool { return false }
	}
	o.Count = clampInt(o.Count, MinCount, MaxCount)
	res.Asked = o.Count
	res.Dir = filepath.Join(filepath.Dir(path), ResultDirName)
	if skipDone && vfAlreadyDone(path) {
		res.Skipped = true
		return res, nil
	}

	info, err := probe(path)
	if err != nil {
		return res, err
	}
	if info.duration < 0.3 {
		return res, errors.New("durasi video tidak terbaca atau terlalu pendek")
	}
	rot := info.rotation
	Logf("info", fmt.Sprintf("[VideoFoto] %s: durasi %.1f dtk, tampil %dx%d (rotasi metadata %d°), dibuat %s",
		filepath.Base(path), info.duration, info.width, info.height, rot, func() string {
			if info.createdAt.IsZero() {
				return "tidak tercatat (pakai waktu file)"
			}
			return info.createdAt.Format(time.RFC3339)
		}()))
	// Putar dulu (bila diminta & gambar mendatar), baru potong 2:3 dari gambar yang sudah tegak.
	w, h, pre := info.width, info.height, ""
	if w > h {
		switch o.Tegak {
		case "cw":
			pre, w, h = "transpose=1", h, w
		case "ccw":
			pre, w, h = "transpose=2", h, w
		}
	}
	crop := ""
	if o.Crop23 {
		crop = cropFilter(w, h)
	}

	fps := math.Min(30, math.Max(2, maxAnalysis/info.duration))
	onStage("Menilai gambar video…")
	scores, err := analyzeFrames(path, w, h, pre, crop, fps, func(n int) {
		onStage(fmt.Sprintf("Menilai gambar video… (%d)", n))
	}, cancelled)
	if err != nil {
		return res, err
	}
	picked, similar := pickFrames(scores, o.Count, fps, o.Unique)
	{
		secs := make([]string, len(picked))
		for i, idx := range picked {
			secs[i] = fmt.Sprintf("%.1f", float64(idx)/fps)
		}
		Logf("info", fmt.Sprintf("[VideoFoto] %s: %d frame dinilai (%.1f fps), terpilih di detik [%s], %d momen dilewati (mirip), filter=%s",
			filepath.Base(path), len(scores), fps, strings.Join(secs, " "), similar, vfFilter(pre, crop)))
	}
	if len(picked) == 0 {
		return res, errors.New("tidak ada momen yang layak di video ini")
	}
	res.Similar = similar

	if err := os.MkdirAll(res.Dir, 0o755); err != nil {
		return res, fmt.Errorf("tidak bisa membuat folder hasil: %w", err)
	}
	startAt := info.createdAt
	if startAt.IsZero() {
		if fi, e := os.Stat(path); e == nil {
			startAt = fi.ModTime().Add(-time.Duration(info.duration * float64(time.Second)))
		} else {
			startAt = time.Now()
		}
	}
	base := vfBase(path)
	n := 0
	for _, idx := range picked {
		if cancelled() {
			return res, errVFCancelled
		}
		sec := float64(idx) / fps
		onStage(fmt.Sprintf("Menyimpan foto %d/%d…", len(res.Files)+1, len(picked)))
		jpg, gerr := grabFrameQ(path, sec, vfFilter(pre, crop), false, 2)
		if gerr != nil {
			return res, gerr
		}
		// Foto di bawah 1 MB (batas hi-res FotoYu) selalu diperbesar sampai ±2 MB.
		raw := len(jpg)
		if len(jpg) < core.MinHiresBytes {
			big, eerr := core.EnlargeJPEG(jpg)
			switch {
			case eerr != nil:
				Logf("warn", fmt.Sprintf("[VideoFoto] %s detik %.1f: gagal diperbesar (%v) — dipakai ukuran asli %d KB",
					filepath.Base(path), sec, eerr, raw/1024))
			case len(big) > len(jpg):
				jpg = big
				res.Enlarged++
			}
		}
		jpg = withExifDate(jpg, startAt.Add(time.Duration(sec*float64(time.Second))).Local())
		var name, full string
		for {
			n++
			name = fmt.Sprintf("%s_%02d.jpg", base, n)
			full = filepath.Join(res.Dir, name)
			if _, serr := os.Stat(full); serr != nil {
				break
			}
		}
		if werr := os.WriteFile(full+".tmp", jpg, 0o644); werr != nil {
			return res, werr
		}
		if rerr := os.Rename(full+".tmp", full); rerr != nil {
			return res, rerr
		}
		res.Files = append(res.Files, name)
		Logf("info", fmt.Sprintf("[VideoFoto] %s: detik %.1f → %s (%d KB%s)", filepath.Base(path), sec, name, len(jpg)/1024,
			func() string {
				if len(jpg) > raw+1024 {
					return fmt.Sprintf(", diperbesar dari %d KB", raw/1024)
				}
				return ""
			}()))
	}
	return res, nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func even(n int) int { return n &^ 1 }

// cropRect: potongan 2:3 tegak (w:h = 2:3) dari frame w×h, dalam piksel genap.
// Video lebar: dipotong di tengah. Video tegak yang lebih sempit dari 2:3 (mis. 9:16):
// tinggi dipotong, 70% bagian yang dibuang diambil dari atas.
func cropRect(w, h int) (cw, ch, x, y int) {
	if w*3 >= h*2 { // sama atau lebih lebar dari 2:3 → potong lebar
		ch = even(h)
		cw = even(ch * 2 / 3)
		if cw > w {
			cw = even(w)
		}
		return cw, ch, even((w - cw) / 2), 0
	}
	cw = even(w)
	ch = even(cw * 3 / 2)
	if ch > h {
		ch = even(h)
	}
	return cw, ch, 0, even(int(float64(h-ch) * cropTopBias))
}

func cropFilter(w, h int) string {
	cw, ch, x, y := cropRect(w, h)
	return fmt.Sprintf("crop=%d:%d:%d:%d", cw, ch, x, y)
}

// vfFilter menggabung putaran & potongan jadi satu rantai filter ("null" bila keduanya kosong).
func vfFilter(pre, crop string) string {
	var f []string
	for _, v := range []string{pre, crop} {
		if v != "" {
			f = append(f, v)
		}
	}
	if len(f) == 0 {
		return "null"
	}
	return strings.Join(f, ",")
}

// ---------- penilaian frame ----------

type frameScore struct {
	sharp float64
	light float64
	thumb [256]float32 // 16x16 abu-abu, untuk deteksi momen yang mirip
}

func analyzeFrames(path string, w, h int, pre, crop string, fps float64, progress func(int), cancelled func() bool) ([]frameScore, error) {
	fw, fh := w, h
	if crop != "" {
		fw, fh, _, _ = cropRect(w, h)
	}
	// Ukuran gambar penilaian dihitung sendiri supaya rawvideo punya dimensi pasti.
	aw, ah := fw, fh
	if m := max(fw, fh); m > analysisSide {
		s := float64(analysisSide) / float64(m)
		aw, ah = even(int(float64(fw)*s)), even(int(float64(fh)*s))
	}
	aw, ah = max(aw, 16), max(ah, 16)

	vf := []string{fmt.Sprintf("fps=%.4f", fps)}
	if pre != "" {
		vf = append(vf, pre)
	}
	if crop != "" {
		vf = append(vf, crop)
	}
	vf = append(vf, fmt.Sprintf("scale=%d:%d:flags=area", aw, ah), "format=gray")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, findFfmpeg(), "-hide_banner", "-loglevel", "error", "-i", path,
		"-an", "-sn", "-vf", strings.Join(vf, ","), "-f", "rawvideo", "-pix_fmt", "gray", "pipe:1")
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var scores []frameScore
	buf := make([]byte, aw*ah)
	for {
		if cancelled() {
			cancel()
			_ = cmd.Wait()
			return nil, errVFCancelled
		}
		if _, rerr := io.ReadFull(out, buf); rerr != nil {
			break
		}
		scores = append(scores, scoreGray(buf, aw, ah))
		if len(scores)%50 == 0 {
			progress(len(scores))
		}
	}
	_ = cmd.Wait()
	if len(scores) == 0 {
		msg := strings.TrimSpace(lastLine(errb.String()))
		return nil, fmt.Errorf("video tidak bisa dibaca (rusak / format tidak didukung)%s", func() string {
			if msg != "" {
				return ": " + msg
			}
			return ""
		}())
	}
	return scores, nil
}

// scoreGray: ketajaman = varians Laplacian (bagian tengah lebih berat), pencahayaan = penalti
// untuk frame terlalu gelap/terang atau banyak piksel hitam/putih murni.
func scoreGray(p []byte, w, h int) frameScore {
	var sum, sum2, n float64
	var cSum, cSum2, cN float64
	x0, x1 := w/6, w-w/6
	y0, y1 := h/6, h-h/6
	for y := 1; y < h-1; y++ {
		row := y * w
		inY := y >= y0 && y < y1
		for x := 1; x < w-1; x++ {
			i := row + x
			l := float64(int(p[i-w]) + int(p[i+w]) + int(p[i-1]) + int(p[i+1]) - 4*int(p[i]))
			sum += l
			sum2 += l * l
			n++
			if inY && x >= x0 && x < x1 {
				cSum += l
				cSum2 += l * l
				cN++
			}
		}
	}
	varAll, varCtr := 0.0, 0.0
	if n > 0 {
		varAll = sum2/n - (sum/n)*(sum/n)
	}
	if cN > 0 {
		varCtr = cSum2/cN - (cSum/cN)*(cSum/cN)
	}

	var total float64
	var extreme int
	for _, v := range p {
		total += float64(v)
		if v < 10 || v > 245 {
			extreme++
		}
	}
	mean := total / float64(len(p))
	light := 1 - math.Min(0.5, 1.5*float64(extreme)/float64(len(p)))
	if mean < 35 {
		light *= math.Max(0.5, mean/35)
	} else if mean > 225 {
		light *= math.Max(0.5, (255-mean)/30)
	}

	fs := frameScore{sharp: 0.6*varCtr + 0.4*varAll, light: math.Max(0.4, light)}
	const g = 16
	for gy := 0; gy < g; gy++ {
		ya, yb := gy*h/g, max(gy*h/g+1, (gy+1)*h/g)
		for gx := 0; gx < g; gx++ {
			xa, xb := gx*w/g, max(gx*w/g+1, (gx+1)*w/g)
			var s, c float64
			for yy := ya; yy < yb; yy += 2 {
				for xx := xa; xx < xb; xx += 2 {
					s += float64(p[yy*w+xx])
					c++
				}
			}
			if c > 0 {
				fs.thumb[gy*g+gx] = float32(s / c)
			}
		}
	}
	return fs
}

func thumbDiff(a, b *[256]float32) float64 {
	var d float64
	for i := range a {
		d += math.Abs(float64(a[i] - b[i]))
	}
	return d / float64(len(a))
}

// pickFrames memilih indeks frame: video dibagi N segmen sama panjang, tiap segmen diambil frame
// berskor tertinggi (skor dinormalisasi terhadap median video supaya ketajaman antar-video sebanding).
// similar = jumlah segmen yang dilewati karena semua kandidatnya terlalu mirip foto terpilih.
func pickFrames(sc []frameScore, count int, fps float64, unique bool) (picked []int, similar int) {
	total := len(sc)
	sharps := make([]float64, total)
	for i := range sc {
		sharps[i] = sc[i].sharp
	}
	sort.Float64s(sharps)
	med := sharps[total/2]
	if med <= 0 {
		med = 1
	}
	score := make([]float64, total)
	for i := range sc {
		score[i] = sc[i].sharp / med * sc[i].light
	}

	trim := 0
	if total > count*4 {
		trim = int(math.Round(trimFrac * float64(total)))
	}
	usable := max(1, total-2*trim)
	count = max(1, min(count, usable))
	for s := 0; s < count; s++ {
		a := trim + int(math.Round(float64(s)*float64(usable)/float64(count)))
		b := max(a+1, trim+int(math.Round(float64(s+1)*float64(usable)/float64(count))))
		cand := make([]int, 0, b-a)
		for i := a; i < b && i < total; i++ {
			cand = append(cand, i)
		}
		sort.SliceStable(cand, func(i, j int) bool { return score[cand[i]] > score[cand[j]] })
		chosen := -1
		for _, c := range cand {
			ok := true
			if unique {
				for _, p := range picked {
					if thumbDiff(&sc[c].thumb, &sc[p].thumb) < similarThresh {
						ok = false
						break
					}
				}
			}
			if ok {
				chosen = c
				break
			}
		}
		if chosen < 0 {
			similar++
			continue
		}
		picked = append(picked, chosen)
	}
	sort.Ints(picked)
	return picked, similar
}

// ---------- EXIF waktu jepret ----------

// withExifDate menyisipkan APP1 (DateTimeOriginal) ke JPEG; bila bukan JPEG dikembalikan apa adanya.
func withExifDate(jpg []byte, t time.Time) []byte {
	if len(jpg) < 4 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		return jpg
	}
	ts := t.Format("2006:01:02 15:04:05") + "\x00" // 20 byte

	// TIFF: header(8) + IFD0(2+12+4) + ExifIFD(2+12+4) + string(20)
	const exifIFDOff = 8 + 18
	const strOff = exifIFDOff + 18
	tiff := make([]byte, strOff+len(ts))
	le := binary.LittleEndian
	copy(tiff, "II")
	le.PutUint16(tiff[2:], 42)
	le.PutUint32(tiff[4:], 8)
	le.PutUint16(tiff[8:], 1) // IFD0: 1 entri → penunjuk Exif IFD
	le.PutUint16(tiff[10:], 0x8769)
	le.PutUint16(tiff[12:], 4)
	le.PutUint32(tiff[14:], 1)
	le.PutUint32(tiff[18:], exifIFDOff)
	le.PutUint32(tiff[22:], 0)
	le.PutUint16(tiff[exifIFDOff:], 1) // Exif IFD: DateTimeOriginal
	le.PutUint16(tiff[exifIFDOff+2:], 0x9003)
	le.PutUint16(tiff[exifIFDOff+4:], 2)
	le.PutUint32(tiff[exifIFDOff+6:], uint32(len(ts)))
	le.PutUint32(tiff[exifIFDOff+10:], strOff)
	le.PutUint32(tiff[exifIFDOff+14:], 0)
	copy(tiff[strOff:], ts)

	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := make([]byte, 4, 4+len(payload))
	seg[0], seg[1] = 0xFF, 0xE1
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)

	// Taruh setelah APP0 (JFIF) bila ada, supaya urutan segmen tetap lazim.
	at := 2
	if jpg[2] == 0xFF && jpg[3] == 0xE0 && len(jpg) > 6 {
		at = 4 + int(binary.BigEndian.Uint16(jpg[4:6]))
		if at > len(jpg) {
			at = 2
		}
	}
	out := make([]byte, 0, len(jpg)+len(seg))
	out = append(out, jpg[:at]...)
	out = append(out, seg...)
	return append(out, jpg[at:]...)
}
