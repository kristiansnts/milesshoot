// Package fotoyu adalah penghubung ke API FotoYu, disusun dari rekaman web fotoyu.com.
// Port dari src/fotoyu-api.js (MAS EDY). Model clone: 1 lisensi = 1 akun, jadi SATU akun per instalasi
// (versi asli mendukung banyak akun; di sini disederhanakan). Password tidak pernah disimpan —
// hanya token sesi di <dataDir>/fotoyu-akun.json. Bila FotoYu berubah, file inilah yang disesuaikan.
package fotoyu

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var apiBase = func() string {
	if v := os.Getenv("FOTOYU_API_BASE"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.fotoyu.com"
}()

// Kredensial klien "web" fotoyu.com (terlihat di kode web publiknya), dipakai untuk perpanjang token.
var webClientBasic = "Basic " + base64.StdEncoding.EncodeToString([]byte("web:5e4fdee1bf71db5c3568eb8ea25fd8a15c971a66b1fa83f92e4c11ade4e54d6d"))

var baseHeaders = map[string]string{
	"Accept":  "application/json, text/plain, */*",
	"Origin":  "https://www.fotoyu.com",
	"Referer": "https://www.fotoyu.com/",
}

// Error dari FotoYu membawa status HTTP + penanda alur (lihat classify di uploader).
type Error struct {
	Message   string
	Status    int
	Detail    string
	Duplicate string // own|other|recycle
	ContentID string
	Put       bool // gangguan saat PUT ke penyimpanan: cukup diulang
}

func (e *Error) Error() string { return e.Message }
func errf(status int, format string, a ...any) *Error {
	return &Error{Status: status, Message: fmt.Sprintf(format, a...)}
}

type User struct {
	ID         string          `json:"id"`
	Username   string          `json:"username"`
	Nickname   string          `json:"nickname"`
	FullName   string          `json:"full_name"`
	CurrencyID json.RawMessage `json:"currency_id"` // tipe tak pasti → diteruskan apa adanya
	Photo      string          `json:"photo"`
}

type account struct {
	ID           string `json:"id"`
	Identifier   string `json:"identifier"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	NeedsLogin   bool   `json:"needsLogin"`
	User         User   `json:"user"`
	AddedAt      int64  `json:"addedAt"`
	LastUsedAt   int64  `json:"lastUsedAt"`
}

type Location struct {
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Tree struct {
	ID       string   `json:"id"`
	TagID    string   `json:"tag_id"`
	Name     string   `json:"name"`
	Location Location `json:"location"`
	IsBib    bool     `json:"is_bib"`
}

// Beberapa akun FotoYu bisa disimpan sekaligus (mis. studio dengan 2 kreator) — semua
// tersimpan LOKAL di <dataDir>/fotoyu-akun.json (bukan database), persis seperti sebelum
// ada multi-akun. acct selalu menunjuk elemen accounts yang sedang aktif (activeID).
type Client struct {
	mu       sync.Mutex
	accounts []*account
	activeID string
	acct     *account // == elemen accounts dengan ID == activeID, atau nil
	file     string
	http     *http.Client

	refreshing sync.Map // id -> chan untuk single-flight refresh
}

type accountsFile struct {
	Accounts []*account `json:"accounts"`
	ActiveID string     `json:"activeId"`
}

func New(dataDir string) *Client {
	c := &Client{
		file: filepath.Join(dataDir, "fotoyu-akun.json"),
		http: &http.Client{},
	}
	c.load()
	return c
}

func (c *Client) load() {
	b, err := os.ReadFile(c.file)
	if err != nil {
		return
	}
	var f accountsFile
	if json.Unmarshal(b, &f) == nil && len(f.Accounts) > 0 {
		c.accounts = f.Accounts
		c.activeID = f.ActiveID
		c.syncActive()
		return
	}
	// Berkas format lama (satu akun tunggal, dari sebelum ada multi-akun) → migrasi
	// jadi daftar berisi satu akun, supaya instalasi lama tidak perlu masuk ulang.
	var a account
	if json.Unmarshal(b, &a) == nil && a.AccessToken != "" {
		c.accounts = []*account{&a}
		c.activeID = a.ID
		c.acct = &a
	}
}

// syncActive menyamakan c.acct dengan accounts[i].ID == activeID setelah accounts/activeID
// berubah (load awal, atau activeID yang tersimpan sudah tidak ada di daftar).
func (c *Client) syncActive() {
	c.acct = nil
	for _, a := range c.accounts {
		if a.ID == c.activeID {
			c.acct = a
			return
		}
	}
	if len(c.accounts) > 0 {
		c.acct = c.accounts[0]
		c.activeID = c.acct.ID
	}
}

func (c *Client) save() {
	b, _ := json.MarshalIndent(accountsFile{Accounts: c.accounts, ActiveID: c.activeID}, "", "  ")
	tmp := c.file + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, c.file)
	}
}

// removeByID mencabut satu akun dari daftar (dipanggil dengan mu terkunci). Kalau yang
// dicabut sedang aktif, akun lain (kalau ada) jadi aktif berikutnya.
func (c *Client) removeByID(id string) {
	out := c.accounts[:0]
	for _, a := range c.accounts {
		if a.ID != id {
			out = append(out, a)
		}
	}
	c.accounts = out
	if c.activeID == id {
		c.activeID = ""
		c.acct = nil
		if len(c.accounts) > 0 {
			c.activeID = c.accounts[0].ID
			c.acct = c.accounts[0]
		}
	}
}

// accountsLocked mengembalikan ringkasan semua akun tersimpan (dipanggil dengan mu terkunci).
func (c *Client) accountsLocked() []AccountInfo {
	out := make([]AccountInfo, 0, len(c.accounts))
	for _, a := range c.accounts {
		out = append(out, AccountInfo{SessionInfo: sessionOf(a), Active: a.ID == c.activeID})
	}
	return out
}

// AccountInfo = satu akun tersimpan + penanda apakah dia yang aktif sekarang.
type AccountInfo struct {
	SessionInfo
	Active bool `json:"active"`
}

// Accounts: semua akun FotoYu yang tersimpan lokal — dipakai dropdown "Akun" buat ganti akun.
func (c *Client) Accounts() []AccountInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accountsLocked()
}

// SwitchAccount mengaktifkan salah satu akun yang sudah tersimpan (tanpa perlu password lagi).
func (c *Client) SwitchAccount(id string) (SessionInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, a := range c.accounts {
		if a.ID == id {
			c.activeID = id
			c.acct = a
			c.save()
			return sessionOf(a), nil
		}
	}
	return SessionInfo{}, errf(404, "Akun tidak ditemukan")
}

// RemoveAccount mencabut satu akun tersimpan (mis. sudah tidak dipakai lagi).
func (c *Client) RemoveAccount(id string) []AccountInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removeByID(id)
	c.save()
	return c.accountsLocked()
}

type envelope struct {
	Result  json.RawMessage `json:"result"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail"`
	Paging  struct {
		NextCursor string `json:"next_cursor"`
	} `json:"paging"`
}

type callOpts struct {
	body    any
	auth    bool
	headers map[string]string
	retried bool
}

func (c *Client) call(method, path string, o callOpts) (*envelope, error) {
	headers := map[string]string{}
	for k, v := range baseHeaders {
		headers[k] = v
	}
	for k, v := range o.headers {
		headers[k] = v
	}
	var bodyReader io.Reader
	if o.body != nil {
		b, _ := json.Marshal(o.body)
		bodyReader = bytes.NewReader(b)
		headers["Content-Type"] = "application/json"
	}

	c.mu.Lock()
	acct := c.acct
	c.mu.Unlock()
	if o.auth {
		if acct == nil {
			return nil, errf(401, "Belum masuk akun FotoYu")
		}
		headers["Authorization"] = "Bearer " + acct.AccessToken
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, bodyReader)
	if err != nil {
		return nil, errf(0, "permintaan tidak valid: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, errf(0, "FotoYu tidak menjawab (server sibuk)")
		}
		return nil, errf(0, "Tidak bisa terhubung ke FotoYu (%v)", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var env envelope
	_ = json.Unmarshal(raw, &env)

	if res.StatusCode == 401 && o.auth && !o.retried && acct != nil {
		// Sesi habis: perpanjang token sekali (single-flight), lalu ulangi.
		if acct.RefreshToken != "" {
			switch c.refresh(acct) {
			case refreshOK:
				o.retried = true
				return c.call(method, path, o)
			case refreshBusy:
				return nil, errf(503, "FotoYu sedang sibuk — sesi belum bisa diperpanjang, dicoba lagi")
			}
		}
		c.mu.Lock()
		if c.acct == acct { // masih akun yang sama (belum dipindah akun lain selagi permintaan jalan)
			acct.NeedsLogin = true
			c.save()
		}
		c.mu.Unlock()
		return nil, errf(401, "Sesi akun @%s habis — masuk ulang", acct.User.Username)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &env, &Error{Status: res.StatusCode, Message: describeError(res.StatusCode, &env), Detail: string(env.Detail)}
	}
	if o.auth {
		c.mu.Lock()
		if c.acct != nil {
			c.acct.LastUsedAt = time.Now().UnixMilli()
		}
		c.mu.Unlock()
	}
	return &env, nil
}

func describeError(status int, env *envelope) string {
	m := env.Message
	if status == 401 {
		return "Sesi FotoYu habis — silakan masuk ulang"
	}
	if status == 403 && m == "sell not allowed" {
		return "Akun belum diizinkan menjual foto (cek verifikasi kreator)"
	}
	if m != "" {
		return fmt.Sprintf("FotoYu menolak (%d): %s", status, m)
	}
	return fmt.Sprintf("FotoYu menolak (%d)", status)
}

type refreshResult int

const (
	refreshFail refreshResult = iota
	refreshOK
	refreshBusy
)

// Satu permintaan perpanjang per akun: refresh token lama langsung tak berlaku setelah dipakai.
func (c *Client) refresh(acct *account) refreshResult {
	chAny, loaded := c.refreshing.LoadOrStore(acct.ID, make(chan refreshResult, 1))
	ch := chAny.(chan refreshResult)
	if loaded {
		r := <-ch
		ch <- r // teruskan ke penunggu lain
		return r
	}
	r := c.refreshOnce(acct)
	c.refreshing.Delete(acct.ID)
	ch <- r
	return r
}

func (c *Client) refreshOnce(acct *account) refreshResult {
	env, err := c.call("POST", "/oauth/v1/token", callOpts{
		auth:    false,
		headers: map[string]string{"Authorization": webClientBasic},
		body: map[string]any{
			"code_verifier": "testing",
			"grant_type":    "refresh_token",
			"refresh_token": acct.RefreshToken,
		},
		retried: true, // jangan rekursi refresh
	})
	if err != nil {
		if e, ok := err.(*Error); ok && (e.Status == 400 || e.Status == 401) {
			return refreshFail
		}
		return refreshBusy
	}
	var t struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	_ = json.Unmarshal(env.Result, &t)
	if t.AccessToken == "" {
		return refreshFail
	}
	c.mu.Lock()
	acct.AccessToken = t.AccessToken
	if t.RefreshToken != "" {
		acct.RefreshToken = t.RefreshToken
	}
	acct.ExpiresAt = time.Now().UnixMilli() + t.ExpiresIn*1000
	acct.NeedsLogin = false
	c.save()
	c.mu.Unlock()
	return refreshOK
}

// Ping mengukur jeda (ms) ke server FotoYu — dipakai buat kartu "Internet" di
// layar Unggah. Tidak peduli status HTTP-nya, cuma lama pulang-pergi koneksinya.
func (c *Client) Ping() (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, apiBase+"/", nil)
	if err != nil {
		return -1, err
	}
	start := time.Now()
	res, err := c.http.Do(req)
	if err != nil {
		return -1, err
	}
	defer res.Body.Close()
	return time.Since(start).Milliseconds(), nil
}

type SessionInfo struct {
	LoggedIn bool   `json:"loggedIn"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Photo    string `json:"photo"`
	ID       string `json:"id"`
}

func (c *Client) IsLoggedIn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.acct != nil && !c.acct.NeedsLogin
}

func (c *Client) ActiveUsername() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.acct != nil {
		return c.acct.User.Username
	}
	return ""
}

// sessionOf meringkas satu akun tersimpan jadi SessionInfo (dipakai Session() &
// daftar akun di Accounts()/AccountInfo).
func sessionOf(a *account) SessionInfo {
	u := a.User
	name := u.FullName
	if name == "" {
		name = u.Nickname
	}
	if name == "" {
		name = u.Username
	}
	return SessionInfo{LoggedIn: !a.NeedsLogin, Username: u.Username, Name: name, Photo: u.Photo, ID: a.ID}
}

func (c *Client) Session() SessionInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.acct == nil {
		return SessionInfo{}
	}
	return sessionOf(c.acct)
}

// Login masuk ke sebuah akun FotoYu dan menjadikannya aktif. Kalau akun ini (ID sama)
// sudah pernah disimpan sebelumnya, sesinya diperbarui di tempat alih-alih dobel —
// jadi "masuk akun lain" dan "masuk ulang akun yang sama" lewat jalur yang sama.
func (c *Client) Login(identifier, password string) (SessionInfo, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" || password == "" {
		return SessionInfo{}, errf(400, "Isi akun dan password")
	}
	env, err := c.call("POST", "/gs/v2/auth/login", callOpts{
		auth: false,
		body: map[string]any{"identifier": identifier, "password": password},
	})
	if err != nil {
		if e, ok := err.(*Error); ok && (e.Status == 400 || e.Status == 401 || e.Status == 404) {
			return SessionInfo{}, errf(e.Status, "Akun atau password salah")
		}
		return SessionInfo{}, err
	}
	var u struct {
		User
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	_ = json.Unmarshal(env.Result, &u)
	if u.AccessToken == "" {
		return SessionInfo{}, errf(202, "FotoYu meminta verifikasi tambahan. Masuk sekali lewat web FotoYu, lalu coba lagi.")
	}
	id := u.ID
	if id == "" {
		id = u.Username
	}
	na := &account{
		ID: id, Identifier: identifier, AccessToken: u.AccessToken, RefreshToken: u.RefreshToken,
		ExpiresAt: time.Now().UnixMilli() + u.ExpiresIn*1000, NeedsLogin: false,
		User:    u.User,
		AddedAt: time.Now().UnixMilli(),
	}
	c.mu.Lock()
	replaced := false
	for i, a := range c.accounts {
		if a.ID == id {
			na.AddedAt = a.AddedAt
			c.accounts[i] = na
			replaced = true
			break
		}
	}
	if !replaced {
		c.accounts = append(c.accounts, na)
	}
	c.activeID = id
	c.acct = na
	c.save()
	c.mu.Unlock()
	return c.Session(), nil
}

// Logout mencabut akun yang sedang aktif dari daftar tersimpan (bukan sekadar
// "nonaktifkan" — ini sign out sungguhan). Akun lain yang masih tersimpan, kalau ada,
// otomatis jadi aktif berikutnya; pakai SwitchAccount untuk pindah ke akun tertentu.
func (c *Client) Logout() {
	c.mu.Lock()
	c.removeByID(c.activeID)
	c.save()
	c.mu.Unlock()
}

// ---------- unggah foto: minta link → PUT ke S3 → terbitkan ----------

type PhotoUpload struct {
	Buffer      []byte
	Title       string
	Width       int
	Height      int
	Extension   string
	Mime        string
	MD5         string
	OriginalAt  time.Time
	Price       int
	Tree        *Tree
	Description string
	Nicknames   []string
}

type Result struct {
	RemoteID string
	Quality  string
	Account  string
}

func (c *Client) curAcct() (*account, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.acct == nil || c.acct.NeedsLogin {
		return nil, errf(401, "Belum memilih akun FotoYu")
	}
	return c.acct, nil
}

func (c *Client) UploadPhoto(p PhotoUpload) (Result, error) {
	acct, err := c.curAcct()
	if err != nil {
		return Result{}, err
	}
	env, err := c.call("POST", "/gs/v3/creations/link", callOpts{auth: true, body: map[string]any{
		"contents": []map[string]any{{
			"height": p.Height, "width": p.Width, "size": len(p.Buffer),
			"extension": p.Extension, "file": map[string]any{}, "signature": p.MD5,
		}},
	}})
	if err != nil {
		return Result{}, err
	}
	var items []linkItem
	_ = json.Unmarshal(env.Result, &items)
	if len(items) == 0 {
		return Result{}, errf(500, "FotoYu tidak memberi link upload")
	}
	item := items[0]
	if ref := linkRefusal(item); ref != nil {
		return Result{}, ref
	}
	if item.OriginalLink == "" {
		return Result{}, errf(500, "FotoYu tidak memberi link upload")
	}
	if err := c.putBuffer(item.OriginalLink, p.Buffer, p.Mime, p.Width, p.Height, "file"); err != nil {
		return Result{}, err
	}
	at := p.OriginalAt
	if at.IsZero() {
		at = time.Now()
	}
	sig := item.Signature
	if sig == "" {
		sig = p.MD5
	}
	env2, err := c.call("POST", "/gs/v4/creations", callOpts{auth: true, body: map[string]any{
		"currency_id":      rawOrNull(acct.User.CurrencyID),
		"location":         locationString(p.Tree),
		"original_at":      at.UTC().Format(time.RFC3339),
		"unix_original_at": at.Unix(),
		"description":      p.Description,
		"is_bibs":          bibs(p.Tree),
		"nickname":         p.Nicknames,
		"price":            p.Price,
		"resend":           true,
		"tag_ids":          tagIDs(p.Tree),
		"contents": []map[string]any{{
			"title": p.Title, "content": item.OriginalLink, "height": p.Height, "width": p.Width,
			"size": len(p.Buffer), "signature": sig, "upload_key": item.UploadKey,
		}},
	}})
	if err != nil {
		return Result{}, err
	}
	var created []struct {
		ContentID string `json:"content_id"`
		Quality   string `json:"quality"`
	}
	_ = json.Unmarshal(env2.Result, &created)
	r := Result{Account: acct.User.Username}
	if len(created) > 0 {
		r.RemoteID = created[0].ContentID
		r.Quality = created[0].Quality
	}
	return r, nil
}

// ---------- unggah video (zip 17 frame pratinjau) ----------

const videoPreviewMS = 4000

type VideoUpload struct {
	Zip         []byte
	Title       string
	Width       int
	Height      int
	MD5         string
	OriginalAt  time.Time
	Price       int
	Tree        *Tree
	Description string
	Nicknames   []string
}

func (c *Client) UploadVideo(v VideoUpload) (Result, error) {
	acct, err := c.curAcct()
	if err != nil {
		return Result{}, err
	}
	env, err := c.call("POST", "/gs/v3/creations/videos/link", callOpts{auth: true, body: map[string]any{
		"video":     map[string]any{"extension": "zip", "height": v.Height, "size": len(v.Zip), "width": v.Width, "duration": videoPreviewMS},
		"signature": v.MD5,
	}})
	if err != nil {
		return Result{}, err
	}
	// result bisa objek tunggal atau array
	var one linkItem
	if json.Unmarshal(env.Result, &one) != nil {
		var arr []linkItem
		_ = json.Unmarshal(env.Result, &arr)
		if len(arr) > 0 {
			one = arr[0]
		}
	}
	if ref := linkRefusal(one); ref != nil {
		return Result{}, ref
	}
	link := one.VideoLink
	if link == "" {
		link = one.Link
	}
	if link == "" {
		link = one.OriginalLink
	}
	if link == "" {
		return Result{}, errf(500, "FotoYu tidak memberi link upload video")
	}
	if err := c.putBuffer(link, v.Zip, "application/zip", v.Width, v.Height, "video"); err != nil {
		return Result{}, err
	}
	at := v.OriginalAt
	if at.IsZero() {
		at = time.Now()
	}
	exif, _ := json.Marshal(map[string]any{"duration": videoPreviewMS})
	env2, err := c.call("POST", "/gs/v2/creations/videos", callOpts{auth: true, body: map[string]any{
		"resend": true, "title": v.Title, "description": v.Description, "content": link,
		"currency_id": rawOrNull(acct.User.CurrencyID), "height": v.Height, "is_parent": true,
		"location": locationString(v.Tree), "original_at": at.UTC().Format(time.RFC3339),
		"size": len(v.Zip), "unix_original_at": at.Unix(), "width": v.Width,
		"exif": string(exif), "nickname": strings.Join(v.Nicknames, ","),
		"price": v.Price, "tag_ids": tagIDs(v.Tree), "signature": v.MD5,
	}})
	if err != nil {
		return Result{}, err
	}
	var created []struct {
		ContentID string `json:"content_id"`
		ID        string `json:"id"`
	}
	r := Result{Account: acct.User.Username}
	if json.Unmarshal(env2.Result, &created) == nil && len(created) > 0 {
		r.RemoteID = created[0].ContentID
		if r.RemoteID == "" {
			r.RemoteID = created[0].ID
		}
	}
	return r, nil
}

type linkItem struct {
	OriginalLink string `json:"original_link"`
	VideoLink    string `json:"video_link"`
	Link         string `json:"link"`
	Signature    string `json:"signature"`
	UploadKey    string `json:"upload_key"`
	Error        string `json:"error"`
	IsOwn        bool   `json:"is_own"`
	ContentID    string `json:"content_id"`
}

// Aturan Anti-Spam Duplikat FotoYu dikenali di langkah minta-link, sebelum file dikirim.
func linkRefusal(item linkItem) *Error {
	switch item.Error {
	case "":
		return nil
	case "content: duplicate records":
		if item.IsOwn {
			return &Error{Status: 409, Message: "Sudah ada di akun FotoYu ini", Duplicate: "own", ContentID: item.ContentID}
		}
		return &Error{Status: 409, Message: "Duplikat: file ini sudah diunggah akun lain (dilarang FotoYu)", Duplicate: "other", ContentID: item.ContentID}
	case "content: reupload":
		return &Error{Status: 409, Message: "Daur ulang: foto ini pernah dihapus dari FotoYu, tidak boleh diunggah lagi", Duplicate: "recycle"}
	default:
		return &Error{Status: 422, Message: "FotoYu menolak: " + item.Error}
	}
}

// Kirim isi file ke link penyimpanan (S3). Batas waktu ≥60 dtk + 1 dtk per 40 KB.
func (c *Client) putBuffer(link string, buf []byte, mime string, width, height int, what string) error {
	timeout := time.Duration(60+len(buf)/(40*1024)) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "PUT", link, bytes.NewReader(buf))
	if err != nil {
		return &Error{Status: 0, Message: "link tidak valid", Put: true}
	}
	req.Header.Set("Content-Type", mime)
	req.ContentLength = int64(len(buf))
	req.Header.Set("x-amz-meta-imagewidth", strconv.Itoa(width))
	req.Header.Set("x-amz-meta-imageheight", strconv.Itoa(height))
	res, err := c.http.Do(req)
	if err != nil {
		msg := fmt.Sprintf("Gagal mengirim %s (%v)", what, err)
		if ctx.Err() == context.DeadlineExceeded {
			msg = fmt.Sprintf("Kirim %s terlalu lama (sinyal lemah), diulang", what)
		}
		return &Error{Status: 0, Message: msg, Put: true}
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		st := 0
		if res.StatusCode >= 500 {
			st = res.StatusCode
		}
		return &Error{Status: st, Message: fmt.Sprintf("Link kirim %s ditolak (%d), diulang dengan link baru", what, res.StatusCode), Put: true}
	}
	return nil
}

// ---------- helper body ----------

func locationString(t *Tree) string {
	name := ""
	var lat, lng float64
	if t != nil {
		name = t.Name
		lat = t.Location.Latitude
		lng = t.Location.Longitude
	}
	nb, _ := json.Marshal(name)
	return fmt.Sprintf(`{"name":%s, "longitude":%v, "latitude":%v}`, nb, lng, lat)
}

func tagIDs(t *Tree) []string {
	if t == nil {
		return []string{}
	}
	return []string{t.TagID}
}

func bibs(t *Tree) []map[string]any {
	if t == nil {
		return []map[string]any{}
	}
	return []map[string]any{{"tag_id": t.TagID, "is_bib": t.IsBib}}
}

func rawOrNull(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	return r
}
