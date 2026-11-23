package core

import (
	"path/filepath"
	"strings"
	"testing"
)

const sampleXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="Adobe XMP Core 7.0">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/"
    crs:Version="15.0"
    crs:ProcessVersion="15.4"
    crs:Temperature="6500"
    crs:Tint="10"
    crs:Exposure2012="+0.50"
    crs:Contrast2012="20"
    crs:Highlights2012="-30"
    crs:Shadows2012="25"
    crs:Whites2012="10"
    crs:Blacks2012="-10"
    crs:Vibrance="15"
    crs:Saturation="5"
    crs:Clarity2012="20">
   <crs:Name>
    <rdf:Alt>
     <rdf:li xml:lang="x-default">Sample Warm Preset</rdf:li>
    </rdf:Alt>
   </crs:Name>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
`

func TestReadPresetXMP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warm.xmp")
	writeFile(t, path, sampleXMP)

	p, err := readPreset(path)
	if err != nil {
		t.Fatalf("readPreset: %v", err)
	}
	if p.name != "Sample Warm Preset" {
		t.Fatalf("nama preset salah: %q", p.name)
	}
	if numVal(p.values, "Exposure2012") != 0.5 {
		t.Fatalf("Exposure2012 salah: %v", p.values["Exposure2012"])
	}
	ignored := presetIgnoredEffects(p)
	if len(ignored) != 1 || ignored[0] != "Clarity" {
		t.Fatalf("ignored effects salah: %v", ignored)
	}
}

func TestBuildTransformIdentityWhenNeutral(t *testing.T) {
	p := presetData{values: map[string]string{}, curves: map[string][][2]float64{}}
	f := buildTransform(p)
	r, g, b := f(0.5, 0.3, 0.8)
	if diff(r, 0.5) > 0.01 || diff(g, 0.3) > 0.01 || diff(b, 0.8) > 0.01 {
		t.Fatalf("preset kosong harus (hampir) tidak mengubah warna, dapat (%v,%v,%v)", r, g, b)
	}
}

func TestBuildTransformExposureBrightens(t *testing.T) {
	p := presetData{values: map[string]string{"Exposure2012": "1.0"}, curves: map[string][][2]float64{}}
	f := buildTransform(p)
	r, _, _ := f(0.5, 0.5, 0.5)
	if r <= 0.5 {
		t.Fatalf("Exposure2012=1.0 (stop +1) harus mencerahkan, dapat %v dari 0.5", r)
	}
}

func TestPresetToCubeShape(t *testing.T) {
	p := presetData{values: map[string]string{"Exposure2012": "0.3"}, curves: map[string][][2]float64{}}
	cube := presetToCube(p, "Uji", 5)
	if !strings.Contains(cube, "LUT_3D_SIZE 5") {
		t.Fatalf("header LUT_3D_SIZE hilang:\n%s", cube)
	}
	lines := strings.Split(strings.TrimSpace(cube), "\n")
	// 3 baris header + 5*5*5 baris data
	if len(lines) != 3+125 {
		t.Fatalf("jumlah baris salah: %d", len(lines))
	}
}

func TestConvertPresetToLUTWritesCubeFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.xmp")
	writeFile(t, src, sampleXMP)

	lf, ignored, err := ConvertPresetToLUT(dir, src)
	if err != nil {
		t.Fatalf("ConvertPresetToLUT: %v", err)
	}
	if !strings.Contains(lf.Name, "(preset)") {
		t.Fatalf("nama harus menandai hasil konversi preset: %q", lf.Name)
	}
	if len(ignored) != 1 || ignored[0] != "Clarity" {
		t.Fatalf("ignored salah: %v", ignored)
	}
	// Hasilnya harus benar-benar .cube yang bisa dimuat lagi oleh parseCube (loadLUT).
	if _, err := loadLUT(lf.Path); err != nil {
		t.Fatalf("hasil konversi bukan .cube yang valid: %v", err)
	}
}

func TestReadPresetRejectsEmptyPreset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.xmp")
	writeFile(t, path, `<x:xmpmeta><rdf:RDF><rdf:Description crs:Version="15.0" crs:ProcessVersion="15.4"/></rdf:RDF></x:xmpmeta>`)
	if _, err := readPreset(path); err == nil {
		t.Fatalf("preset tanpa pengaturan warna harus ditolak")
	}
}

func diff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
