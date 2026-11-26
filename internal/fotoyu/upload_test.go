package fotoyu

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Server FotoYu palsu: link → PUT → creations. createResult = isi "result" balasan pembuatan.
func fakeFotoyu(t *testing.T, createResult string) (*Client, *[]string) {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/gs/v3/creations/link", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"result": []map[string]any{{
			"original_link": srv.URL + "/put/abc", "signature": "sig", "upload_key": "k"}}})
	})
	mux.HandleFunc("/put/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/gs/v4/creations", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":` + createResult + `}`))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })

	dir := t.TempDir()
	acc := map[string]any{"accounts": []map[string]any{{
		"id": "u1", "identifier": "x", "access_token": "tok", "refresh_token": "r",
		"expires_at": 4102444800000, "user": map[string]any{"id": "u1", "username": "kalachitrasport", "currency_id": 1},
	}}, "activeId": "u1"}
	b, _ := json.Marshal(acc)
	os.WriteFile(filepath.Join(dir, "fotoyu-akun.json"), b, 0o600)

	var lines []string
	old2 := Logf
	Logf = func(level, msg string) { lines = append(lines, level+" "+msg) }
	t.Cleanup(func() { Logf = old2 })
	return New(dir), &lines
}

func photo() PhotoUpload {
	return PhotoUpload{Buffer: []byte("jpegdata"), Title: "IMG_1.jpg", Width: 10, Height: 20, Extension: "jpg", Mime: "image/jpeg",
		MD5: "m", Price: 25000, Tree: &Tree{ID: "t1", TagID: "tag-77", Name: "Lari Pagi"}}
}

func TestUploadPhotoLogsDestinationAndResponse(t *testing.T) {
	c, lines := fakeFotoyu(t, `[{"content_id":"c-123","quality":"hd"}]`)
	r, err := c.UploadPhoto(photo())
	if err != nil || r.RemoteID != "c-123" || r.Account != "kalachitrasport" || r.Quality != "hd" {
		t.Fatalf("unggah normal: %+v %v", r, err)
	}
	all := strings.Join(*lines, "\n")
	for _, want := range []string{"[FotoYu] IMG_1.jpg", "akun @kalachitrasport", "tag=[tag-77]", "harga=25000", "c-123"} {
		if !strings.Contains(all, want) {
			t.Errorf("log tidak memuat %q:\n%s", want, all)
		}
	}
}

// Balasan 2xx tanpa content_id = foto tidak tersimpan; jangan dianggap sukses.
func TestUploadPhotoWithoutContentIDIsFailure(t *testing.T) {
	for _, body := range []string{`[]`, `[{"quality":"hd"}]`, `null`, `{}`} {
		c, lines := fakeFotoyu(t, body)
		r, err := c.UploadPhoto(photo())
		fe, ok := err.(*Error)
		if err == nil || !ok || fe.Status != 502 || r.RemoteID != "" {
			t.Fatalf("balasan %s harus gagal (502): %+v %v", body, r, err)
		}
		if !strings.Contains(fe.Message, "tidak memberi ID konten") || fe.Detail == "" {
			t.Errorf("pesan/detail kurang jelas: %+v", fe)
		}
		if !strings.Contains(strings.Join(*lines, "\n"), "error [FotoYu] IMG_1.jpg: balasan sukses tanpa content_id") {
			t.Errorf("harus tercatat sebagai error: %v", *lines)
		}
	}
}
