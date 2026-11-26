// Package ftp adalah server FTP kecil supaya kamera (merek apa pun yang mendukung FTP)
// bisa mengirim foto langsung lewat Wi-Fi ke laptop. File ditaruh di folder yang dipantau,
// lalu scanner/queue mengambilnya seperti file biasa.
//
// Mendukung: USER/PASS, PASV/EPSV (pasif) & PORT (aktif), STOR, LIST/NLST/MLSD,
// CWD/MKD/PWD, SIZE/MDTM, RNFR/RNTO. Tanpa enkripsi — pilih "FTP" biasa di kamera.
// Port dari src/ftp-server.js.
package ftp

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Settings dibaca ulang tiap dibutuhkan (user bisa ganti folder/port kapan saja).
type Settings struct {
	Enabled bool
	Port    int
	User    string
	Pass    string
	Root    string // folder tujuan = folder yang dipantau
	Debug   bool   // catat tiap perintah & balasan (untuk menguji sambungan kamera)
}

type Info struct {
	Enabled   bool     `json:"enabled"`
	Running   bool     `json:"running"`
	Port      int      `json:"port"`
	Error     string   `json:"error"`
	User      string   `json:"user"`
	Addresses []string `json:"addresses"`
	Connected int      `json:"connected"`
	Files     int      `json:"files"`
	Bytes     int64    `json:"bytes"`
	LastFile  string   `json:"lastFile"`
	LastAt    int64    `json:"lastAt"`
	// Pass SENGAJA tidak ada di sini — password hanya dikirim lewat perintah desktop,
	// tidak pernah lewat API jaringan (pelajaran dari kebocoran app asli).
}

type Server struct {
	get    func() Settings
	log    func(level, msg string)
	onFile func(path string)

	mu       sync.Mutex
	ln       net.Listener
	port     int
	errMsg   string
	clients  map[net.Conn]bool
	probing  bool // sedang memeriksa diri sendiri → jangan catat sebagai "kamera"
	files    int
	bytes    int64
	lastFile string
	lastAt   int64
}

func New(get func() Settings, log func(level, msg string), onFile func(path string)) *Server {
	if log == nil {
		log = func(string, string) {}
	}
	if onFile == nil {
		onFile = func(string) {}
	}
	return &Server{get: get, log: log, onFile: onFile, clients: map[net.Conn]bool{}}
}

// Start menyalakan server. Port 21 butuh akses root di macOS/Linux, jadi otomatis
// mundur ke 2121 / 2221 bila gagal.
func (s *Server) Start() {
	s.Stop()
	cfg := s.get()
	if !cfg.Enabled {
		return
	}
	ports := dedupe([]int{cfg.Port, 2121, 2221})
	for _, p := range ports {
		if p <= 0 {
			continue
		}
		ln, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(p))
		if err != nil {
			s.mu.Lock()
			s.errMsg = fmt.Sprintf("Port %d tidak bisa dipakai (%v)", p, rootCause(err))
			s.mu.Unlock()
			continue
		}
		s.mu.Lock()
		s.ln, s.port, s.errMsg = ln, p, ""
		s.mu.Unlock()
		s.log("info", fmt.Sprintf("Server FTP kamera menyala di port %d", p))
		go s.accept(ln)
		go s.selfCheck(p)
		return
	}
	s.mu.Lock()
	msg := s.errMsg
	s.mu.Unlock()
	s.log("error", "Server FTP gagal menyala: "+msg)
}

func (s *Server) Stop() {
	s.mu.Lock()
	ln := s.ln
	s.ln, s.port = nil, 0
	clients := make([]net.Conn, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.clients = map[net.Conn]bool{}
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	for _, c := range clients {
		_ = c.Close()
	}
}

func (s *Server) Info() Info {
	cfg := s.get()
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{
		Enabled: cfg.Enabled, Running: s.ln != nil, Port: s.port, Error: s.errMsg,
		User: cfg.User, Addresses: LanAddresses(), Connected: len(s.clients),
		Files: s.files, Bytes: s.bytes, LastFile: s.lastFile, LastAt: s.lastAt,
	}
}

func (s *Server) accept(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return // listener ditutup
		}
		s.mu.Lock()
		if s.ln != ln { // sudah di-restart
			s.mu.Unlock()
			_ = c.Close()
			return
		}
		s.clients[c] = true
		s.mu.Unlock()
		go s.handle(c)
	}
}

// selfCheck memastikan koneksi ke port ini BENAR-BENAR sampai ke kita.
// Di macOS dua proses bisa sama-sama "berhasil" mengikat port yang sama, tapi hanya
// satu yang menerima koneksi — tanpa pemeriksaan ini kamera bisa diam-diam tersambung
// ke aplikasi lain sementara log kita bersih.
func (s *Server) selfCheck(port int) {
	s.mu.Lock()
	s.probing = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.probing = false
		s.mu.Unlock()
	}()

	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 2*time.Second)
	if err != nil {
		return
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 160)
	n, _ := c.Read(buf)
	banner := strings.TrimSpace(strings.SplitN(string(buf[:n]), "\n", 2)[0])
	if banner != "" && !strings.Contains(banner, "milesshoot") {
		s.log("error", fmt.Sprintf(
			"Port %d sudah dipakai aplikasi lain (%q) — kamera akan tersambung ke sana, BUKAN ke milesshoot. Tutup aplikasi itu, atau ganti port FTP di Pengaturan.",
			port, banner))
	}
}

// state satu koneksi kamera
type session struct {
	user       string
	authed     bool
	cwd        string
	pasvLn     net.Listener
	pasvConn   chan net.Conn
	active     *net.TCPAddr
	renameFrom string
}

func (s *Server) handle(conn net.Conn) {
	remote := hostOnly(conn.RemoteAddr().String())
	probe := s.isProbing() // koneksi dari pemeriksa-diri, bukan kamera sungguhan
	st := &session{cwd: "/"}
	defer func() {
		closePasv(st)
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
		_ = conn.Close()
		if !probe {
			s.log("info", "FTP "+remote+": kamera terputus")
		}
	}()

	lg := func(level, msg string) { s.log(level, "FTP "+remote+": "+msg) }
	reply := func(code int, text string) {
		_, _ = fmt.Fprintf(conn, "%d %s\r\n", code, text)
		if !probe && s.get().Debug {
			lg("info", fmt.Sprintf("<- %d %s", code, text))
		}
	}

	if !probe {
		lg("info", "kamera tersambung")
	}
	reply(220, "milesshoot FTP siap")

	r := bufio.NewReader(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Minute))
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		if s.get().Debug {
			lg("info", "-> "+maskCmd(line))
		}
		if quit := s.command(line, st, reply, lg, conn); quit {
			return
		}
	}
}

func (s *Server) command(line string, st *session, reply func(int, string), lg func(string, string), conn net.Conn) bool {
	cmd, arg := split2(line)
	cmd = strings.ToUpper(cmd)
	cfg := s.get()

	switch cmd {
	case "USER":
		st.user = arg
		reply(331, "Masukkan password")
		return false
	case "PASS":
		if st.user == cfg.User && arg == cfg.Pass && cfg.Pass != "" {
			if cfg.Root == "" || !isDir(cfg.Root) {
				lg("error", "login ditolak: folder dipantau belum diatur")
				reply(530, "Folder tujuan di milesshoot belum diatur")
				return false
			}
			st.authed = true
			lg("ok", "login berhasil")
			reply(230, "Login berhasil")
			return false
		}
		lg("error", "user/password salah")
		reply(530, "User atau password salah")
		return false
	case "QUIT":
		reply(221, "Sampai jumpa")
		return true
	case "AUTH", "PBSZ", "PROT":
		reply(502, "Gunakan FTP biasa (tanpa SSL/TLS)")
		return false
	case "FEAT":
		_, _ = io.WriteString(conn, "211-Fitur:\r\n SIZE\r\n MDTM\r\n EPSV\r\n PASV\r\n UTF8\r\n211 Selesai\r\n")
		return false
	case "SYST":
		reply(215, "UNIX Type: L8")
		return false
	case "NOOP", "OPTS":
		reply(200, "OK")
		return false
	}

	if !st.authed {
		reply(530, "Login dulu")
		return false
	}

	switch cmd {
	case "PWD", "XPWD":
		reply(257, `"`+st.cwd+`"`)
	case "TYPE", "MODE", "STRU", "ALLO", "SITE", "ABOR":
		reply(200, "OK")
	case "CWD", "XCWD":
		v, real := s.resolve(cfg.Root, st, arg)
		if isDir(real) {
			st.cwd = v
			reply(250, "OK")
		} else {
			reply(550, "Folder tidak ada")
		}
	case "CDUP", "XCUP":
		v, _ := s.resolve(cfg.Root, st, "..")
		st.cwd = v
		reply(250, "OK")
	case "MKD", "XMKD":
		v, real := s.resolve(cfg.Root, st, arg)
		if os.MkdirAll(real, 0o755) == nil {
			reply(257, `"`+v+`" dibuat`)
		} else {
			reply(550, "Gagal membuat folder")
		}
	case "SIZE":
		_, real := s.resolve(cfg.Root, st, arg)
		if fi, err := os.Stat(real); err == nil && !fi.IsDir() {
			reply(213, strconv.FormatInt(fi.Size(), 10))
		} else {
			reply(550, "Tidak ada")
		}
	case "MDTM":
		_, real := s.resolve(cfg.Root, st, arg)
		if fi, err := os.Stat(real); err == nil {
			reply(213, fi.ModTime().UTC().Format("20060102150405"))
		} else {
			reply(550, "Tidak ada")
		}
	case "PASV", "EPSV":
		s.handlePasv(cmd, st, reply, conn)
	case "PORT":
		n := strings.Split(arg, ",")
		if len(n) != 6 {
			reply(501, "Format PORT salah")
			return false
		}
		nums := make([]int, 6)
		for i, v := range n {
			nums[i], _ = strconv.Atoi(strings.TrimSpace(v))
		}
		closePasv(st)
		st.active = &net.TCPAddr{
			IP:   net.ParseIP(fmt.Sprintf("%d.%d.%d.%d", nums[0], nums[1], nums[2], nums[3])),
			Port: nums[4]*256 + nums[5],
		}
		reply(200, "OK")
	case "LIST", "NLST", "MLSD":
		s.handleList(cmd, arg, cfg, st, reply)
	case "STOR", "APPE":
		s.handleStor(arg, cfg, st, reply, lg)
	case "DELE", "RMD", "XRMD":
		reply(550, "Menghapus tidak diizinkan")
	case "RNFR":
		_, real := s.resolve(cfg.Root, st, arg)
		if !exists(real) {
			reply(550, "Tidak ada")
			return false
		}
		st.renameFrom = real
		reply(350, "Lanjutkan RNTO")
	case "RNTO":
		if st.renameFrom == "" {
			reply(503, "RNFR dulu")
			return false
		}
		_, real := s.resolve(cfg.Root, st, arg)
		if os.Rename(st.renameFrom, real) == nil {
			reply(250, "OK")
		} else {
			reply(550, "Gagal")
		}
		st.renameFrom = ""
	default:
		reply(502, "Perintah tidak didukung")
	}
	return false
}

func (s *Server) handlePasv(cmd string, st *session, reply func(int, string), conn net.Conn) {
	closePasv(st)
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		reply(425, "Tidak bisa membuka koneksi data")
		return
	}
	st.pasvLn = ln
	ch := make(chan net.Conn, 1)
	st.pasvConn = ch
	go func() {
		_ = ln.(*net.TCPListener).SetDeadline(time.Now().Add(20 * time.Second))
		c, err := ln.Accept()
		if err == nil {
			ch <- c
		} else {
			close(ch)
		}
		_ = ln.Close()
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	if cmd == "EPSV" {
		reply(229, fmt.Sprintf("Mode pasif (|||%d|)", port))
		return
	}
	ip := hostOnly(conn.LocalAddr().String())
	h := strings.ReplaceAll(ip, ".", ",")
	if strings.Contains(ip, ":") {
		h = "127,0,0,1"
	}
	reply(227, fmt.Sprintf("Mode pasif (%s,%d,%d)", h, port>>8, port&255))
}

// openData membuka koneksi data (pasif atau aktif).
func (s *Server) openData(st *session, reply func(int, string)) net.Conn {
	if st.active != nil {
		addr := st.active
		st.active = nil
		c, err := net.DialTimeout("tcp", addr.String(), 15*time.Second)
		if err != nil {
			reply(425, "Koneksi data gagal: "+err.Error())
			return nil
		}
		reply(150, "Membuka koneksi data")
		return c
	}
	if st.pasvConn != nil {
		reply(150, "Membuka koneksi data")
		c, ok := <-st.pasvConn
		st.pasvConn, st.pasvLn = nil, nil
		if !ok {
			reply(425, "Kamera tidak membuka koneksi data")
			return nil
		}
		return c
	}
	reply(425, "Pakai PASV atau PORT dulu")
	return nil
}

func (s *Server) handleList(cmd, arg string, cfg Settings, st *session, reply func(int, string)) {
	if strings.HasPrefix(arg, "-") {
		arg = ""
	}
	_, real := s.resolve(cfg.Root, st, arg)
	entries, _ := os.ReadDir(real)

	data := s.openData(st, reply)
	if data == nil {
		return
	}
	var lines []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		switch cmd {
		case "NLST":
			lines = append(lines, e.Name())
		case "MLSD":
			size := int64(0)
			typ := "file"
			if fi, err := e.Info(); err == nil {
				size = fi.Size()
			}
			if e.IsDir() {
				typ = "dir"
			}
			lines = append(lines, fmt.Sprintf("type=%s;size=%d; %s", typ, size, e.Name()))
		default:
			fi, err := e.Info()
			var size int64
			mt := time.Now()
			if err == nil {
				size, mt = fi.Size(), fi.ModTime()
			}
			d := "-"
			if e.IsDir() {
				d = "d"
			}
			lines = append(lines, fmt.Sprintf("%srw-r--r-- 1 milesshoot milesshoot %10d %s %2d %02d:%02d %s",
				d, size, mt.Format("Jan"), mt.Day(), mt.Hour(), mt.Minute(), e.Name()))
		}
	}
	out := ""
	if len(lines) > 0 {
		out = strings.Join(lines, "\r\n") + "\r\n"
	}
	_, _ = io.WriteString(data, out)
	_ = data.Close()
	reply(226, "Selesai")
}

func (s *Server) handleStor(arg string, cfg Settings, st *session, reply func(int, string), lg func(string, string)) {
	_, real := s.resolve(cfg.Root, st, arg)
	dir := filepath.Dir(real)
	name := filepath.Base(real)
	_ = os.MkdirAll(dir, 0o755)

	// Tulis ke file sementara tersembunyi, baru diganti nama setelah lengkap,
	// supaya scanner tidak mengambil foto yang belum selesai terkirim.
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d.part", name, time.Now().UnixNano()))

	data := s.openData(st, reply)
	if data == nil {
		return
	}
	out, err := os.Create(tmp)
	if err != nil {
		_ = data.Close()
		reply(451, "Gagal menyimpan file")
		return
	}
	// Kamera keluar jangkauan Wi-Fi di tengah kiriman: batasi waktu nganggur 90 dtk.
	n, copyErr := io.Copy(out, &idleReader{c: data, idle: 90 * time.Second})
	closeErr := out.Close()
	_ = data.Close()

	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		lg("warn", name+" terputus saat dikirim")
		reply(426, "Transfer terputus")
		return
	}

	// Jangan timpa foto lama (nomor file kamera bisa berulang).
	final := real
	if exists(final) {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		for k := 1; ; k++ {
			cand := filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, k, ext))
			if !exists(cand) {
				final = cand
				break
			}
		}
	}
	// Windows: antivirus kadang memegang file sesaat → coba beberapa kali.
	renamed := false
	for k := 0; k < 10; k++ {
		if os.Rename(tmp, final) == nil {
			renamed = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !renamed {
		_ = os.Remove(tmp)
		lg("error", name+" gagal disimpan")
		reply(451, "Gagal menyimpan file")
		return
	}

	s.mu.Lock()
	s.files++
	s.bytes += n
	s.lastFile = filepath.Base(final)
	s.lastAt = time.Now().UnixMilli()
	s.mu.Unlock()

	lg("ok", fmt.Sprintf("diterima %s (%.1f MB)", filepath.Base(final), float64(n)/1e6))
	s.onFile(final)
	reply(226, "File diterima")
}

// resolve memetakan path virtual FTP ke path nyata di dalam Root (anti path traversal).
func (s *Server) resolve(root string, st *session, p string) (virt, real string) {
	v := p
	if !path.IsAbs(v) {
		v = path.Join(st.cwd, v)
	}
	v = path.Clean(v)
	var safe []string
	for _, seg := range strings.Split(v, "/") {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		safe = append(safe, seg)
	}
	return "/" + strings.Join(safe, "/"), filepath.Join(append([]string{root}, safe...)...)
}

// idleReader membatalkan baca bila nganggur lebih dari idle.
type idleReader struct {
	c    net.Conn
	idle time.Duration
}

func (r *idleReader) Read(p []byte) (int, error) {
	_ = r.c.SetReadDeadline(time.Now().Add(r.idle))
	return r.c.Read(p)
}

func closePasv(st *session) {
	if st.pasvLn != nil {
		_ = st.pasvLn.Close()
		st.pasvLn = nil
	}
	st.pasvConn = nil
}

func LanAddresses() []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip := ipn.IP.To4(); ip != nil && ip.IsPrivate() {
					out = append(out, ip.String())
				}
			}
		}
	}
	return out
}

func split2(line string) (string, string) {
	if i := strings.IndexByte(line, ' '); i >= 0 {
		return line[:i], line[i+1:]
	}
	return line, ""
}

// maskCmd menyembunyikan password di catatan debug.
func maskCmd(line string) string {
	if len(line) >= 4 && strings.EqualFold(line[:4], "PASS") {
		return "PASS ******"
	}
	return line
}

// hostOnly membuang port dan awalan IPv4-mapped IPv6 ("::ffff:").
func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		addr = h
	}
	return strings.TrimPrefix(addr, "::ffff:")
}

func isDir(p string) bool  { fi, err := os.Stat(p); return err == nil && fi.IsDir() }
func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func dedupe(in []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func rootCause(err error) error {
	if oe, ok := err.(*net.OpError); ok && oe.Err != nil {
		return oe.Err
	}
	return err
}

func (s *Server) isProbing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.probing
}
