// Package update: pembaruan otomatis lewat GitHub Releases.
//
// Alur: Check (GET rilis terbaru) → Download (zip + tanda tangan, diverifikasi) → Extract (aman) →
// Apply (ganti aplikasi setelah proses lama keluar; lihat apply_*.go).
//
// Keamanan: berkas hanya dipasang bila tanda tangan ed25519-nya cocok dengan kunci publik yang
// tertanam di aplikasi (PublicKeyB64). Pesan yang ditandatangani mengikat nomor versi, nama aset,
// dan sha256 berkas, jadi zip lama yang valid tidak bisa dipasang sebagai versi baru.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	sigContext   = "milesshoot-update-v1"
	maxZipBytes  = 400 << 20
	maxSigBytes  = 4 << 10
	maxAPIBytes  = 2 << 20
	defaultAPI   = "https://api.github.com"
	defaultWeb   = "https://github.com"
	userAgent    = "milesshoot-updater"
	maxNotesRune = 4000
)

var (
	ErrNoRepo       = errors.New("alamat repo pembaruan belum diatur di build ini")
	ErrNotFound     = errors.New("rilis tidak ditemukan (belum ada rilis, atau repo bersifat privat)")
	ErrBadSignature = errors.New("tanda tangan pembaruan tidak cocok — berkas ditolak")
	repoRe          = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
)

// Release: rilis terbaru yang lebih baru dari versi terpasang.
type Release struct {
	Tag       string
	Version   string
	Notes     string
	Published time.Time
	AssetName string
	AssetURL  string
	SigURL    string
	Size      int64
}

type Client struct {
	Repo     string
	APIBase  string // bawaan https://api.github.com
	WebBase  string // bawaan https://github.com (cadangan saat API kena batas laju)
	HTTP     *http.Client
	PubKey   ed25519.PublicKey
	GOOS     string
	GOARCH   string
	Insecure bool // hanya untuk test (httptest memakai http://)
}

// New membuat klien untuk repo "pemilik/nama" dengan kunci publik tertanam.
func New(repo string) (*Client, error) {
	if repo == "" {
		return nil, ErrNoRepo
	}
	if !repoRe.MatchString(repo) {
		return nil, fmt.Errorf("alamat repo pembaruan %q tidak valid (harus pemilik/nama)", repo)
	}
	raw, err := base64.StdEncoding.DecodeString(PublicKeyB64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("kunci publik pembaruan di build ini tidak valid")
	}
	return &Client{Repo: repo, APIBase: defaultAPI, WebBase: defaultWeb, HTTP: &http.Client{}, PubKey: ed25519.PublicKey(raw),
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}, nil
}

// AssetName: nama zip rilis untuk satu platform, mis. milesshoot-darwin-arm64.zip.
func AssetName(goos, goarch string) string { return fmt.Sprintf("milesshoot-%s-%s.zip", goos, goarch) }

// SignedMessage: yang ditandatangani saat rilis dan diverifikasi saat pasang.
func SignedMessage(tag, asset, sha256hex string) []byte {
	return []byte(sigContext + "\n" + tag + "\n" + asset + "\n" + sha256hex)
}

type ghRelease struct {
	TagName    string    `json:"tag_name"`
	Body       string    `json:"body"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published_at"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (c *Client) get(ctx context.Context, rawURL string, accept string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && !(c.Insecure && u.Scheme == "http")) {
		return nil, fmt.Errorf("alamat unduhan tidak aman: %q", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	cl := *hc
	cl.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("terlalu banyak pengalihan")
		}
		if r.URL.Scheme != "https" && !c.Insecure {
			return errors.New("pengalihan ke alamat tidak aman ditolak")
		}
		return nil
	}
	return cl.Do(req)
}

// Check menanyakan rilis terbaru. (nil, nil) = sudah yang terbaru.
func (c *Client) Check(ctx context.Context, current string) (*Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	api := c.APIBase
	if api == "" {
		api = defaultAPI
	}
	res, err := c.get(ctx, api+"/repos/"+c.Repo+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("tidak bisa menghubungi GitHub: %w", err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests:
		// API tanpa token dibatasi 60 permintaan/jam per IP — di jaringan bersama (kantor, seluler
		// CGNAT) cepat habis. Cadangan: halaman web rilis tidak memakai batas API itu.
		if rel, werr := c.checkWeb(ctx, current); werr == nil || errors.Is(werr, ErrNotFound) {
			return rel, werr
		}
		return nil, fmt.Errorf("GitHub membatasi permintaan (HTTP %d) — coba lagi nanti", res.StatusCode)
	case res.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub menjawab HTTP %d", res.StatusCode)
	}
	var gr ghRelease
	if err := json.NewDecoder(io.LimitReader(res.Body, maxAPIBytes)).Decode(&gr); err != nil {
		return nil, fmt.Errorf("balasan GitHub tidak terbaca: %w", err)
	}
	if gr.Draft || gr.Prerelease {
		return nil, nil
	}
	newer, err := Newer(gr.TagName, current)
	if err != nil {
		return nil, err
	}
	if !newer {
		return nil, nil
	}
	want := AssetName(c.GOOS, c.GOARCH)
	rel := &Release{Tag: gr.TagName, Version: strings.TrimPrefix(strings.TrimPrefix(gr.TagName, "v"), "V"),
		Notes: truncate(strings.TrimSpace(gr.Body), maxNotesRune), Published: gr.Published, AssetName: want}
	for _, a := range gr.Assets {
		switch a.Name {
		case want:
			rel.AssetURL, rel.Size = a.URL, a.Size
		case want + ".sig":
			rel.SigURL = a.URL
		}
	}
	if rel.AssetURL == "" || rel.SigURL == "" {
		return nil, fmt.Errorf("rilis %s tidak punya berkas %s beserta .sig untuk %s/%s", gr.TagName, want, c.GOOS, c.GOARCH)
	}
	return rel, nil
}

var tagRe = regexp.MustCompile(`^[vV]?[0-9]+(\.[0-9]+)*$`)

// checkWeb: cadangan tanpa API. /releases/latest dialihkan GitHub ke /releases/tag/<tag>, dari situ
// tag dan alamat unduhan (pola tetap /releases/download/<tag>/<berkas>) bisa dibentuk. Catatan rilis
// tidak ikut (hanya ada di API). Keaslian tetap dijaga tanda tangan, yang mencakup tag + berkas.
func (c *Client) checkWeb(ctx context.Context, current string) (*Release, error) {
	web := c.WebBase
	if web == "" {
		web = defaultWeb
	}
	latest := web + "/" + c.Repo + "/releases/latest"
	u, err := url.Parse(latest)
	if err != nil || (u.Scheme != "https" && !(c.Insecure && u.Scheme == "http")) {
		return nil, fmt.Errorf("alamat tidak aman: %q", latest)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latest, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	cl := *hc
	cl.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	loc := res.Header.Get("Location")
	i := strings.LastIndex(loc, "/releases/tag/")
	if res.StatusCode < 300 || res.StatusCode >= 400 || i < 0 {
		return nil, fmt.Errorf("GitHub (web) menjawab HTTP %d", res.StatusCode)
	}
	tag := loc[i+len("/releases/tag/"):]
	if !tagRe.MatchString(tag) {
		return nil, fmt.Errorf("tag rilis %q tidak dikenali", tag)
	}
	newer, err := Newer(tag, current)
	if err != nil {
		return nil, err
	}
	if !newer {
		return nil, nil
	}
	want := AssetName(c.GOOS, c.GOARCH)
	base := web + "/" + c.Repo + "/releases/download/" + tag + "/"
	return &Release{Tag: tag, Version: strings.TrimPrefix(strings.TrimPrefix(tag, "v"), "V"),
		Published: time.Now(), AssetName: want, AssetURL: base + want, SigURL: base + want + ".sig"}, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Download mengunduh zip + tanda tangannya ke dir, lalu memverifikasi SEBELUM mengembalikan path.
// Berkas yang gagal verifikasi dihapus.
func (c *Client) Download(ctx context.Context, rel *Release, dir string, progress func(done, total int64)) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	sigRes, err := c.get(ctx, rel.SigURL, "")
	if err != nil {
		return "", fmt.Errorf("tanda tangan tidak bisa diunduh: %w", err)
	}
	sigB64, _ := io.ReadAll(io.LimitReader(sigRes.Body, maxSigBytes))
	sigRes.Body.Close()
	if sigRes.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tanda tangan tidak bisa diunduh (HTTP %d)", sigRes.StatusCode)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return "", ErrBadSignature
	}

	res, err := c.get(ctx, rel.AssetURL, "application/octet-stream")
	if err != nil {
		return "", fmt.Errorf("unduhan gagal: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unduhan gagal (HTTP %d)", res.StatusCode)
	}
	total := res.ContentLength
	if total > maxZipBytes {
		return "", fmt.Errorf("berkas pembaruan terlalu besar (%d MB)", total>>20)
	}

	name := filepath.Base(rel.AssetName)
	part := filepath.Join(dir, name+".part")
	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > maxZipBytes {
				f.Close()
				os.Remove(part)
				return "", errors.New("berkas pembaruan melewati batas ukuran")
			}
			h.Write(buf[:n])
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(part)
				return "", werr
			}
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(part)
			return "", fmt.Errorf("unduhan terputus: %w", rerr)
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return "", err
	}
	if total > 0 && done != total {
		os.Remove(part)
		return "", fmt.Errorf("unduhan tidak lengkap (%d dari %d byte)", done, total)
	}
	if !ed25519.Verify(c.PubKey, SignedMessage(rel.Tag, rel.AssetName, hex.EncodeToString(h.Sum(nil))), sig) {
		os.Remove(part)
		return "", ErrBadSignature
	}
	final := filepath.Join(dir, name)
	if err := os.Rename(part, final); err != nil {
		os.Remove(part)
		return "", err
	}
	return final, nil
}
