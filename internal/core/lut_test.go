package core

import (
	"strings"
	"testing"
)

// cube 2x2x2: entri urut merah paling cepat.
func cube(corners [8][3]float64) string {
	var b strings.Builder
	b.WriteString("LUT_3D_SIZE 2\n")
	for _, c := range corners {
		b.WriteString(floatLine(c))
	}
	return b.String()
}
func floatLine(c [3]float64) string {
	return trimF(c[0]) + " " + trimF(c[1]) + " " + trimF(c[2]) + "\n"
}
func trimF(f float64) string {
	if f == 0 {
		return "0.0"
	}
	return "1.0"
}

var identity = [8][3]float64{
	{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}, {0, 0, 1}, {1, 0, 1}, {0, 1, 1}, {1, 1, 1},
}

// invert: output = 1 - koordinat tiap sudut
var invert = [8][3]float64{
	{1, 1, 1}, {0, 1, 1}, {1, 0, 1}, {0, 0, 1}, {1, 1, 0}, {0, 1, 0}, {1, 0, 0}, {0, 0, 0},
}

func TestLUTIdentity(t *testing.T) {
	lut, err := parseCube(strings.NewReader(cube(identity)))
	if err != nil {
		t.Fatal(err)
	}
	pix := []uint8{10, 20, 30, 255, 128, 128, 128, 255, 200, 50, 90, 255}
	orig := append([]uint8(nil), pix...)
	applyLUT(pix, lut, 1)
	for i := 0; i < len(pix); i++ {
		if i%4 == 3 {
			continue // alpha
		}
		if abs(int(pix[i])-int(orig[i])) > 1 {
			t.Fatalf("identitas mengubah piksel: pix[%d]=%d asal=%d", i, pix[i], orig[i])
		}
	}
}

func TestLUTInvert(t *testing.T) {
	lut, err := parseCube(strings.NewReader(cube(invert)))
	if err != nil {
		t.Fatal(err)
	}
	pix := []uint8{10, 20, 30, 255}
	applyLUT(pix, lut, 1)
	// 10→245, 20→235, 30→225 (±2)
	want := []uint8{245, 235, 225}
	for i := 0; i < 3; i++ {
		if abs(int(pix[i])-int(want[i])) > 2 {
			t.Fatalf("invert salah: pix[%d]=%d, mau ~%d", i, pix[i], want[i])
		}
	}
}

func TestLUTStrengthHalf(t *testing.T) {
	lut, _ := parseCube(strings.NewReader(cube(invert)))
	pix := []uint8{0, 0, 0, 255}
	applyLUT(pix, lut, 0.5) // 0→255 pada kekuatan penuh; setengah → ~128
	if abs(int(pix[0])-128) > 2 {
		t.Fatalf("kekuatan 50%% salah: %d (mau ~128)", pix[0])
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
