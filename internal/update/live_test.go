package update

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Uji NYATA terhadap rilis di GitHub: cek → unduh → verifikasi tanda tangan (kunci publik
// tertanam) → ekstrak → cari payload. Dilewati kecuali diminta:
//
//	MILESSHOOT_LIVE_REPO=pemilik/nama go test ./internal/update -run Live -v
func TestLiveRelease(t *testing.T) {
	repo := os.Getenv("MILESSHOOT_LIVE_REPO")
	if repo == "" {
		t.Skip("set MILESSHOOT_LIVE_REPO=pemilik/nama untuk menguji rilis sungguhan")
	}
	c, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rel, err := c.Check(ctx, "0.0.1") // versi palsu yang pasti lebih lama
	if err != nil || rel == nil {
		t.Fatalf("cek rilis: %v %v", rel, err)
	}
	t.Logf("rilis %s: %s (%d byte)", rel.Tag, rel.AssetName, rel.Size)
	dir := t.TempDir()
	zp, err := c.Download(ctx, rel, dir, nil)
	if err != nil {
		t.Fatalf("unduh/verifikasi: %v", err)
	}
	out := filepath.Join(dir, "new")
	if err := Extract(zp, out); err != nil {
		t.Fatal(err)
	}
	payload, err := FindPayload(out, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("payload: %s", filepath.Base(payload))
	if runtime.GOOS == "darwin" {
		for _, f := range []string{"Contents/MacOS/milesshoot", "Contents/MacOS/milesshoot-kamera", "Contents/Info.plist"} {
			if _, err := os.Stat(filepath.Join(payload, f)); err != nil {
				t.Errorf("rilis tidak memuat %s", f)
			}
		}
	}
}
