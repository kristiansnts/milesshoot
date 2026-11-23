package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Pengaturan aplikasi, disimpan di <dataDir>/config.json. Port dari config.js.
// Aman diakses banyak goroutine (scanner, queue, server HP) lewat RWMutex.

type Compress struct {
	Size int `json:"size"` // 1440 (standar FotoYu) atau 4096 (ori)
}

type VideoCfg struct {
	Enabled        bool   `json:"enabled"`
	Orientation    string `json:"orientation"` // auto|0|90|180|270
	Confirm        bool   `json:"confirm"`     // tunggu orientasi dicek
	RotateOriginal bool   `json:"rotateOriginal"`
}

type Tree struct {
	ID       string   `json:"id"`
	TagID    string   `json:"tag_id"`
	Name     string   `json:"name"`
	Location Location `json:"location"`
	IsBib    bool     `json:"is_bib"`
}

type Location struct {
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Event struct {
	Name        string   `json:"name"`
	Price       int      `json:"price"`
	VideoPrice  int      `json:"videoPrice"` // 0 = sama dengan harga foto
	Location    string   `json:"location"`
	Tags        string   `json:"tags"`
	Tree        *Tree    `json:"tree"`
	Description string   `json:"description"`
	Nicknames   []string `json:"nicknames"`
}

type LUT struct {
	Enabled  bool   `json:"enabled"`
	File     string `json:"file"`
	Photo    string `json:"photo"`
	Video    string `json:"video"`
	Strength int    `json:"strength"`
}

type FTP struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Pass    string `json:"-"` // TIDAK ikut ke JSON publik (pelajaran dari kebocoran app asli)
}

type AfterUpload struct {
	Move        bool   `json:"move"`
	Folder      string `json:"folder"`
	FolderOdd   string `json:"folderOdd"`
	FolderEven  string `json:"folderEven"`
	FolderVideo string `json:"folderVideo"`
}

type Parity struct {
	Mode string `json:"mode"` // all|odd|even
	Only bool   `json:"only"`
}

type Hires struct {
	Folders     []string `json:"folders"`
	Auto        bool     `json:"auto"`
	LUT         string   `json:"lut"`
	LUTStrength int      `json:"lutStrength"`
	AutoTone    bool     `json:"autoTone"`
}

type Watermark struct {
	Enabled  bool   `json:"enabled"`
	Type     string `json:"type"` // text|logo
	Text     string `json:"text"`
	Pos      string `json:"pos"`  // br|bl|tr|tl|c
	Size     int    `json:"size"` // persen lebar foto
	Opacity  int    `json:"opacity"`
	LogoPath string `json:"logoPath"`
}

// UsbCam: kamera kabel (PTP/tethering) lewat program pembantu native.
type UsbCam struct {
	Enabled    bool `json:"enabled"`
	AutoSource bool `json:"autoSource"` // kamera tersambung → folder unduhan jadi sumber foto
}

// Preset: simpan event + harga + deskripsi + nickname, pakai lagi sekali ketuk.
type Preset struct {
	Name     string   `json:"name"`
	Event    Event    `json:"event"`
	Compress Compress `json:"compress"`
}

type Settings struct {
	WatchFolder       string      `json:"watchFolder"`
	WatchFolders      []string    `json:"watchFolders"` // folder sumber TAMBAHAN — dipindai konkuren bersama WatchFolder (primary)
	IncludeSubfolders bool        `json:"includeSubfolders"`
	Destination       string      `json:"destination"` // local|fotoyu
	LocalOutputFolder string      `json:"localOutputFolder"`
	MaxRetries        int         `json:"maxRetries"`
	Performance       string      `json:"performance"` // auto|hemat|seimbang|ngebut
	Compress          Compress    `json:"compress"`
	Video             VideoCfg    `json:"video"`
	Event             Event       `json:"event"`
	AutoUpload        bool        `json:"autoUpload"`
	UploadOnly        bool        `json:"uploadOnly"`
	UploadOrder       string      `json:"uploadOrder"` // newest|oldest
	Theme             string      `json:"theme"`       // dark|light|system
	LUT               LUT         `json:"lut"`
	FTP               FTP         `json:"ftp"`
	AfterUpload       AfterUpload `json:"afterUpload"`
	Parity            Parity      `json:"parity"`
	Hires             Hires       `json:"hires"`
	Watermark         Watermark   `json:"watermark"`
	Presets           []Preset    `json:"presets"`
	UsbCam            UsbCam      `json:"usbCam"`
	Debug             bool        `json:"debug"` // catat detail (tiap perintah FTP dll) ke log
	ScanIntervalSec   int         `json:"scanIntervalSec"`
}

func defaults(dataDir string) Settings {
	return Settings{
		WatchFolders:      []string{},
		IncludeSubfolders: true,
		Destination:       "local",
		LocalOutputFolder: filepath.Join(dataDir, "hasil-upload"),
		MaxRetries:        5,
		Performance:       "auto",
		Compress:          Compress{Size: 1440},
		Video:             VideoCfg{Enabled: true, Orientation: "auto", Confirm: false, RotateOriginal: true},
		Event:             Event{Price: 15000, Nicknames: []string{}},
		AutoUpload:        true,
		UploadOrder:       "newest",
		Theme:             "dark",
		LUT:               LUT{Strength: 100},
		FTP:               FTP{Enabled: true, Port: 21, User: "masedy"},
		AfterUpload:       AfterUpload{Move: true, Folder: "Sudah upload", FolderOdd: "Sudah upload ganjil", FolderEven: "Sudah upload genap", FolderVideo: "Sudah upload video"},
		Parity:            Parity{Mode: "all"},
		Hires:             Hires{Folders: []string{}, LUTStrength: 100},
		Watermark:         Watermark{Type: "text", Pos: "br", Size: 22, Opacity: 60},
		UsbCam:            UsbCam{Enabled: true, AutoSource: true},
		Presets:           []Preset{},
		ScanIntervalSec:   2,
	}
}

type Config struct {
	mu      sync.RWMutex
	cur     Settings
	dataDir string
	file    string
}

func LoadConfig(dataDir string) (*Config, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	c := &Config{dataDir: dataDir, file: filepath.Join(dataDir, "config.json")}
	c.cur = defaults(dataDir)
	if b, err := os.ReadFile(c.file); err == nil {
		// Merge di atas default: field yang tak ada di file tetap memakai default.
		_ = json.Unmarshal(b, &c.cur)
	}
	// Password FTP acak 6 angka, dibuat sekali lalu disimpan terpisah (bukan di config.json publik).
	if c.cur.FTP.Pass == "" {
		c.cur.FTP.Pass = c.loadOrMakeFTPPass()
	}
	c.normalize()
	c.save()
	return c, nil
}

// Password FTP disimpan di file terpisah ber-permission 0600, tidak pernah masuk config.json
// yang bisa terbaca UI/jaringan.
func (c *Config) loadOrMakeFTPPass() string {
	pf := filepath.Join(c.dataDir, "ftp-pass")
	if b, err := os.ReadFile(pf); err == nil && len(b) > 0 {
		return strings.TrimSpace(string(b))
	}
	n := 100000 + int(randInt(900000))
	pass := strconv.Itoa(n)
	_ = os.WriteFile(pf, []byte(pass), 0o600)
	return pass
}

func (c *Config) Get() Settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cur
}

// Update menerima JSON patch (dari UI) dan menimpa hanya field yang ada.
func (c *Config) Update(patch []byte) (Settings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := json.Unmarshal(patch, &c.cur); err != nil {
		return c.cur, err
	}
	c.normalize()
	c.save()
	return c.cur, nil
}

func (c *Config) Set(mutate func(*Settings)) Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	mutate(&c.cur)
	c.normalize()
	c.save()
	return c.cur
}

func (c *Config) DataDir() string { return c.dataDir }

func (c *Config) normalize() {
	s := &c.cur
	if s.Compress.Size != 4096 {
		s.Compress.Size = 1440
	}
	if !oneOf(s.Performance, "auto", "hemat", "seimbang", "ngebut") {
		s.Performance = "auto"
	}
	if !oneOf(s.Video.Orientation, "auto", "0", "90", "180", "270") {
		s.Video.Orientation = "auto"
	}
	// "Cek orientasi dulu" butuh pratinjau+konfirmasi manual yang belum dikerjakan — tak ada
	// jalan buat melepas video yang ditahan (OriOK tak pernah diisi true). Paksa mati sampai
	// alur konfirmasinya ada, supaya tak ada video yang diam-diam nyangkut selamanya.
	s.Video.Confirm = false
	if !oneOf(s.Theme, "dark", "light", "system") {
		s.Theme = "dark"
	}
	if !oneOf(s.UploadOrder, "newest", "oldest") {
		s.UploadOrder = "newest"
	}
	if !oneOf(s.Parity.Mode, "all", "odd", "even") {
		s.Parity.Mode = "all"
	}
	if s.Parity.Mode == "all" {
		s.Parity.Only = false
	}
	s.MaxRetries = clamp(s.MaxRetries, 0, 20)
	s.LUT.Strength = clamp(s.LUT.Strength, 0, 100)
	s.Hires.LUTStrength = clamp(s.Hires.LUTStrength, 0, 100)
	if s.Event.Nicknames == nil {
		s.Event.Nicknames = []string{}
	}
	// nickname: buang "@", kosong, dan batasi 20
	{
		out := s.Event.Nicknames[:0]
		for _, n := range s.Event.Nicknames {
			n = strings.TrimPrefix(strings.TrimSpace(n), "@")
			if n != "" && len(out) < 20 {
				out = append(out, n)
			}
		}
		s.Event.Nicknames = out
	}
	if s.Event.Price < 0 {
		s.Event.Price = 0
	}
	if s.Event.VideoPrice < 0 {
		s.Event.VideoPrice = 0
	}
	if len(s.Event.Description) > 500 {
		s.Event.Description = s.Event.Description[:500]
	}
	if s.Presets == nil {
		s.Presets = []Preset{}
	}
	{
		out := s.Presets[:0]
		for _, p := range s.Presets {
			if strings.TrimSpace(p.Name) != "" && len(out) < 30 {
				out = append(out, p)
			}
		}
		s.Presets = out
	}
	// --- folder sumber tambahan: tanpa duplikat satu sama lain atau folder utama ---
	if s.WatchFolders == nil {
		s.WatchFolders = []string{}
	}
	{
		seen := map[string]bool{s.WatchFolder: true}
		out := []string{}
		for _, f := range s.WatchFolders {
			f = strings.TrimSpace(f)
			if f != "" && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
		s.WatchFolders = out
	}

	// --- watermark ---
	if !oneOf(s.Watermark.Type, "text", "logo") {
		s.Watermark.Type = "text"
	}
	if !oneOf(s.Watermark.Pos, "br", "bl", "tr", "tl", "c") {
		s.Watermark.Pos = "br"
	}
	s.Watermark.Size = clamp(s.Watermark.Size, 5, 60)
	s.Watermark.Opacity = clamp(s.Watermark.Opacity, 5, 100)
	s.Watermark.Text = strings.TrimSpace(s.Watermark.Text)
	if len(s.Watermark.Text) > 120 {
		s.Watermark.Text = s.Watermark.Text[:120]
	}
	// tanpa teks (mode text) atau tanpa logo (mode logo) → tidak ada apa pun untuk digambar
	if (s.Watermark.Type == "text" && s.Watermark.Text == "") ||
		(s.Watermark.Type == "logo" && s.Watermark.LogoPath == "") {
		s.Watermark.Enabled = false
	}

	s.AfterUpload.Folder = cleanFolderName(s.AfterUpload.Folder, "Sudah upload")
	s.AfterUpload.FolderOdd = cleanFolderName(s.AfterUpload.FolderOdd, "Sudah upload ganjil")
	s.AfterUpload.FolderEven = cleanFolderName(s.AfterUpload.FolderEven, "Sudah upload genap")
	s.AfterUpload.FolderVideo = cleanFolderName(s.AfterUpload.FolderVideo, "Sudah upload video")
}

func (c *Config) save() {
	tmp := c.file + ".tmp"
	b, _ := json.MarshalIndent(c.cur, "", "  ")
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		_ = os.Rename(tmp, c.file)
	}
}

var (
	badFolderRe = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)
	lastNumRe   = regexp.MustCompile(`(\d+)(?:\D*)$`)
)

func cleanFolderName(name, fallback string) string {
	n := badFolderRe.ReplaceAllString(name, " ")
	n = strings.Join(strings.Fields(n), " ")
	n = strings.Trim(n, ". ")
	if len(n) > 60 {
		n = n[:60]
	}
	n = strings.TrimSpace(n)
	if n == "" {
		return fallback
	}
	return n
}

// ParityOf: ganjil/genap dari angka terakhir di nama file (DSC_0123 -> odd). "" bila tak ada angka.
func ParityOf(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		base = base[:len(base)-len(ext)]
	}
	m := lastNumRe.FindStringSubmatch(base)
	if m == nil {
		return ""
	}
	num := m[1]
	if len(num) > 6 {
		num = num[len(num)-6:]
	}
	n, _ := strconv.Atoi(num)
	if n%2 == 1 {
		return "odd"
	}
	return "even"
}

func oneOf(v string, opts ...string) bool {
	for _, o := range opts {
		if v == o {
			return true
		}
	}
	return false
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
