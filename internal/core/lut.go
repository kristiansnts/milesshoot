package core

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// LUT 3D (.cube) dari Lightroom/Capture One/DaVinci. Port dari src/lut.js:
// parse + interpolasi trilinear pada piksel RGBA (stride 4).

type lut3D struct {
	size int
	min  [3]float64
	max  [3]float64
	data []float64 // size^3 * 3
}

var (
	lutMu    sync.Mutex
	lutCache = map[string]lutEntry{}
)

type lutEntry struct {
	mtime int64
	lut   *lut3D
}

func loadLUT(file string) (*lut3D, error) {
	fi, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	key := file
	lutMu.Lock()
	if e, ok := lutCache[key]; ok && e.mtime == fi.ModTime().UnixNano() {
		lutMu.Unlock()
		return e.lut, nil
	}
	lutMu.Unlock()

	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	lut, err := parseCube(f)
	if err != nil {
		return nil, err
	}
	lutMu.Lock()
	lutCache[key] = lutEntry{mtime: fi.ModTime().UnixNano(), lut: lut}
	lutMu.Unlock()
	return lut, nil
}

func parseCube(r interface{ Read([]byte) (int, error) }) (*lut3D, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	lut := &lut3D{min: [3]float64{0, 0, 0}, max: [3]float64{1, 1, 1}}
	var vals []float64
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "TITLE"):
			continue
		case strings.HasPrefix(up, "LUT_1D_SIZE"):
			return nil, fmt.Errorf("LUT 1D tidak didukung, pakai LUT 3D")
		case strings.HasPrefix(up, "LUT_3D_SIZE"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				lut.size, _ = strconv.Atoi(fields[1])
			}
			continue
		case strings.HasPrefix(up, "DOMAIN_MIN"):
			lut.min = triple(line)
			continue
		case strings.HasPrefix(up, "DOMAIN_MAX"):
			lut.max = triple(line)
			continue
		}
		if len(up) > 0 && (up[0] >= 'A' && up[0] <= 'Z' || up[0] == '_') {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 3 {
			r0, e0 := strconv.ParseFloat(f[0], 64)
			g0, e1 := strconv.ParseFloat(f[1], 64)
			b0, e2 := strconv.ParseFloat(f[2], 64)
			if e0 == nil && e1 == nil && e2 == nil {
				vals = append(vals, r0, g0, b0)
			}
		}
	}
	if !(lut.size >= 2 && lut.size <= 256) {
		return nil, fmt.Errorf("file .cube tidak punya LUT_3D_SIZE yang benar")
	}
	want := lut.size * lut.size * lut.size * 3
	if len(vals) != want {
		return nil, fmt.Errorf("isi LUT tidak lengkap (%d dari %d titik)", len(vals)/3, lut.size*lut.size*lut.size)
	}
	lut.data = vals
	return lut, nil
}

func triple(line string) [3]float64 {
	f := strings.Fields(line)
	var out [3]float64
	for i := 0; i < 3 && i+1 < len(f); i++ {
		out[i], _ = strconv.ParseFloat(f[i+1], 64)
	}
	return out
}

type lutTable struct {
	i0 [256]int
	f  [256]float64
}

func mkTable(scale, off float64, n int) lutTable {
	var t lutTable
	for v := 0; v < 256; v++ {
		x := (float64(v) - off) * scale
		if x < 0 {
			x = 0
		} else if x > float64(n) {
			x = float64(n)
		}
		a := int(math.Floor(x))
		if a > n-1 {
			a = n - 1
		}
		t.i0[v] = a
		t.f[v] = x - float64(a)
	}
	return t
}

// applyLUT: interpolasi trilinear pada buffer RGBA (stride 4; alpha dilewati). strength 0..1.
func applyLUT(pix []uint8, lut *lut3D, strength float64) {
	size := lut.size
	n := size - 1
	s2 := size * size
	data := lut.data
	sr := float64(n) / (255 * (lut.max[0] - lut.min[0]))
	sg := float64(n) / (255 * (lut.max[1] - lut.min[1]))
	sb := float64(n) / (255 * (lut.max[2] - lut.min[2]))
	or := lut.min[0] * 255
	og := lut.min[1] * 255
	ob := lut.min[2] * 255
	k := strength
	if k < 0 {
		k = 0
	} else if k > 1 {
		k = 1
	}
	ik := 1 - k
	R := mkTable(sr, or, n)
	G := mkTable(sg, og, n)
	B := mkTable(sb, ob, n)

	for p := 0; p+3 <= len(pix); p += 4 {
		rr, gg, bb := pix[p], pix[p+1], pix[p+2]
		r0, g0, b0 := R.i0[rr], G.i0[gg], B.i0[bb]
		fr, fg, fb := R.f[rr], G.f[gg], B.f[bb]
		c000 := (r0 + g0*size + b0*s2) * 3
		c100 := c000 + 3
		c010 := c000 + size*3
		c110 := c010 + 3
		c001 := c000 + s2*3
		c101 := c001 + 3
		c011 := c001 + size*3
		c111 := c011 + 3
		for ch := 0; ch < 3; ch++ {
			x00 := data[c000+ch] + (data[c100+ch]-data[c000+ch])*fr
			x10 := data[c010+ch] + (data[c110+ch]-data[c010+ch])*fr
			x01 := data[c001+ch] + (data[c101+ch]-data[c001+ch])*fr
			x11 := data[c011+ch] + (data[c111+ch]-data[c011+ch])*fr
			y0 := x00 + (x10-x00)*fg
			y1 := x01 + (x11-x01)*fg
			v := (y0 + (y1-y0)*fb) * 255
			v = v*k + float64(pix[p+ch])*ik
			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			pix[p+ch] = uint8(v + 0.5)
		}
	}
}

// ---- Pustaka LUT (.cube) tersimpan di <dataDir>/luts/ ----

// LUTFile: satu entri pustaka LUT yang dikembalikan ke UI.
type LUTFile struct {
	Name string `json:"name"` // nama file (unik, dipakai sebagai pengenal)
	Path string `json:"path"` // path lengkap — inilah yang disimpan ke Settings.LUT.Photo/Video
}

var lutNameBadRe = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

// LUTDir: folder pustaka LUT, dibuat bila belum ada.
func LUTDir(dataDir string) string {
	dir := filepath.Join(dataDir, "luts")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// ListLUTs: daftar file .cube di pustaka, terurut nama.
func ListLUTs(dataDir string) []LUTFile {
	dir := LUTDir(dataDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []LUTFile{}
	}
	out := []LUTFile{}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".cube") {
			continue
		}
		out = append(out, LUTFile{Name: e.Name(), Path: filepath.Join(dir, e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// ImportLUT: validasi file .cube sumber (harus benar-benar ter-parse, bukan cuma
// cek ekstensi) lalu salin ke pustaka. Nama yang sudah dipakai mendapat sufiks " 2", " 3", dst.
// Preset Lightroom (.xmp/.dng/.lrtemplate) diubah dulu jadi .cube lewat ConvertPresetToLUT.
func ImportLUT(dataDir, srcPath string) (LUTFile, error) {
	if isPresetFile(srcPath) {
		lf, _, err := ConvertPresetToLUT(dataDir, srcPath)
		return lf, err
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return LUTFile{}, fmt.Errorf("tidak bisa membuka file: %w", err)
	}
	_, perr := parseCube(f)
	f.Close()
	if perr != nil {
		return LUTFile{}, fmt.Errorf("bukan file .cube yang valid: %w", perr)
	}

	base := lutNameBadRe.ReplaceAllString(strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath)), " ")
	base = strings.TrimSpace(strings.Join(strings.Fields(base), " "))
	if base == "" {
		base = "LUT"
	}
	dir := LUTDir(dataDir)
	name := base + ".cube"
	for i := 2; fileExists(filepath.Join(dir, name)); i++ {
		name = fmt.Sprintf("%s %d.cube", base, i)
	}
	dstPath := filepath.Join(dir, name)
	if err := copyFile(srcPath, dstPath); err != nil {
		return LUTFile{}, fmt.Errorf("gagal menyalin: %w", err)
	}
	return LUTFile{Name: name, Path: dstPath}, nil
}

// DeleteLUT: hapus satu file dari pustaka. `name` dibatasi hanya basename (anti path-traversal) —
// tidak pernah menerima path lengkap, supaya tak bisa dipakai menghapus file lain di luar pustaka.
func DeleteLUT(dataDir, name string) error {
	if name == "" || name != filepath.Base(name) || strings.Contains(name, "..") {
		return fmt.Errorf("nama LUT tidak sah")
	}
	return os.Remove(filepath.Join(LUTDir(dataDir), name))
}
