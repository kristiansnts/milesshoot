package main

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"milesshoot/internal/core"
	"milesshoot/internal/ftp"
	"milesshoot/internal/server"
	"milesshoot/internal/usbcam"
	"milesshoot/internal/video"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

const phonePort = 4577

func main() {
	// Video lewat ffmpeg (disuntik di sini supaya package core tak mengimpor video → tak ada import cycle).
	core.VideoPrepareFn = video.Prepare
	// Durasi/dimensi video untuk kiriman hi-res (ffmpeg).
	core.VideoProbeFn = func(path string) (float64, int, int, error) {
		dur, w, h, _, err := video.ProbeDims(path)
		return dur, w, h, err
	}

	dataDir := appDataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("tidak bisa membuat folder data: %v", err)
	}

	hub := server.NewHub()
	fileLog := core.NewFileLogger(dataDir) // semua catatan juga masuk berkas harian
	eng, err := core.NewEngine(dataDir, hub, fileLog)
	if err != nil {
		log.Fatalf("gagal memulai engine: %v", err)
	}

	// Server FTP kamera Wi-Fi: file masuk ke folder yang dipantau, lalu scanner mengambilnya.
	ftpSrv := ftp.New(
		func() ftp.Settings {
			c := eng.Cfg.Get()
			return ftp.Settings{Enabled: c.FTP.Enabled, Port: c.FTP.Port, User: c.FTP.User,
				Pass: c.FTP.Pass, Root: c.WatchFolder, Debug: c.Debug}
		},
		eng.Log,
		func(string) { eng.Scanner.Scan() }, // foto baru masuk → pindai sekarang, jangan tunggu tick
	)

	// Kamera kabel (PTP/tethering) lewat program pembantu native.
	usbDir := filepath.Join(dataDir, "kamera-usb")
	cam := usbcam.New(usbcam.Opts{
		OutDir: usbDir,
		Log:    eng.Log,
		OnFile: func(string) { eng.Scanner.Scan() }, // foto baru → pindai sekarang
		OnConnect: func(dir string) {
			c := eng.Cfg.Get()
			if c.UsbCam.AutoSource && c.WatchFolder != dir {
				eng.Cfg.Set(func(s *core.Settings) { s.WatchFolder = dir })
				eng.Log("info", "Folder sumber dialihkan ke kamera kabel: "+dir)
				eng.Scanner.Scan()
			}
		},
		OnChange: eng.BroadcastStatus,
	})

	// Kartu memori / card reader / kamera mode flashdisk (drive berisi DCIM).
	drives := usbcam.NewDriveWatcher(eng.Log, eng.BroadcastStatus)
	drives.Start()

	app := NewApp(eng, phonePort, ftpSrv, fileLog, cam, drives)

	// Server HP (pantau dari HP di Wi-Fi yang sama) — UI + SSE + API.
	// AfterConfig disambungkan ke app supaya ubah setelan dari HP punya efek yang sama
	// dengan dari jendela desktop.
	if sub, err := fs.Sub(assets, "frontend/dist"); err == nil {
		srv := server.New(eng, hub, sub)
		srv.AfterConfig = app.applySideEffects
		go func() {
			if e := srv.Listen("0.0.0.0:" + itoa(phonePort)); e != nil {
				log.Printf("server HP mati: %v", e)
			}
		}()
	}

	err = wails.Run(&options.App{
		Title:            "milesshoot",
		Width:            1180,
		Height:           780,
		MinWidth:         900,
		MinHeight:        600,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 243, G: 244, B: 246, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
	})
	if err != nil {
		log.Fatalf("Wails: %v", err)
	}
}

// Folder data: ~/Library/Application Support/milesshoot (Mac), %AppData%/milesshoot (Windows).
func appDataDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		dir := filepath.Join(d, "milesshoot")
		// Migrasi satu kali dari nama lama (fotonaik) agar login & setelan tidak hilang.
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			_ = os.Rename(filepath.Join(d, "fotonaik"), dir)
		}
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".milesshoot")
}
