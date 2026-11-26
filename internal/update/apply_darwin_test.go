//go:build darwin

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeApp(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "Contents", "MacOS", "milesshoot"), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func deadPID(t *testing.T) int {
	t.Helper()
	c := exec.Command("/usr/bin/true")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	return c.Process.Pid // sudah selesai → skrip tidak perlu menunggu
}

func waitLog(t *testing.T, path, want string) string {
	t.Helper()
	for i := 0; i < 100; i++ {
		if b, _ := os.ReadFile(path); strings.Contains(string(b), want) {
			return string(b)
		}
		time.Sleep(100 * time.Millisecond)
	}
	b, _ := os.ReadFile(path)
	t.Fatalf("log tidak memuat %q dalam 10 dtk: %q", want, b)
	return ""
}

func read(t *testing.T, app string) string {
	b, err := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "milesshoot"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApplySwapsBundle(t *testing.T) {
	d := t.TempDir()
	bundle, src, logp := filepath.Join(d, "Apps", "milesshoot.app"), filepath.Join(d, "stage", "milesshoot.app"), filepath.Join(d, "apply.log")
	fakeApp(t, bundle, "lama")
	fakeApp(t, src, "baru")

	if err := Apply(src, bundle, deadPID(t), logp, false); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logp, "OK:")
	if got := read(t, bundle); got != "baru" {
		t.Fatalf("bundle harus berisi versi baru, dapat %q", got)
	}
	for _, left := range []string{bundle + ".update-new", bundle + ".update-old"} {
		if _, err := os.Stat(left); err == nil {
			t.Errorf("sisa %s harus dibersihkan", left)
		}
	}
	if s := TakeApplyLog(logp); !strings.Contains(s, "OK:") {
		t.Errorf("TakeApplyLog: %q", s)
	}
	if _, err := os.Stat(logp); err == nil {
		t.Error("log harus dihapus setelah dibaca")
	}
}

// Bila menyalin versi baru gagal, versi lama TIDAK boleh tersentuh.
func TestApplyKeepsOldWhenCopyFails(t *testing.T) {
	d := t.TempDir()
	bundle, src, logp := filepath.Join(d, "milesshoot.app"), filepath.Join(d, "stage", "milesshoot.app"), filepath.Join(d, "apply.log")
	fakeApp(t, bundle, "lama")
	fakeApp(t, src, "baru")
	secret := filepath.Join(src, "Contents", "tak-terbaca")
	os.WriteFile(secret, []byte("x"), 0o000) // ditto gagal membacanya
	defer os.Chmod(secret, 0o644)

	if err := Apply(src, bundle, deadPID(t), logp, false); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logp, "GAGAL")
	if got := read(t, bundle); got != "lama" {
		t.Fatalf("versi lama harus tetap utuh, dapat %q", got)
	}
	if _, err := os.Stat(bundle + ".update-new"); err == nil {
		t.Error("salinan setengah jadi harus dibuang")
	}
}

func TestApplyRejectsIncompleteApp(t *testing.T) {
	d := t.TempDir()
	bundle := filepath.Join(d, "milesshoot.app")
	fakeApp(t, bundle, "lama")
	os.MkdirAll(filepath.Join(d, "kosong.app"), 0o755)
	if err := Apply(filepath.Join(d, "kosong.app"), bundle, 1, filepath.Join(d, "l.log"), false); err == nil {
		t.Fatal(".app tanpa Contents/MacOS harus ditolak")
	}
}

func TestCurrentBundleOutsideApp(t *testing.T) {
	// binary test bukan di dalam .app → harus galat jelas, bukan menebak
	if _, err := CurrentBundle(); err == nil || !strings.Contains(err.Error(), ".app") {
		t.Fatalf("di luar .app harus galat: %v", err)
	}
}
