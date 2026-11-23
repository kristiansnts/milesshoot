// Package video menyiapkan video untuk FotoYu persis seperti web fotoyu.com:
// 17 frame JPEG (sisi terpanjang 1440 px) pada detik tertentu, dibungkus .zip (store).
// Port dari src/video.js. Butuh ffmpeg di sistem.
package video

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	"math"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"time"

	"fotonaik/internal/core"
)

const frameSize = 1440

type stamp struct{ a, b float64 }

var stamps = buildStamps()

func buildStamps() []stamp {
	s := []stamp{{0, 0}, {2, 0}}
	for t := 0; t < 12; t++ {
		s = append(s, stamp{4, float64(t)})
	}
	s = append(s, stamp{6, 0}, stamp{8, 0}, stamp{10, -1})
	return s // 17
}

var (
	ffmpegOnce sync.Once
	ffmpegPath string
)

func findFfmpeg() string {
	ffmpegOnce.Do(func() {
		name := "ffmpeg"
		if runtime.GOOS == "windows" {
			name = "ffmpeg.exe"
		}
		if p, err := exec.LookPath(name); err == nil {
			ffmpegPath = p
			return
		}
		for _, c := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg", "/usr/bin/ffmpeg"} {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				ffmpegPath = c
				return
			}
		}
	})
	return ffmpegPath
}

func Available() bool { return findFfmpeg() != "" }

var (
	reDur  = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)
	reDim  = regexp.MustCompile(`Video:[^\n]*?\b(\d{2,5})x(\d{2,5})\b`)
	reRot1 = regexp.MustCompile(`rotation of (-?\d+(?:\.\d+)?) degrees`)
	reRot2 = regexp.MustCompile(`rotate\s*:\s*(-?\d+)`)
	reCT   = regexp.MustCompile(`creation_time\s*:\s*(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z?)`)
)

type probeInfo struct {
	duration      float64
	width, height int
	rotation      int
	createdAt     time.Time
}

func probe(file string) (probeInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, findFfmpeg(), "-hide_banner", "-i", file)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run() // ffmpeg -i tanpa output selalu "error"; info ada di stderr
	s := stderr.String()

	d := reDur.FindStringSubmatch(s)
	v := reDim.FindStringSubmatch(s)
	if d == nil || v == nil {
		return probeInfo{}, &core.UploadError{Msg: "Video tidak bisa dibaca (format tidak didukung atau rusak)", Permanent: true}
	}
	hh, _ := strconv.ParseFloat(d[1], 64)
	mm, _ := strconv.ParseFloat(d[2], 64)
	ss, _ := strconv.ParseFloat(d[3], 64)
	dur := hh*3600 + mm*60 + ss
	w, _ := strconv.Atoi(v[1])
	h, _ := strconv.Atoi(v[2])

	rotation := 0
	if m := reRot1.FindStringSubmatch(s); m != nil {
		f, _ := strconv.ParseFloat(m[1], 64)
		rotation = ((int(math.Round(f)) % 360) + 360) % 360
	} else if m := reRot2.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		rotation = ((n % 360) + 360) % 360
	}
	swap := rotation == 90 || rotation == 270

	var createdAt time.Time
	if m := reCT.FindStringSubmatch(s); m != nil {
		v := m[1]
		if v[len(v)-1] != 'Z' {
			v += "Z"
		}
		if ct, err := time.Parse(time.RFC3339, v); err == nil && ct.Year() >= 2000 {
			createdAt = ct
		}
	}
	info := probeInfo{duration: dur, rotation: rotation, createdAt: createdAt}
	if swap {
		info.width, info.height = h, w
	} else {
		info.width, info.height = w, h
	}
	return info, nil
}

func timestamps(duration float64) []float64 {
	s := math.Min(1, duration/20)
	d := (1.0 / 24) * s
	m := 2 * s
	out := make([]float64, len(stamps))
	for i, st := range stamps {
		t := m + st.a*s + st.b*d
		t = math.Max(0, math.Min(duration-0.05, t))
		out[i] = math.Round(t*1000) / 1000
	}
	return out
}

// 'auto' ikuti rotasi tersimpan; '0'..'270' putar paksa searah jarum jam.
func filterFor(orientation string) string {
	var f []string
	switch orientation {
	case "90":
		f = append(f, "transpose=1")
	case "180":
		f = append(f, "hflip", "vflip")
	case "270":
		f = append(f, "transpose=2")
	}
	f = append(f, fmt.Sprintf("scale='if(gte(iw,ih),%d,-2)':'if(gte(iw,ih),-2,%d)'", frameSize, frameSize))
	return joinComma(f)
}

func joinComma(f []string) string {
	out := ""
	for i, v := range f {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}

func grabFrame(file string, t float64, vf string, noAutoRotate bool) ([]byte, error) {
	args := []string{"-hide_banner", "-loglevel", "error"}
	if noAutoRotate {
		args = append(args, "-noautorotate")
	}
	args = append(args, "-ss", strconv.FormatFloat(t, 'f', 3, 64), "-i", file,
		"-frames:v", "1", "-vf", vf, "-q:v", "3", "-f", "image2", "-c:v", "mjpeg", "pipe:1")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, findFfmpeg(), args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil || out.Len() < 100 {
		return nil, fmt.Errorf("gagal mengambil frame: %s", lastLine(errb.String()))
	}
	return out.Bytes(), nil
}

func lastLine(s string) string {
	lines := bytes.Split([]byte(s), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if len(bytes.TrimSpace(lines[i])) > 0 {
			return string(bytes.TrimSpace(lines[i]))
		}
	}
	return ""
}

// Prepare: dipakai sebagai core.VideoPrepareFn (disuntik di main). frames = konkurensi grab frame.
// lut = LUT video (gaya MAS EDY: opts.grade di prepare() — tiap frame pratinjau diwarnai
// sebelum di-zip, lewat core.GradeJPEG supaya tak perlu pecah enkapsulasi loadLUT/applyLUT).
func Prepare(path string, orientation string, frames int, lut core.LUT) (core.PreparedVideo, error) {
	if !Available() {
		return core.PreparedVideo{}, &core.UploadError{Msg: "Upload video butuh ffmpeg (pasang ffmpeg di sistem)", Pause: true}
	}
	info, err := probe(path)
	if err != nil {
		return core.PreparedVideo{}, err
	}
	if info.duration <= 0.3 {
		return core.PreparedVideo{}, &core.UploadError{Msg: "Video terlalu pendek", Permanent: true}
	}
	if orientation == "" {
		orientation = "auto"
	}
	vf := filterFor(orientation)
	manual := orientation != "auto"
	times := timestamps(info.duration)

	conc := frames
	if conc < 1 {
		conc = 2
	}
	grabbed := make([][]byte, len(times))
	var grabErr error
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, conc)
	for i, t := range times {
		wg.Add(1)
		go func(i int, t float64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			b, e := grabFrame(path, t, vf, manual)
			if e != nil { // dekat akhir video: mundur sedikit
				b, e = grabFrame(path, math.Max(0, t-0.25), vf, manual)
			}
			mu.Lock()
			if e != nil {
				grabErr = e
			} else {
				grabbed[i] = b
			}
			mu.Unlock()
		}(i, t)
	}
	wg.Wait()
	if grabErr != nil {
		return core.PreparedVideo{}, grabErr
	}
	for _, b := range grabbed {
		if len(b) == 0 {
			return core.PreparedVideo{}, fmt.Errorf("jumlah frame tidak lengkap")
		}
	}

	if lut.Enabled && lut.File != "" {
		var gwg sync.WaitGroup
		gsem := make(chan struct{}, conc)
		for i := range grabbed {
			gwg.Add(1)
			go func(i int) {
				defer gwg.Done()
				gsem <- struct{}{}
				defer func() { <-gsem }()
				grabbed[i] = core.GradeJPEG(grabbed[i], lut)
			}(i)
		}
		gwg.Wait()
	}

	zipBuf, err := makeZip(grabbed)
	if err != nil {
		return core.PreparedVideo{}, err
	}
	fw, fh := jpegDims(grabbed[0])
	at := info.createdAt
	if at.IsZero() {
		if fi, e := os.Stat(path); e == nil {
			at = fi.ModTime()
		}
	}
	return core.PreparedVideo{
		Zip: zipBuf, Width: fw, Height: fh, Duration: int(info.duration * 1000),
		FirstFrame: grabbed[0], OriginalAt: at, Frames: len(grabbed),
	}, nil
}

// makeZip: simpan frame sebagai "0.jpeg".."16.jpeg" tanpa kompresi (store), seperti web FotoYu.
func makeZip(frames [][]byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for i, data := range frames {
		fh := &zip.FileHeader{Name: fmt.Sprintf("%d.jpeg", i), Method: zip.Store}
		f, err := w.CreateHeader(fh)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func jpegDims(b []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// ProbeDims mengembalikan durasi & orientasi tampilan (buat UI pemilihan orientasi nanti).
func ProbeDims(path string) (durationSec float64, width, height, rotation int, err error) {
	info, e := probe(path)
	if e != nil {
		return 0, 0, 0, 0, e
	}
	return info.duration, info.width, info.height, info.rotation, nil
}
