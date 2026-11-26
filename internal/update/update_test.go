package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, cur string
		want, bad   bool
	}{
		{"v1.2.0", "1.1.9", true, false},
		{"1.10.0", "1.9.0", true, false}, // numerik, bukan teks
		{"1.0.0", "1.0.0", false, false},
		{"v1.0.0", "1.0.1", false, false},
		{"2", "1.9.9", true, false},
		{"1.2.0+build5", "1.2.0", false, false},
		{"1.2.0-beta", "1.0.0", false, true}, // pra-rilis ditolak
		{"abc", "1.0.0", false, true},
		{"1.0.0", "dev", false, true},
	}
	for _, c := range cases {
		got, err := Newer(c.latest, c.cur)
		if (err != nil) != c.bad || got != c.want {
			t.Errorf("Newer(%q,%q) = %v,%v; mau %v bad=%v", c.latest, c.cur, got, err, c.want, c.bad)
		}
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

type fakeRelease struct {
	srv      *httptest.Server
	pub      ed25519.PublicKey
	priv     ed25519.PrivateKey
	zip      []byte
	sigTag   string // tag yang ditandatangani (bisa dibuat beda dari tag rilis)
	tag      string
	draft    bool
	noSig    bool
	status   int
	asset    string
	zipBytes func([]byte) []byte // untuk merusak isi zip setelah ditandatangani
	web      bool                // halaman web rilis (cadangan saat API kena batas) menjawab dengan benar
}

func newFake(t *testing.T, tag string) *fakeRelease {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	f := &fakeRelease{pub: pub, priv: priv, tag: tag, sigTag: tag, asset: AssetName("darwin", "arm64"),
		zip: makeZip(t, map[string]string{"milesshoot.app/Contents/MacOS/milesshoot": "baru"})}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		assets := []map[string]any{{"name": f.asset, "browser_download_url": f.srv.URL + "/dl/" + f.asset, "size": len(f.zip)}}
		if !f.noSig {
			assets = append(assets, map[string]any{"name": f.asset + ".sig", "browser_download_url": f.srv.URL + "/dl/" + f.asset + ".sig"})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "body": "Catatan rilis", "draft": f.draft, "assets": assets})
	})
	mux.HandleFunc("/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if !f.web {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, f.srv.URL+"/o/r/releases/tag/"+f.tag, http.StatusFound)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sig") {
			sum := sha256.Sum256(f.zip)
			sig := ed25519.Sign(f.priv, SignedMessage(f.sigTag, f.asset, hex.EncodeToString(sum[:])))
			w.Write([]byte(base64.StdEncoding.EncodeToString(sig)))
			return
		}
		body := f.zip
		if f.zipBytes != nil {
			body = f.zipBytes(body)
		}
		w.Write(body)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRelease) client() *Client {
	return &Client{Repo: "o/r", APIBase: f.srv.URL, WebBase: f.srv.URL, HTTP: f.srv.Client(), PubKey: f.pub, GOOS: "darwin", GOARCH: "arm64", Insecure: true}
}

func TestCheck(t *testing.T) {
	ctx := context.Background()
	f := newFake(t, "v1.1.0")
	if r, err := f.client().Check(ctx, "1.1.0"); r != nil || err != nil {
		t.Fatalf("sudah terbaru harus (nil,nil): %v %v", r, err)
	}
	r, err := f.client().Check(ctx, "1.0.0")
	if err != nil || r == nil || r.Version != "1.1.0" || r.Notes != "Catatan rilis" || r.AssetName != "milesshoot-darwin-arm64.zip" {
		t.Fatalf("rilis baru salah: %+v %v", r, err)
	}
	f.draft = true
	if r, err := f.client().Check(ctx, "1.0.0"); r != nil || err != nil {
		t.Fatal("draft harus diabaikan")
	}
	f.draft = false
	f.noSig = true
	if _, err := f.client().Check(ctx, "1.0.0"); err == nil || !strings.Contains(err.Error(), ".sig") {
		t.Fatalf("tanpa .sig harus galat jelas: %v", err)
	}
	f.noSig = false
	c := f.client()
	c.GOARCH = "riscv64"
	if _, err := c.Check(ctx, "1.0.0"); err == nil || !strings.Contains(err.Error(), "riscv64") {
		t.Fatalf("platform tanpa aset harus galat: %v", err)
	}
	f.status = 404
	if _, err := f.client().Check(ctx, "1.0.0"); err != ErrNotFound {
		t.Fatalf("404 → ErrNotFound, dapat %v", err)
	}
	f.status = 403
	if _, err := f.client().Check(ctx, "1.0.0"); err == nil || !strings.Contains(err.Error(), "membatasi") {
		t.Fatalf("403 → pesan rate limit: %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(""); err != ErrNoRepo {
		t.Errorf("repo kosong → ErrNoRepo, dapat %v", err)
	}
	if _, err := New("bukan repo"); err == nil {
		t.Error("repo tidak valid harus ditolak")
	}
	if c, err := New("kristiansnts/contoh"); err != nil || len(c.PubKey) != ed25519.PublicKeySize {
		t.Errorf("kunci publik tertanam harus valid: %v", err)
	}
}

func TestDownloadVerifies(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	f := newFake(t, "v1.1.0")
	rel, _ := f.client().Check(ctx, "1.0.0")
	var last int64
	path, err := f.client().Download(ctx, rel, dir, func(d, _ int64) { last = d })
	if err != nil || last != int64(len(f.zip)) {
		t.Fatalf("unduh sah gagal: %v (progres %d)", err, last)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, f.zip) {
		t.Fatal("isi berkas berbeda")
	}

	// isi zip diubah setelah ditandatangani
	f2 := newFake(t, "v1.1.0")
	f2.zipBytes = func(b []byte) []byte { return append(append([]byte{}, b...), 'X') }
	rel2, _ := f2.client().Check(ctx, "1.0.0")
	dir2 := t.TempDir()
	if _, err := f2.client().Download(ctx, rel2, dir2, nil); err != ErrBadSignature {
		t.Fatalf("zip dirusak harus ErrBadSignature, dapat %v", err)
	}
	if ents, _ := os.ReadDir(dir2); len(ents) != 0 {
		t.Errorf("berkas gagal verifikasi harus dihapus, tersisa %d", len(ents))
	}

	// zip sah versi LAMA dipakai ulang sebagai versi baru (replay/downgrade)
	f3 := newFake(t, "v2.0.0")
	f3.sigTag = "v1.0.0"
	rel3, _ := f3.client().Check(ctx, "1.5.0")
	if _, err := f3.client().Download(ctx, rel3, t.TempDir(), nil); err != ErrBadSignature {
		t.Fatalf("tanda tangan versi lain harus ditolak, dapat %v", err)
	}

	// ditandatangani kunci lain
	f4 := newFake(t, "v1.1.0")
	rel4, _ := f4.client().Check(ctx, "1.0.0")
	c4 := f4.client()
	c4.PubKey, _, _ = ed25519.GenerateKey(rand.Reader)
	if _, err := c4.Download(ctx, rel4, t.TempDir(), nil); err != ErrBadSignature {
		t.Fatalf("kunci asing harus ditolak, dapat %v", err)
	}
}

func TestDownloadRejectsInsecureURL(t *testing.T) {
	c := &Client{Repo: "o/r", PubKey: make(ed25519.PublicKey, ed25519.PublicKeySize)}
	_, err := c.Download(context.Background(), &Release{AssetName: "a.zip", AssetURL: "http://x/a.zip", SigURL: "http://x/a.zip.sig"}, t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "tidak aman") {
		t.Fatalf("http polos harus ditolak: %v", err)
	}
}

func TestExtractSafe(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "ok.zip")
	os.WriteFile(zp, makeZip(t, map[string]string{"A.app/Contents/MacOS/x": "isi", "__MACOSX/junk": "x", "A.app/Contents/MacOS/._x": "bayangan"}), 0o644)
	out := filepath.Join(dir, "out")
	if err := Extract(zp, out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "A.app/Contents/MacOS/x")); string(b) != "isi" {
		t.Fatal("isi tidak terekstrak")
	}
	if _, err := os.Stat(filepath.Join(out, "__MACOSX")); err == nil {
		t.Error("__MACOSX harus dilewati")
	}
	if _, err := os.Stat(filepath.Join(out, "A.app/Contents/MacOS/._x")); err == nil {
		t.Error("file bayangan ._* harus dilewati")
	}
	if app, err := FindApp(out); err != nil || filepath.Base(app) != "A.app" {
		t.Fatalf("FindApp: %s %v", app, err)
	}

	// zip-slip
	bad := filepath.Join(dir, "bad.zip")
	os.WriteFile(bad, makeZip(t, map[string]string{"../../evil.txt": "x"}), 0o644)
	if err := Extract(bad, filepath.Join(dir, "o2")); err == nil || !strings.Contains(err.Error(), "berbahaya") {
		t.Fatalf("path keluar dari tujuan harus ditolak: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
		t.Fatal("berkas berbahaya tertulis di luar folder tujuan!")
	}

	// symlink
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "A.app/link"}
	h.SetMode(os.ModeSymlink | 0o755)
	w, _ := zw.CreateHeader(h)
	fmt.Fprint(w, "/etc/passwd")
	zw.Close()
	sl := filepath.Join(dir, "sym.zip")
	os.WriteFile(sl, buf.Bytes(), 0o644)
	if err := Extract(sl, filepath.Join(dir, "o3")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink harus ditolak: %v", err)
	}

	// FindApp: harus tepat satu
	two := filepath.Join(dir, "two")
	os.MkdirAll(filepath.Join(two, "A.app"), 0o755)
	os.MkdirAll(filepath.Join(two, "B.app"), 0o755)
	if _, err := FindApp(two); err == nil {
		t.Error("dua .app harus ditolak")
	}
}

func TestFindPayloadWindows(t *testing.T) {
	d := t.TempDir()
	if _, err := FindPayload(d, "windows"); err == nil {
		t.Fatal("tanpa milesshoot.exe harus galat")
	}
	os.WriteFile(filepath.Join(d, "milesshoot.exe"), []byte("MZ"), 0o755)
	if p, err := FindPayload(d, "windows"); err != nil || filepath.Base(p) != "milesshoot.exe" {
		t.Fatalf("exe harus ditemukan: %s %v", p, err)
	}
	// zip rilis Windows yang dibuat skrip: exe langsung di tingkat atas
	zp := filepath.Join(d, "w.zip")
	os.WriteFile(zp, makeZip(t, map[string]string{"milesshoot.exe": "MZ"}), 0o644)
	out := filepath.Join(d, "out")
	if err := Extract(zp, out); err != nil {
		t.Fatal(err)
	}
	if _, err := FindPayload(out, "windows"); err != nil {
		t.Fatal(err)
	}
}

// Repo yang diganti namanya: GitHub menjawab 301 ke /repositories/<id>/...; harus tetap berhasil.
func TestCheckFollowsRepoRenameRedirect(t *testing.T) {
	f := newFake(t, "v1.1.0")
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/lama/nama/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/repos/o/r/releases/latest", http.StatusMovedPermanently)
	})
	mux.Handle("/", f.srv.Config.Handler)
	front := httptest.NewServer(mux)
	defer front.Close()
	c := f.client()
	c.Repo, c.APIBase = "lama/nama", front.URL
	rel, err := c.Check(context.Background(), "1.0.0")
	if err != nil || rel == nil || rel.Version != "1.1.0" {
		t.Fatalf("pengalihan 301 harus diikuti: %+v %v", rel, err)
	}
}

func TestCheckFallsBackToWebWhenRateLimited(t *testing.T) {
	ctx := context.Background()
	f := newFake(t, "v1.1.0")
	f.status = http.StatusForbidden
	// web tak tersedia → galat batas laju yang jelas
	if _, err := f.client().Check(ctx, "1.0.0"); err == nil || !strings.Contains(err.Error(), "membatasi") {
		t.Fatalf("harus galat batas laju: %v", err)
	}
	f.web = true
	r, err := f.client().Check(ctx, "1.0.0")
	if err != nil || r == nil || r.Version != "1.1.0" || r.Tag != "v1.1.0" ||
		!strings.HasSuffix(r.AssetURL, "/o/r/releases/download/v1.1.0/milesshoot-darwin-arm64.zip") ||
		!strings.HasSuffix(r.SigURL, "/o/r/releases/download/v1.1.0/milesshoot-darwin-arm64.zip.sig") {
		t.Fatalf("cadangan web salah: %+v %v", r, err)
	}
	if r, err := f.client().Check(ctx, "1.1.0"); r != nil || err != nil {
		t.Fatalf("sudah terbaru harus (nil,nil): %v %v", r, err)
	}
}
