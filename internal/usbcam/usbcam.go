// Package usbcam menjalankan program pembantu kamera kabel (PTP/tethering) dan
// menerjemahkan keluarannya jadi kejadian untuk aplikasi.
//
// Kamera tether tidak muncul sebagai drive, jadi tidak bisa dipantau seperti folder.
// Pembantu native (macOS: Swift + ImageCaptureCore) mengunduh jepretan BARU ke sebuah
// folder; folder itulah yang kemudian dijadikan sumber foto.
//
// Kontraknya sengaja sederhana: pembantu menulis satu JSON per baris ke stdout,
// dan bila mati akan dinyalakan ulang.
package usbcam

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type State struct {
	Available bool   `json:"available"` // program pembantu ada di sistem ini
	Running   bool   `json:"running"`
	Connected bool   `json:"connected"`
	Model     string `json:"model"`
	Files     int    `json:"files"`   // jepretan yang sudah diunduh sesi ini
	Ignored   int    `json:"ignored"` // foto lama di kamera yang dilewati
	Error     string `json:"error"`
	Dir       string `json:"dir"`
}

type Opts struct {
	HelperPath string                  // "" = cari otomatis
	OutDir     string                  // folder tujuan unduhan
	Log        func(level, msg string) //
	OnFile     func(path string)       // satu foto selesai diunduh
	OnConnect  func(dir string)        // kamera tersambung → dir layak jadi sumber
	OnChange   func()                  // status berubah (untuk menyegarkan UI)
}

type Cam struct {
	mu   sync.Mutex
	opt  Opts
	st   State
	cmd  *exec.Cmd
	stop bool
}

func New(o Opts) *Cam {
	if o.Log == nil {
		o.Log = func(string, string) {}
	}
	for _, f := range []*func(string){&o.OnFile, &o.OnConnect} {
		if *f == nil {
			*f = func(string) {}
		}
	}
	if o.OnChange == nil {
		o.OnChange = func() {}
	}
	if o.HelperPath == "" {
		o.HelperPath = FindHelper()
	}
	c := &Cam{opt: o}
	c.st.Dir = o.OutDir
	c.st.Available = o.HelperPath != ""
	return c
}

// FindHelper mencari program pembantu: di sebelah aplikasi (saat dipaketkan),
// lalu di build/helpers (saat pengembangan).
func FindHelper() string {
	name := "fotonaik-kamera"
	var cands []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		cands = append(cands,
			filepath.Join(dir, name),                    // Contents/MacOS/
			filepath.Join(dir, "..", "Resources", name), // Contents/Resources/
		)
	}
	if wd, err := os.Getwd(); err == nil {
		cands = append(cands, filepath.Join(wd, "build", "helpers", name))
	}
	for _, p := range cands {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	return ""
}

func (c *Cam) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st
}

func (c *Cam) Start() {
	c.mu.Lock()
	if c.st.Running || c.opt.HelperPath == "" {
		avail := c.opt.HelperPath != ""
		c.st.Available = avail
		if !avail {
			c.st.Error = "Program pembantu kamera kabel tidak ditemukan"
		}
		c.mu.Unlock()
		return
	}
	c.stop = false
	c.mu.Unlock()
	go c.loop()
}

func (c *Cam) Stop() {
	c.mu.Lock()
	c.stop = true
	cmd := c.cmd
	c.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// loop menjalankan pembantu, dan menyalakannya lagi bila mati (kamera dicabut
// kadang membuat pembantu keluar).
func (c *Cam) loop() {
	for {
		c.mu.Lock()
		if c.stop {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		c.runOnce()

		c.mu.Lock()
		stopped := c.stop
		c.st.Running = false
		c.st.Connected = false
		c.mu.Unlock()
		c.opt.OnChange()
		if stopped {
			return
		}
		time.Sleep(5 * time.Second)
	}
}

func (c *Cam) runOnce() {
	_ = os.MkdirAll(c.opt.OutDir, 0o755)
	cmd := exec.Command(c.opt.HelperPath, c.opt.OutDir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		c.mu.Lock()
		c.st.Error = "Pembantu kamera gagal dijalankan: " + err.Error()
		c.mu.Unlock()
		c.opt.Log("error", c.st.Error)
		return
	}

	c.mu.Lock()
	c.cmd = cmd
	c.st.Running = true
	c.st.Error = ""
	c.mu.Unlock()
	c.opt.OnChange()

	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		c.handle(sc.Bytes())
	}
	_ = cmd.Wait()

	c.mu.Lock()
	c.cmd = nil
	c.mu.Unlock()
}

type helperMsg struct {
	Event   string `json:"event"`
	Model   string `json:"model"`
	Path    string `json:"path"`
	Msg     string `json:"msg"`
	Ignored int    `json:"ignored"`
}

func (c *Cam) handle(line []byte) {
	var m helperMsg
	if json.Unmarshal(line, &m) != nil || m.Event == "" {
		return
	}
	switch m.Event {
	case "ready":
		c.opt.Log("info", "Pembantu kamera kabel siap")

	case "connected":
		c.mu.Lock()
		c.st.Connected = true
		c.st.Model = m.Model
		c.st.Files = 0
		c.st.Error = ""
		dir := c.opt.OutDir
		c.mu.Unlock()
		c.opt.Log("ok", "Kamera kabel tersambung: "+m.Model)
		c.opt.OnConnect(dir)
		c.opt.OnChange()

	case "catalog":
		c.mu.Lock()
		c.st.Ignored = m.Ignored
		c.mu.Unlock()
		if m.Ignored > 0 {
			c.opt.Log("info", "Kamera kabel: "+itoa(m.Ignored)+" foto lama di kartu diabaikan, hanya jepretan baru yang diunggah")
		}
		c.opt.OnChange()

	case "file":
		if m.Path == "" {
			return
		}
		c.mu.Lock()
		c.st.Files++
		c.mu.Unlock()
		c.opt.Log("ok", "Dari kamera kabel: "+filepath.Base(m.Path))
		c.opt.OnFile(m.Path)
		c.opt.OnChange()

	case "disconnected":
		c.mu.Lock()
		c.st.Connected = false
		c.st.Model = ""
		c.mu.Unlock()
		c.opt.Log("info", "Kamera kabel dilepas")
		c.opt.OnChange()

	case "error":
		c.mu.Lock()
		c.st.Error = m.Msg
		c.mu.Unlock()
		c.opt.Log("error", "Kamera kabel: "+m.Msg)
		c.opt.OnChange()
	}
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
