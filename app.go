package main

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"time"

	"milesshoot/internal/core"
	"milesshoot/internal/fotoyu"
	"milesshoot/internal/ftp"
	"milesshoot/internal/usbcam"
	"milesshoot/internal/video"

	wrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App adalah jembatan Wails: membungkus Engine, mengirim event ke jendela desktop,
// dan membuka perintah (command) yang dipanggil frontend lewat invoke/bindings.
type App struct {
	ctx    context.Context
	eng    *core.Engine
	ftp    *ftp.Server
	ftpSig string // tanda tangan setelan FTP terakhir yang diterapkan
	port   int    // port server HP (buat tombol "pantau dari HP")
	logs   *core.FileLogger
	cam    *usbcam.Cam
	drives *usbcam.DriveWatcher
	vf     vfJob // pekerjaan Video → Foto (vidfoto_app.go)
}

func NewApp(eng *core.Engine, phonePort int, ftpSrv *ftp.Server, logs *core.FileLogger,
	cam *usbcam.Cam, drives *usbcam.DriveWatcher) *App {
	return &App{eng: eng, port: phonePort, ftp: ftpSrv, logs: logs, cam: cam, drives: drives}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.eng.AddEmitter(&wailsEmitter{ctx: ctx})
	video.Logf = a.eng.Log
	a.logStartup()
	a.eng.Start()
	if a.ftp != nil {
		a.ftp.Start()
	}
	if a.cam != nil && a.eng.Cfg.Get().UsbCam.Enabled {
		a.cam.Start()
	}
	go func() {
		t := time.NewTicker(1500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.eng.BroadcastStatus()
			}
		}
	}()
}

func (a *App) shutdown(ctx context.Context) {
	if a.ftp != nil {
		a.ftp.Stop()
	}
	if a.cam != nil {
		a.cam.Stop()
	}
	a.eng.Stop()
}

// ---------- emitter → event Wails ----------

type wailsEmitter struct{ ctx context.Context }

func (w *wailsEmitter) Emit(event string, data any) { wrt.EventsEmit(w.ctx, event, data) }
func (w *wailsEmitter) Log(level, msg string) {
	wrt.EventsEmit(w.ctx, "log", map[string]any{"level": level, "msg": msg, "t": time.Now().UnixMilli()})
}

// ---------- perintah (dibind ke frontend) ----------

func (a *App) Status() core.Status      { return a.eng.Status() }
func (a *App) GetConfig() core.Settings { return a.eng.Cfg.Get() }

func (a *App) UpdateConfig(patch string) core.Settings {
	s, _ := a.eng.Cfg.Update([]byte(patch))
	a.applySideEffects()
	a.eng.BroadcastStatus()
	return s
}

// Beberapa perubahan setting butuh "lupakan yang dilewati" supaya dipindai ulang.
func (a *App) applySideEffects() {
	a.eng.Queue.VideosEnabledChanged()
	a.eng.Queue.FiltersChanged()
	a.eng.Scanner.Scan()
	a.syncFTP()
}

// syncFTP menyalakan ulang server FTP hanya bila pengaturannya benar-benar berubah
// (kalau tidak, kamera yang sedang tersambung ikut terputus tiap ganti setelan).
func (a *App) syncFTP() {
	if a.ftp == nil {
		return
	}
	c := a.eng.Cfg.Get()
	sig := itoa(btoi(c.FTP.Enabled)) + "|" + itoa(c.FTP.Port) + "|" + c.FTP.User + "|" + c.WatchFolder
	if sig == a.ftpSig {
		return
	}
	a.ftpSig = sig
	a.ftp.Start()
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// FTPInfo: dipakai jendela desktop. Password HANYA dikirim lewat jalur ini,
// tidak pernah lewat API jaringan /api (server HP).
type FTPInfo struct {
	ftp.Info
	Pass string `json:"pass"`
}

func (a *App) FTPInfo() FTPInfo {
	if a.ftp == nil {
		return FTPInfo{}
	}
	return FTPInfo{Info: a.ftp.Info(), Pass: a.eng.Cfg.Get().FTP.Pass}
}

func (a *App) RestartFTP() FTPInfo {
	if a.ftp != nil {
		a.ftp.Start()
	}
	if a.cam != nil && a.eng.Cfg.Get().UsbCam.Enabled {
		a.cam.Start()
	}
	return a.FTPInfo()
}

// ---------- Kamera kabel (PTP/tethering) ----------

func (a *App) UsbCamState() usbcam.State {
	if a.cam == nil {
		return usbcam.State{}
	}
	return a.cam.State()
}

// UsbCamToggle menyalakan/mematikan pembantu kamera kabel.
func (a *App) UsbCamToggle(on bool) usbcam.State {
	a.eng.Cfg.Set(func(s *core.Settings) { s.UsbCam.Enabled = on })
	if a.cam == nil {
		return usbcam.State{}
	}
	if on {
		a.cam.Start()
	} else {
		a.cam.Stop()
	}
	return a.cam.State()
}

// ---------- Kartu memori / card reader ----------

// CardDrives: daftar kartu/kamera yang tercolok dan punya folder DCIM.
func (a *App) CardDrives() []usbcam.Drive {
	if a.drives == nil {
		return []usbcam.Drive{}
	}
	return a.drives.Drives()
}

// UseCardDrive menjadikan folder DCIM sebuah kartu sebagai sumber foto.
//
// Dua pengaman yang disengaja:
//  1. Unggahan DIJEDA — kartu bisa berisi ribuan foto lama; pengguna harus memeriksa
//     saringan dulu lalu menekan Lanjutkan.
//  2. "Pindah setelah terunggah" DIMATIKAN — jangan pernah menulis/memindahkan apa pun
//     di kartu milik orang.
func (a *App) UseCardDrive(path string) core.Settings {
	if path == "" {
		return a.eng.Cfg.Get()
	}
	a.eng.Cfg.Set(func(s *core.Settings) {
		s.WatchFolder = path
		s.AfterUpload.Move = false
		// Kartu SELALU menyimpan foto di subfolder (DCIM/100NIKON, DCIM/100SLNTC, …).
		// Tanpa ini scanner hanya melihat folder DCIM yang isinya folder — 0 foto,
		// dan pengguna bingung kenapa tidak ada yang terbaca.
		s.IncludeSubfolders = true
	})
	a.eng.Queue.Pause("manual")
	a.applySideEffects()
	a.eng.Log("warn", "Sumber diubah ke kartu: "+path+
		" — unggahan DIJEDA dan file di kartu tidak akan dipindah. Periksa saringan, lalu tekan Lanjutkan.")
	a.eng.BroadcastStatus()
	return a.eng.Cfg.Get()
}

// ---------- Ranking (peringkat daun per akun di sebuah FotoTree) ----------

func (a *App) RankingStart(treeID, name, rangeKey string) core.RankView {
	return a.eng.Ranking.Start(treeID, name, rangeKey)
}

func (a *App) RankingStatus(treeID, rangeKey string) core.RankView {
	v, _ := a.eng.Ranking.Status(treeID, rangeKey)
	return v
}

func (a *App) RankingCancel(treeID, rangeKey string) { a.eng.Ranking.Cancel(treeID, rangeKey) }

// ---------- Log (untuk menguji sambungan kamera dll) ----------

// LogTail: n catatan terbaru (terbaru di atas).
func (a *App) LogTail(n int) []core.LogLine {
	if a.logs == nil {
		return nil
	}
	return a.logs.Tail(n)
}

// LogFile: lokasi berkas log hari ini (ditampilkan di UI supaya mudah dikirim saat minta bantuan).
func (a *App) LogFile() string {
	if a.logs == nil {
		return ""
	}
	return a.logs.Today()
}

// OpenLogFolder membuka folder log di Finder/Explorer.
func (a *App) OpenLogFolder() {
	if a.logs != nil {
		wrt.BrowserOpenURL(a.ctx, "file://"+a.logs.Dir())
	}
}

// ---------- Hi-res (foto terjual menunggu file asli) ----------

func (a *App) HiresState() core.HiresState { return a.eng.Hires.State() }

func (a *App) HiresRefresh() core.HiresState { return a.eng.Hires.Refresh() }

func (a *App) HiresSend(id string) core.HiresState { return a.eng.Hires.Send(id) }

func (a *App) HiresSendAll() core.HiresState { return a.eng.Hires.SendAllExact() }

// HiresAddFolder membuka dialog folder tambahan tempat file asli disimpan.
func (a *App) HiresAddFolder() (core.Settings, error) {
	dir, err := wrt.OpenDirectoryDialog(a.ctx, wrt.OpenDialogOptions{Title: "Folder tambahan tempat file asli"})
	if err != nil || dir == "" {
		return a.eng.Cfg.Get(), err
	}
	return a.eng.Hires.AddFolder(dir), nil
}

func (a *App) HiresRemoveFolder(p string) core.Settings { return a.eng.Hires.RemoveFolder(p) }

// HiresSetAuto menyalakan/mematikan "kirim otomatis": tiap 5 menit, pesanan yang
// "cocok pasti" (sidik file identik) dikirim sendiri tanpa perlu ditekan manual.
// "Cocok nama" (isi beda, mis. foto sudah diedit) TIDAK PERNAH dikirim otomatis.
func (a *App) HiresSetAuto(on bool) core.Settings {
	return a.eng.Cfg.Set(func(s *core.Settings) { s.Hires.Auto = on })
}

// ListLUTs mengembalikan pustaka LUT (.cube) yang sudah diimpor.
func (a *App) ListLUTs() []core.LUTFile { return core.ListLUTs(a.eng.Cfg.DataDir()) }

// ImportLUT membuka dialog pilih file LUT (.cube) atau preset Lightroom (.xmp/.dng/
// .lrtemplate — diubah otomatis jadi .cube), memvalidasi (benar-benar ter-parse, bukan
// cuma cek ekstensi), lalu menyalin/menyimpannya ke pustaka data/luts/.
func (a *App) ImportLUT() ([]core.LUTFile, error) {
	path, err := wrt.OpenFileDialog(a.ctx, wrt.OpenDialogOptions{
		Title: "Pilih LUT (.cube) atau preset Lightroom (.xmp / .dng / .lrtemplate)",
		Filters: []wrt.FileFilter{
			{DisplayName: "LUT & preset Lightroom (*.cube;*.xmp;*.dng;*.lrtemplate)", Pattern: "*.cube;*.xmp;*.dng;*.lrtemplate"},
		},
	})
	if err != nil || path == "" {
		return core.ListLUTs(a.eng.Cfg.DataDir()), err
	}
	if _, ierr := core.ImportLUT(a.eng.Cfg.DataDir(), path); ierr != nil {
		return core.ListLUTs(a.eng.Cfg.DataDir()), ierr
	}
	return core.ListLUTs(a.eng.Cfg.DataDir()), nil
}

// DeleteLUT menghapus satu LUT dari pustaka dan membersihkan referensi yang menunjuk
// ke file itu (Photo/Video) supaya setelan tidak diam-diam menunjuk ke file yang hilang.
func (a *App) DeleteLUT(name string) ([]core.LUTFile, error) {
	dataDir := a.eng.Cfg.DataDir()
	full := filepath.Join(core.LUTDir(dataDir), name)
	if err := core.DeleteLUT(dataDir, name); err != nil {
		return core.ListLUTs(dataDir), err
	}
	a.eng.Cfg.Set(func(s *core.Settings) {
		if s.LUT.Photo == full {
			s.LUT.Photo = ""
		}
		if s.LUT.Video == full {
			s.LUT.Video = ""
		}
		if s.Hires.LUT == full {
			s.Hires.LUT = ""
		}
	})
	return core.ListLUTs(dataDir), nil
}

// SetLUT menetapkan (atau mengosongkan, path="") LUT untuk peran "photo", "video", atau "hires".
func (a *App) SetLUT(role, path string) core.Settings {
	return a.eng.Cfg.Set(func(s *core.Settings) {
		switch role {
		case "photo":
			s.LUT.Photo = path
		case "video":
			s.LUT.Video = path
		case "hires":
			s.Hires.LUT = path
		}
	})
}

// AddWatchFolder membuka dialog pilih folder tambahan — file di dalamnya dipindai &
// diunggah BERSAMAAN dengan folder utama (lewat gate konkurensi yang sudah ada,
// tidak membedakan folder asal file). Pola sama dengan HiresAddFolder.
func (a *App) AddWatchFolder() (core.Settings, error) {
	dir, err := wrt.OpenDirectoryDialog(a.ctx, wrt.OpenDialogOptions{Title: "Tambah folder sumber"})
	if err != nil || dir == "" {
		return a.eng.Cfg.Get(), err
	}
	return a.eng.Cfg.Set(func(s *core.Settings) {
		if dir == s.WatchFolder {
			return
		}
		for _, f := range s.WatchFolders {
			if f == dir {
				return
			}
		}
		s.WatchFolders = append(s.WatchFolders, dir)
	}), nil
}

func (a *App) RemoveWatchFolder(path string) core.Settings {
	return a.eng.Cfg.Set(func(s *core.Settings) {
		out := s.WatchFolders[:0]
		for _, f := range s.WatchFolders {
			if f != path {
				out = append(out, f)
			}
		}
		s.WatchFolders = out
	})
}

// WatermarkPickLogo membuka dialog pilih gambar logo untuk watermark tipe "logo".
func (a *App) WatermarkPickLogo() (core.Settings, error) {
	path, err := wrt.OpenFileDialog(a.ctx, wrt.OpenDialogOptions{
		Title: "Pilih gambar logo",
		Filters: []wrt.FileFilter{
			{DisplayName: "Gambar (*.png;*.jpg;*.jpeg)", Pattern: "*.png;*.jpg;*.jpeg"},
		},
	})
	if err != nil || path == "" {
		return a.eng.Cfg.Get(), err
	}
	return a.eng.Cfg.Set(func(s *core.Settings) { s.Watermark.LogoPath = path }), nil
}

// WatermarkPreview merender foto contoh (bukan foto asli siapa pun) dengan watermark
// diterapkan — patch opsional (JSON parsial) buat pratinjau sebelum disimpan, gaya MAS EDY
// (geser slider → pratinjau ikut berubah tanpa nunggu disimpan dulu).
func (a *App) WatermarkPreview(patch string) ([]byte, error) {
	wm := a.eng.Cfg.Get().Watermark
	if patch != "" {
		_ = json.Unmarshal([]byte(patch), &wm)
	}
	wm.Enabled = true
	return core.WatermarkPreviewImage(wm)
}

func (a *App) Pause()       { a.eng.Queue.Pause("manual"); a.eng.BroadcastStatus() }
func (a *App) Resume()      { a.eng.Queue.Resume(); a.eng.BroadcastStatus() }
func (a *App) RetryFailed() { a.eng.Queue.RetryFailed() }

// PickFolder membuka dialog pilih folder (native), menyimpannya sebagai folder dipantau utama.
// Mengembalikan Settings terbaru (bukan cuma path) supaya UI langsung ikut berubah tanpa
// perlu aksi lain yang kebetulan me-refresh config.
func (a *App) PickFolder() (core.Settings, error) {
	dir, err := wrt.OpenDirectoryDialog(a.ctx, wrt.OpenDialogOptions{Title: "Pilih folder foto yang dipantau"})
	if err != nil || dir == "" {
		return a.eng.Cfg.Get(), err
	}
	s := a.eng.Cfg.Set(func(s *core.Settings) { s.WatchFolder = dir })
	a.eng.Scanner.Scan()
	a.syncFTP()
	a.eng.BroadcastStatus()
	return s, nil
}

// ClearFolder mengosongkan folder utama (mis. sebelum ganti kartu memori) — tanpa folder
// tambahan, antrean otomatis kosong sampai folder baru dipilih.
func (a *App) ClearFolder() core.Settings {
	s := a.eng.Cfg.Set(func(s *core.Settings) { s.WatchFolder = "" })
	a.eng.Scanner.Scan()
	a.eng.BroadcastStatus()
	return s
}

// ---------- akun FotoYu ----------

func (a *App) Login(identifier, password string) (fotoyu.SessionInfo, error) {
	si, err := a.eng.Fotoyu.Login(identifier, password)
	if err == nil {
		a.eng.Queue.Resume() // lanjut otomatis bila tadi dijeda karena belum login
		a.eng.BroadcastStatus()
	}
	return si, err
}

func (a *App) Logout() core.Status {
	a.eng.Fotoyu.Logout()
	a.eng.BroadcastStatus()
	return a.eng.Status()
}

func (a *App) Account() fotoyu.SessionInfo { return a.eng.Fotoyu.Session() }

// ---------- multi-akun FotoYu (semua tersimpan LOKAL di fotoyu-akun.json, bukan database) ----------

func (a *App) Accounts() []fotoyu.AccountInfo { return a.eng.Fotoyu.Accounts() }

func (a *App) SwitchAccount(id string) (fotoyu.SessionInfo, error) {
	si, err := a.eng.Fotoyu.SwitchAccount(id)
	if err == nil {
		a.eng.Queue.Resume()
		a.eng.BroadcastStatus()
	}
	return si, err
}

func (a *App) RemoveAccount(id string) []fotoyu.AccountInfo {
	out := a.eng.Fotoyu.RemoveAccount(id)
	a.eng.BroadcastStatus()
	return out
}

func (a *App) SearchTrees(name string) ([]fotoyu.TreeResult, error) {
	return a.eng.Fotoyu.SearchTrees(name, false)
}

func (a *App) SelectTree(t core.Tree) core.Settings {
	a.eng.Cfg.Set(func(s *core.Settings) { s.Event.Tree = &t })
	a.eng.Queue.Resume()
	a.eng.BroadcastStatus()
	return a.eng.Cfg.Get()
}

func (a *App) Fee(price int, tagID string) (fotoyu.FeeResult, error) {
	return a.eng.Fotoyu.Fee(price, tagID)
}

func (a *App) Storage() (fotoyu.StorageInfo, error) { return a.eng.Fotoyu.Storage() }

// Ping: latensi (ms) ke server FotoYu — dipakai kartu "Internet" di layar Unggah.
func (a *App) Ping() (int64, error) { return a.eng.Fotoyu.Ping() }

// ---------- pantau dari HP ----------

// PhoneURLs mengembalikan alamat yang bisa dibuka dari HP di Wi-Fi yang sama.
func (a *App) PhoneURLs() []string {
	return lanURLs(a.port)
}

func lanURLs(port int) []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			out = append(out, "http://"+ip.String()+":"+itoa(port))
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
