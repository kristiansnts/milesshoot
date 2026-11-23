package core

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Preset Lightroom (.xmp, .dng preset Lightroom Mobile, .lrtemplate lama) → LUT 3D (.cube).
// Port dari src/preset.js + src/colormath.js (MAS EDY). Pengaturan warna & nada dari preset
// dihitung untuk tiap warna di kisi 33³, lalu disimpan sebagai .cube supaya bisa dipakai
// jalur LUT yang sudah ada (unggah foto & video). Ini perkiraan, bukan mesin Adobe — hasil
// mirip untuk preset warna/nada, tapi efek lokal (Clarity/Texture/Dehaze/Vignette/Grain/
// Sharpening/noise reduction) tidak bisa jadi LUT — dilaporkan lewat ignoredEffects.

type presetData struct {
	values map[string]string
	curves map[string][][2]float64
	name   string
}

var (
	reXmpMeta  = regexp.MustCompile(`(?s)<x:xmpmeta.*?</x:xmpmeta>`)
	reRdf      = regexp.MustCompile(`(?s)<rdf:RDF.*?</rdf:RDF>`)
	reCrsAttr  = regexp.MustCompile(`crs:(\w+)="([^"]*)"`)
	reCrsElem  = regexp.MustCompile(`<crs:(\w+)>([^<]*)</crs:\w+>`)
	reCurveBlk = regexp.MustCompile(`(?s)<crs:(ToneCurvePV2012(?:Red|Green|Blue)?)>\s*<rdf:Seq>(.*?)</rdf:Seq>`)
	reCurvePt  = regexp.MustCompile(`<rdf:li>\s*([\d.]+)\s*,\s*([\d.]+)\s*</rdf:li>`)
	reXmpName  = regexp.MustCompile(`<crs:Name>\s*<rdf:Alt>\s*<rdf:li[^>]*>([^<]*)</rdf:li>`)
	reLrPair   = regexp.MustCompile(`(\w+)\s*=\s*(-?[\d.]+)\s*,`)
	reLrTitle  = regexp.MustCompile(`title\s*=\s*"([^"]*)"`)
	reSkipKeys = regexp.MustCompile(`^(Version|ProcessVersion|Name|UUID|Group|SupportsAmount|Copyright|Cluster|PresetType|RequiresRGBTables|HasSettings|CameraProfile|Look)`)
)

var presetIgnoredKeys = map[string]string{
	"Clarity2012": "Clarity", "Texture": "Texture", "Dehaze": "Dehaze",
	"PostCropVignetteAmount": "Vignette", "GrainAmount": "Grain",
	"Sharpness": "Sharpening", "LuminanceSmoothing": "Noise reduction", "ColorNoiseReduction": "Color noise",
}

func isPresetFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xmp", ".dng", ".lrtemplate":
		return true
	}
	return false
}

func extractXMP(buf []byte) string {
	s := string(buf) // literal ASCII cocok dicari walau sisa byte-nya biner (DNG)
	if m := reXmpMeta.FindString(s); m != "" {
		return m
	}
	return reRdf.FindString(s)
}

func parseXMPPreset(xml string) presetData {
	values := map[string]string{}
	for _, m := range reCrsAttr.FindAllStringSubmatch(xml, -1) {
		values[m[1]] = m[2]
	}
	for _, m := range reCrsElem.FindAllStringSubmatch(xml, -1) {
		values[m[1]] = m[2]
	}
	curves := map[string][][2]float64{}
	for _, m := range reCurveBlk.FindAllStringSubmatch(xml, -1) {
		var pts [][2]float64
		for _, p := range reCurvePt.FindAllStringSubmatch(m[2], -1) {
			x, _ := strconv.ParseFloat(p[1], 64)
			y, _ := strconv.ParseFloat(p[2], 64)
			pts = append(pts, [2]float64{x, y})
		}
		curves[m[1]] = pts
	}
	name := ""
	if m := reXmpName.FindStringSubmatch(xml); m != nil {
		name = m[1]
	} else {
		name = values["Name"]
	}
	return presetData{values: values, curves: curves, name: name}
}

func parseLrtemplatePreset(text string) presetData {
	values := map[string]string{}
	for _, m := range reLrPair.FindAllStringSubmatch(text, -1) {
		values[m[1]] = m[2]
	}
	curves := map[string][][2]float64{}
	for _, key := range []string{"ToneCurvePV2012", "ToneCurvePV2012Red", "ToneCurvePV2012Green", "ToneCurvePV2012Blue"} {
		re := regexp.MustCompile(regexp.QuoteMeta(key) + `\s*=\s*\{([^}]*)\}`)
		m := re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		var nums []float64
		for _, p := range strings.Split(m[1], ",") {
			if v, err := strconv.ParseFloat(strings.TrimSpace(p), 64); err == nil {
				nums = append(nums, v)
			}
		}
		var pts [][2]float64
		for i := 0; i+1 < len(nums); i += 2 {
			pts = append(pts, [2]float64{nums[i], nums[i+1]})
		}
		curves[key] = pts
	}
	name := ""
	if m := reLrTitle.FindStringSubmatch(text); m != nil {
		name = m[1]
	}
	return presetData{values: values, curves: curves, name: name}
}

func readPreset(path string) (presetData, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return presetData{}, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	var p presetData
	if ext == ".lrtemplate" {
		p = parseLrtemplatePreset(string(buf))
	} else {
		xml := extractXMP(buf)
		if xml == "" {
			if ext == ".dng" {
				return presetData{}, fmt.Errorf("DNG ini tidak berisi pengaturan preset Lightroom")
			}
			return presetData{}, fmt.Errorf("file ini bukan preset Lightroom (.xmp)")
		}
		p = parseXMPPreset(xml)
	}
	meaningful := len(p.curves) > 0
	for k := range p.values {
		if !reSkipKeys.MatchString(k) {
			meaningful = true
			break
		}
	}
	if !meaningful {
		return presetData{}, fmt.Errorf("preset tidak berisi pengaturan warna")
	}
	return p, nil
}

func presetIgnoredEffects(p presetData) []string {
	var out []string
	for k, label := range presetIgnoredKeys {
		v, ok := p.values[k]
		if !ok {
			continue
		}
		if x, err := strconv.ParseFloat(v, 64); err == nil && math.Abs(x) > 0.5 {
			out = append(out, label)
		}
	}
	sort.Strings(out)
	return out
}

// ---------- color math (port dari colormath.js) ----------

var hslColorNames = [8]string{"Red", "Orange", "Yellow", "Green", "Aqua", "Blue", "Purple", "Magenta"}
var hslCenters = [8]float64{0, 30, 60, 120, 180, 240, 270, 300}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func toLin(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func toSrgb(c float64) float64 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

func lumaOf(r, g, b float64) float64 { return 0.2126*r + 0.7152*g + 0.0722*b }

func rgb2hsl(r, g, b float64) (float64, float64, float64) {
	mx := math.Max(r, math.Max(g, b))
	mn := math.Min(r, math.Min(g, b))
	l := (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	var s float64
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	var h float64
	switch mx {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h * 60, s, l
}

func hsl2rgb(h, s, l float64) (float64, float64, float64) {
	h = math.Mod(math.Mod(h, 360)+360, 360) / 360
	if s == 0 {
		return l, l, l
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	f := func(t float64) float64 {
		t = math.Mod(t+1, 1)
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		default:
			return p
		}
	}
	return f(h + 1.0/3), f(h), f(h - 1.0/3)
}

// makeTCurve: kurva monoton (Fritsch–Carlson) dari titik 0–255, atau nil bila identitas.
func makeTCurve(pts [][2]float64) func(float64) float64 {
	if len(pts) < 2 {
		return nil
	}
	type pt struct{ x, y float64 }
	P := make([]pt, len(pts))
	for i, p := range pts {
		P[i] = pt{p[0] / 255, p[1] / 255}
	}
	sort.Slice(P, func(i, j int) bool { return P[i].x < P[j].x })
	isID := true
	for _, p := range P {
		if math.Abs(p.x-p.y) >= 1e-3 {
			isID = false
			break
		}
	}
	if isID {
		return nil
	}
	n := len(P)
	dx := make([]float64, n-1)
	dy := make([]float64, n-1)
	for i := 0; i < n-1; i++ {
		dx[i] = P[i+1].x - P[i].x
		if dx[i] == 0 {
			dx[i] = 1e-6
		}
		dy[i] = (P[i+1].y - P[i].y) / dx[i]
	}
	m := make([]float64, n)
	m[0] = dy[0]
	m[n-1] = dy[n-2]
	for i := 1; i < n-1; i++ {
		if dy[i-1]*dy[i] <= 0 {
			m[i] = 0
		} else {
			m[i] = (dy[i-1] + dy[i]) / 2
		}
	}
	for i := 0; i < n-1; i++ {
		if dy[i] == 0 {
			m[i] = 0
			m[i+1] = 0
			continue
		}
		a := m[i] / dy[i]
		b := m[i+1] / dy[i]
		h := a*a + b*b
		if h > 9 {
			t := 3 / math.Sqrt(h)
			m[i] = t * a * dy[i]
			m[i+1] = t * b * dy[i]
		}
	}
	return func(x float64) float64 {
		if x <= P[0].x {
			return P[0].y
		}
		if x >= P[n-1].x {
			return P[n-1].y
		}
		i := 0
		for x > P[i+1].x {
			i++
		}
		h := dx[i]
		t := (x - P[i].x) / h
		t2 := t * t
		t3 := t2 * t
		return (2*t3-3*t2+1)*P[i].y + (t3-2*t2+t)*h*m[i] + (-2*t3+3*t2)*P[i+1].y + (t3-t2)*h*m[i+1]
	}
}

type colorTransform func(r, g, b float64) (float64, float64, float64)

func numVal(values map[string]string, k string) float64 {
	v, ok := values[k]
	if !ok {
		return 0
	}
	x, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return x
}

// buildTransform: fungsi warna [r,g,b] 0–1 (sRGB) → [r,g,b] 0–1. Port 1:1 dari
// colormath.js buildTransform (white balance/exposure, highlights/shadows/whites/
// blacks+parametric+contrast, tone curve, HSL+vibrance/saturation, color grading).
func buildTransform(p presetData) colorTransform {
	V := p.values
	num := func(k string) float64 { return numVal(V, k) }

	kT := num("IncrementalTemperature") / 100
	kM := num("IncrementalTint") / 100
	if _, ok := V["IncrementalTemperature"]; !ok {
		if _, ok2 := V["Temperature"]; ok2 {
			kT = clampSigned((num("Temperature")-5500)/3500, -1, 1)
		}
	}
	if _, ok := V["IncrementalTint"]; !ok {
		if _, ok2 := V["Tint"]; ok2 {
			kM = clampSigned(num("Tint")/100, -1, 1)
		}
	}
	wr := 1 + 0.26*kT + 0.10*kM
	wg := 1 + 0.18*kT - 0.20*kM
	wb := 1 - 0.40*kT + 0.10*kM
	wl := lumaOf(wr, wg, wb)
	wr /= wl
	wg /= wl
	wb /= wl

	ev := math.Pow(2, num("Exposure2012"))
	con := num("Contrast2012") / 100
	hi := num("Highlights2012") / 100
	sh := num("Shadows2012") / 100
	wh := num("Whites2012") / 100
	bl := num("Blacks2012") / 100

	par := [4]float64{num("ParametricShadows") / 100, num("ParametricDarks") / 100, num("ParametricLights") / 100, num("ParametricHighlights") / 100}
	splitOr := func(key string, def float64) float64 {
		if v, ok := V[key]; ok {
			if x, err := strconv.ParseFloat(v, 64); err == nil {
				return x / 100
			}
		}
		return def / 100
	}
	sp0 := splitOr("ParametricShadowSplit", 25)
	sp1 := splitOr("ParametricMidtoneSplit", 50)
	sp2 := splitOr("ParametricHighlightSplit", 75)
	parC := [4]float64{sp0 / 2, (sp0 + sp1) / 2, (sp1 + sp2) / 2, (sp2 + 1) / 2}

	cM := makeTCurve(p.curves["ToneCurvePV2012"])
	cR := makeTCurve(p.curves["ToneCurvePV2012Red"])
	cG := makeTCurve(p.curves["ToneCurvePV2012Green"])
	cB := makeTCurve(p.curves["ToneCurvePV2012Blue"])

	sat := num("Saturation") / 100
	vib := num("Vibrance") / 100

	var hslH, hslS, hslL [8]float64
	hasHsl := false
	for i, c := range hslColorNames {
		hslH[i] = num("HueAdjustment" + c)
		hslS[i] = num("SaturationAdjustment"+c) / 100
		hslL[i] = num("LuminanceAdjustment"+c) / 100
		if hslH[i] != 0 || hslS[i] != 0 || hslL[i] != 0 {
			hasHsl = true
		}
	}

	type gradeEntry struct {
		zone       string
		dr, dg, db float64
		s          float64
	}
	var grade []gradeEntry
	addGrade := func(zone string, hue, satPct float64) {
		s := satPct / 100
		if s <= 0 {
			return
		}
		r, g, b := hsl2rgb(hue, 1, 0.5)
		grade = append(grade, gradeEntry{zone, r - 0.5, g - 0.5, b - 0.5, s})
	}
	addGrade("shadow", num("SplitToningShadowHue"), num("SplitToningShadowSaturation"))
	addGrade("mid", num("ColorGradeMidtoneHue"), num("ColorGradeMidtoneSat"))
	addGrade("high", num("SplitToningHighlightHue"), num("SplitToningHighlightSaturation"))
	addGrade("global", num("ColorGradeGlobalHue"), num("ColorGradeGlobalSat"))
	balance := num("SplitToningBalance") / 100

	return func(r0, g0, b0 float64) (float64, float64, float64) {
		R := toLin(r0) * wr * ev
		G := toLin(g0) * wg * ev
		B := toLin(b0) * wb * ev
		mx := math.Max(R, math.Max(G, B))
		if mx > 1 {
			k := (1 - math.Exp(-mx)) / (1 - math.Exp(-1)) / mx
			R *= k
			G *= k
			B *= k
		}
		R = toSrgb(clamp01(R))
		G = toSrgb(clamp01(G))
		B = toSrgb(clamp01(B))

		L := lumaOf(R, G, B)
		nL := L +
			0.28*sh*math.Exp(-math.Pow((L-0.22)/0.2, 2)) +
			0.28*hi*math.Exp(-math.Pow((L-0.78)/0.2, 2)) +
			0.22*wh*math.Pow(L, 3) +
			0.22*bl*math.Pow(1-L, 3)
		for i := 0; i < 4; i++ {
			if par[i] != 0 {
				nL += 0.16 * par[i] * math.Exp(-math.Pow((nL-parC[i])/0.13, 2))
			}
		}
		if con != 0 {
			y := 0.5 + (nL-0.5)*(1+0.65*con)
			factor := clamp01(4*nL*(1-nL) + 0.15)
			nL = nL + (y-nL)*factor
		}
		nL = clamp01(nL)
		if L > 0.04 {
			k := nL / L
			R *= k
			G *= k
			B *= k
		} else {
			d := nL - L
			R += d
			G += d
			B += d
		}
		R = clamp01(R)
		G = clamp01(G)
		B = clamp01(B)

		if cM != nil {
			R = cM(R)
			G = cM(G)
			B = cM(B)
		}
		if cR != nil {
			R = cR(R)
		}
		if cG != nil {
			G = cG(G)
		}
		if cB != nil {
			B = cB(B)
		}
		R = clamp01(R)
		G = clamp01(G)
		B = clamp01(B)

		if hasHsl || sat != 0 || vib != 0 {
			h, s, l := rgb2hsl(R, G, B)
			if hasHsl && s > 0.001 {
				var dh, ds, dl float64
				for i := 0; i < 8; i++ {
					c := hslCenters[i]
					prev := hslCenters[(i+7)%8]
					next := hslCenters[(i+1)%8]
					d := math.Mod(h-c+540, 360) - 180
					var width float64
					if d >= 0 {
						width = math.Mod(next-c+360, 360)
					} else {
						width = math.Mod(c-prev+360, 360)
					}
					if width == 0 {
						width = 360
					}
					w := math.Max(0, 1-math.Abs(d)/width)
					if w == 0 {
						continue
					}
					dh += w * hslH[i] * 0.3
					ds += w * hslS[i]
					dl += w * hslL[i]
				}
				h += dh
				s = clamp01(s * (1 + ds))
				l = clamp01(l + dl*0.25*math.Min(1, s*1.5))
			}
			if sat != 0 {
				s = clamp01(s * (1 + sat))
			}
			if vib != 0 {
				s = clamp01(s * (1 + vib*(1-s)*1.2))
			}
			R, G, B = hsl2rgb(h, s, l)
		}

		if len(grade) > 0 {
			Lg := lumaOf(R, G, B)
			wS := clamp01(math.Pow(1-Lg, 2) * (1 - balance))
			wH := clamp01(math.Pow(Lg, 2) * (1 + balance))
			wM := clamp01(1 - wS - wH)
			for _, ge := range grade {
				var zw float64
				switch ge.zone {
				case "shadow":
					zw = wS
				case "high":
					zw = wH
				case "mid":
					zw = wM
				default:
					zw = 1
				}
				w := zw * ge.s * 0.35
				R += ge.dr * w
				G += ge.dg * w
				B += ge.db * w
			}
			L2 := lumaOf(R, G, B)
			d := Lg - L2
			R += d
			G += d
			B += d
		}
		return clamp01(R), clamp01(G), clamp01(B)
	}
}

func clampSigned(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// presetToCube: tulis fungsi warna sebagai teks .cube (kisi size³, merah bergerak paling cepat).
func presetToCube(p presetData, title string, size int) string {
	f := buildTransform(p)
	n := size - 1
	var sb strings.Builder
	fmt.Fprintf(&sb, "TITLE \"%s\"\n", strings.ReplaceAll(title, `"`, "'"))
	sb.WriteString("# Dibuat fotonaik dari preset Lightroom\n")
	fmt.Fprintf(&sb, "LUT_3D_SIZE %d\n", size)
	for bI := 0; bI < size; bI++ {
		for gI := 0; gI < size; gI++ {
			for rI := 0; rI < size; rI++ {
				R, G, B := f(float64(rI)/float64(n), float64(gI)/float64(n), float64(bI)/float64(n))
				fmt.Fprintf(&sb, "%.5f %.5f %.5f\n", R, G, B)
			}
		}
	}
	return sb.String()
}

// ConvertPresetToLUT membaca preset Lightroom (.xmp/.dng/.lrtemplate), menghitungnya jadi LUT
// 3D (33³), dan menyimpannya di pustaka LUT sebagai "<nama> (preset).cube". Dipanggil dari
// ImportLUT saat file yang dipilih bukan .cube.
func ConvertPresetToLUT(dataDir, srcPath string) (LUTFile, []string, error) {
	p, err := readPreset(srcPath)
	if err != nil {
		return LUTFile{}, nil, err
	}
	base := p.name
	if base == "" {
		base = strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	}
	base = lutNameBadRe.ReplaceAllString(base, " ")
	base = strings.TrimSpace(strings.Join(strings.Fields(base), " "))
	if base == "" {
		base = "Preset"
	}
	if len(base) > 60 {
		base = base[:60]
	}

	cubeText := presetToCube(p, base, 33)
	dir := LUTDir(dataDir)
	name := base + " (preset).cube"
	for i := 2; fileExists(filepath.Join(dir, name)); i++ {
		name = fmt.Sprintf("%s (preset) %d.cube", base, i)
	}
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, []byte(cubeText), 0o644); err != nil {
		return LUTFile{}, nil, fmt.Errorf("gagal menyimpan: %w", err)
	}
	return LUTFile{Name: name, Path: dst}, presetIgnoredEffects(p), nil
}
