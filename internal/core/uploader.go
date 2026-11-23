package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"fotonaik/internal/fotoyu"
)

// UploadError membawa penanda yang menentukan perlakuan antrean (port dari flag di queue.js/fotoyu.js).
type UploadError struct {
	Msg           string
	Status        int
	Pause         bool   // masalah akun/pengaturan → jeda seluruh antrean
	Account       bool   // 401 → perlu masuk ulang
	Permanent     bool   // jangan diulang
	Duplicate     string // own|other|recycle
	Local         bool   // olah gambar gagal sesaat → ulang foto ini saja
	Put           bool   // gangguan kirim ke penyimpanan → cukup diulang
	Missing       bool   // file asli hilang
	SessionReject bool
	ContentID     string
	Detail        string
}

func (e *UploadError) Error() string { return e.Msg }

type Meta struct {
	Event        string
	Price        int
	Rule         string
	OriginalName string
	MD5          string
	FilePath     string
	OriginalAt   time.Time
	Tree         *Tree
	LUT          bool
}

type PhotoJob struct {
	Buffer   []byte
	FileName string
	Width    int
	Height   int
	Meta     Meta
	Cfg      Settings
}

type PreparedVideo struct {
	Zip        []byte
	Width      int
	Height     int
	Duration   int
	FirstFrame []byte
	OriginalAt time.Time
	Frames     int
}

type VideoJob struct {
	Video PreparedVideo
	Meta  Meta
	Cfg   Settings
}

type UploadResult struct {
	RemoteID string
	Account  string
	Quality  string
}

type Uploader interface {
	ID() string
	Upload(PhotoJob) (UploadResult, error)
	UploadVideo(VideoJob) (UploadResult, error)
	Test(Settings) (bool, string)
}

// ---------- tujuan "lokal" (uji coba): salin hasil olah ke folder ----------

type LocalUploader struct{}

func (LocalUploader) ID() string { return "local" }

func (LocalUploader) Test(cfg Settings) (bool, string) {
	if err := os.MkdirAll(cfg.LocalOutputFolder, 0o755); err != nil {
		return false, err.Error()
	}
	return true, "Folder tujuan siap: " + cfg.LocalOutputFolder
}

var badName = regexp.MustCompile(`[\\/:*?"<>|]`)

func (LocalUploader) Upload(j PhotoJob) (UploadResult, error) {
	ev := j.Meta.Event
	if ev == "" {
		ev = "tanpa-event"
	}
	dir := filepath.Join(j.Cfg.LocalOutputFolder, badName.ReplaceAllString(ev, "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return UploadResult{}, &UploadError{Msg: err.Error(), Local: true}
	}
	target := filepath.Join(dir, j.FileName)
	if err := os.WriteFile(target, j.Buffer, 0o644); err != nil {
		return UploadResult{}, &UploadError{Msg: err.Error(), Local: true}
	}
	metaB, _ := json.MarshalIndent(j.Meta, "", "  ")
	_ = os.WriteFile(target+".json", metaB, 0o644)
	return UploadResult{RemoteID: target}, nil
}

func (LocalUploader) UploadVideo(j VideoJob) (UploadResult, error) {
	ev := j.Meta.Event
	if ev == "" {
		ev = "tanpa-event"
	}
	dir := filepath.Join(j.Cfg.LocalOutputFolder, badName.ReplaceAllString(ev, "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return UploadResult{}, &UploadError{Msg: err.Error(), Local: true}
	}
	target := filepath.Join(dir, j.Meta.OriginalName+".frames.zip")
	if err := os.WriteFile(target, j.Video.Zip, 0o644); err != nil {
		return UploadResult{}, &UploadError{Msg: err.Error(), Local: true}
	}
	return UploadResult{RemoteID: target}, nil
}

// ---------- tujuan FotoYu ----------

type FotoyuUploader struct {
	client *fotoyu.Client
}

func NewFotoyuUploader(c *fotoyu.Client) *FotoyuUploader { return &FotoyuUploader{client: c} }

func (FotoyuUploader) ID() string { return "fotoyu" }

func (u *FotoyuUploader) Test(cfg Settings) (bool, string) {
	if !u.client.IsLoggedIn() {
		return false, "Belum ada akun FotoYu yang aktif. Masuk dulu di menu Lisensi/Atur."
	}
	if cfg.Event.Tree == nil {
		return false, "Pilih FotoTree (lokasi) dulu."
	}
	return true, "Tersambung sebagai @" + u.client.ActiveUsername()
}

func (u *FotoyuUploader) checkReady(cfg Settings, videoPrice bool) (*fotoyu.Tree, int, error) {
	if !u.client.IsLoggedIn() {
		return nil, 0, &UploadError{Msg: "Belum masuk akun FotoYu — masuk di menu Lisensi", Pause: true, Account: true}
	}
	if cfg.Event.Tree == nil {
		return nil, 0, &UploadError{Msg: "FotoTree belum dipilih", Pause: true}
	}
	price := cfg.Event.Price
	if videoPrice && cfg.Event.VideoPrice > 0 {
		price = cfg.Event.VideoPrice
	}
	if price <= 0 {
		return nil, 0, &UploadError{Msg: "Harga belum diisi", Pause: true}
	}
	return toFotoyuTree(cfg.Event.Tree), price, nil
}

func (u *FotoyuUploader) Upload(j PhotoJob) (UploadResult, error) {
	tree, price, err := u.checkReady(j.Cfg, false)
	if err != nil {
		return UploadResult{}, err
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(j.FileName)), ".")
	mime := "image/jpeg"
	switch ext {
	case "jpg", "jpeg":
		ext, mime = "jpg", "image/jpeg"
	case "png":
		mime = "image/png"
	default:
		return UploadResult{}, &UploadError{Msg: "Format foto tidak didukung FotoYu (pakai JPG/PNG)", Permanent: true}
	}
	md5 := j.Meta.MD5
	if md5 == "" {
		md5 = MD5Hex(j.Buffer)
	}
	res, ferr := u.client.UploadPhoto(fotoyu.PhotoUpload{
		Buffer: j.Buffer, Title: cut(firstNonEmpty(j.Meta.OriginalName, j.FileName), 100),
		Width: j.Width, Height: j.Height, Extension: ext, Mime: mime, MD5: md5,
		OriginalAt: resolveDate(j.Meta.OriginalAt), Price: price, Tree: tree,
		Description: j.Cfg.Event.Description, Nicknames: j.Cfg.Event.Nicknames,
	})
	if ferr != nil {
		return UploadResult{}, classify(ferr)
	}
	return UploadResult{RemoteID: res.RemoteID, Account: res.Account, Quality: res.Quality}, nil
}

func (u *FotoyuUploader) UploadVideo(j VideoJob) (UploadResult, error) {
	tree, price, err := u.checkReady(j.Cfg, true)
	if err != nil {
		return UploadResult{}, err
	}
	res, ferr := u.client.UploadVideo(fotoyu.VideoUpload{
		Zip: j.Video.Zip, Title: cut(j.Meta.OriginalName, 100), Width: j.Video.Width, Height: j.Video.Height,
		MD5: j.Meta.MD5, OriginalAt: resolveDate(j.Video.OriginalAt), Price: price, Tree: tree,
		Description: j.Cfg.Event.Description, Nicknames: j.Cfg.Event.Nicknames,
	})
	if ferr != nil {
		return UploadResult{}, classify(ferr)
	}
	return UploadResult{RemoteID: res.RemoteID, Account: res.Account}, nil
}

// Penolakan yang berlaku untuk SELURUH sesi (bukan satu foto) → jeda antrean dengan pesan jelas.
var sessionReject = []struct {
	re  *regexp.Regexp
	msg string
}{
	{regexp.MustCompile(`(?i)storage|penyimpanan|quota|kuota|space|kapasitas`), "Penyimpanan akun FotoYu penuh — kosongkan/tambah di aplikasi FotoYu, lalu Lanjutkan"},
	{regexp.MustCompile(`(?i)polic|kebijakan|agreement|syarat|terms|approv|setuju|verif|kyc|identitas`), "Kebijakan/verifikasi akun FotoYu belum selesai — buka aplikasi FotoYu, lalu Lanjutkan"},
	{regexp.MustCompile(`(?i)currenc|mata uang`), "Mata uang akun FotoYu belum diatur — atur di aplikasi FotoYu, lalu Lanjutkan"},
	{regexp.MustCompile(`(?i)price|harga`), "Harga ditolak FotoYu — periksa harga, lalu Lanjutkan"},
}

// classify memetakan fotoyu.Error → UploadError (port dari classify() di uploaders/fotoyu.js).
func classify(err error) error {
	fe, ok := err.(*fotoyu.Error)
	if !ok {
		return &UploadError{Msg: err.Error()}
	}
	e := &UploadError{Msg: fe.Message, Status: fe.Status, Detail: fe.Detail, ContentID: fe.ContentID}
	if fe.Put {
		e.Put = true
		return e
	}
	if fe.Duplicate != "" {
		e.Duplicate = fe.Duplicate
		e.Permanent = true
		return e
	}
	if fe.Status >= 400 && fe.Status < 500 && fe.Status != 429 && fe.Status != 409 {
		for _, sr := range sessionReject {
			if sr.re.MatchString(fe.Message) {
				e.Pause = true
				e.SessionReject = true
				e.Detail = fe.Message
				e.Msg = sr.msg
				return e
			}
		}
	}
	switch {
	case fe.Status == 401:
		e.Pause = true
		e.Account = true
	case fe.Status == 403:
		e.Pause = true
	case fe.Status == 409:
		e.Permanent = true
		e.Duplicate = "other"
		e.Msg = "Duplikat: sudah pernah diunggah ke FotoYu"
	case fe.Status >= 400 && fe.Status < 500 && fe.Status != 429:
		e.Permanent = true
	}
	return e
}

// ---------- helper ----------

func toFotoyuTree(t *Tree) *fotoyu.Tree {
	if t == nil {
		return nil
	}
	return &fotoyu.Tree{
		ID: t.ID, TagID: t.TagID, Name: t.Name, IsBib: t.IsBib,
		Location: fotoyu.Location{Name: t.Location.Name, Latitude: t.Location.Latitude, Longitude: t.Location.Longitude},
	}
}

// Tanggal yang dipakai: dari kamera (EXIF/mtime), atau sekarang bila tidak ada.
func resolveDate(cam time.Time) time.Time {
	if cam.IsZero() {
		return time.Now()
	}
	return cam
}

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
