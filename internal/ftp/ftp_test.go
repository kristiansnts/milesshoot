package ftp

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

type client struct {
	c net.Conn
	r *bufio.Reader
	t *testing.T
}

func dial(t *testing.T, port int) *client {
	t.Helper()
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 3*time.Second)
	if err != nil {
		t.Fatalf("tidak bisa menyambung: %v", err)
	}
	cl := &client{c: c, r: bufio.NewReader(c), t: t}
	cl.expect(220)
	return cl
}

func (cl *client) line() string {
	cl.t.Helper()
	_ = cl.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	s, err := cl.r.ReadString('\n')
	if err != nil {
		cl.t.Fatalf("gagal baca balasan: %v", err)
	}
	return strings.TrimRight(s, "\r\n")
}

func (cl *client) expect(code int) string {
	cl.t.Helper()
	for {
		l := cl.line()
		// lewati baris lanjutan multi-line (mis. "211-Fitur:")
		if len(l) >= 4 && l[3] == '-' {
			continue
		}
		if !strings.HasPrefix(l, strconv.Itoa(code)) {
			cl.t.Fatalf("mau kode %d, dapat: %q", code, l)
		}
		return l
	}
}

func (cl *client) send(s string) {
	cl.t.Helper()
	if _, err := fmt.Fprintf(cl.c, "%s\r\n", s); err != nil {
		cl.t.Fatalf("gagal kirim %q: %v", s, err)
	}
}

var pasvRe = regexp.MustCompile(`\((\d+),(\d+),(\d+),(\d+),(\d+),(\d+)\)`)

func newServer(t *testing.T, root string) (*Server, int, chan string) {
	t.Helper()
	port := 21300 + (int(time.Now().UnixNano()/1e6) % 300)
	got := make(chan string, 8)
	s := New(
		func() Settings {
			return Settings{Enabled: true, Port: port, User: "kamera", Pass: "rahasia123", Root: root}
		},
		func(level, msg string) { t.Logf("[ftp %s] %s", level, msg) },
		func(p string) { got <- p },
	)
	s.Start()
	info := s.Info()
	if !info.Running {
		t.Fatalf("server FTP tidak menyala: %s", info.Error)
	}
	t.Cleanup(s.Stop)
	return s, info.Port, got
}

// Kamera login lalu mengirim foto → file mendarat utuh di folder yang dipantau.
func TestStorePhoto(t *testing.T) {
	root := t.TempDir()
	s, port, got := newServer(t, root)

	cl := dial(t, port)
	cl.send("USER kamera")
	cl.expect(331)
	cl.send("PASS rahasia123")
	cl.expect(230)
	cl.send("TYPE I")
	cl.expect(200)

	cl.send("PASV")
	pasv := cl.expect(227)
	m := pasvRe.FindStringSubmatch(pasv)
	if m == nil {
		t.Fatalf("balasan PASV tak terbaca: %q", pasv)
	}
	p1, _ := strconv.Atoi(m[5])
	p2, _ := strconv.Atoi(m[6])
	dataPort := p1*256 + p2

	data, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", dataPort), 3*time.Second)
	if err != nil {
		t.Fatalf("gagal buka koneksi data: %v", err)
	}
	cl.send("STOR DSC_0001.JPG")
	cl.expect(150)

	payload := strings.Repeat("FOTO", 5000) // 20 KB
	if _, err := data.Write([]byte(payload)); err != nil {
		t.Fatalf("gagal kirim data: %v", err)
	}
	_ = data.Close()
	cl.expect(226)

	// file ada & isinya utuh
	target := filepath.Join(root, "DSC_0001.JPG")
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("file tidak tersimpan: %v", err)
	}
	if string(b) != payload {
		t.Fatalf("isi file tidak utuh: %d byte (mau %d)", len(b), len(payload))
	}
	// tidak menyisakan file .part
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("file sementara tertinggal: %s", e.Name())
		}
	}
	// callback onFile terpanggil
	select {
	case p := <-got:
		if filepath.Base(p) != "DSC_0001.JPG" {
			t.Fatalf("callback salah file: %s", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback onFile tidak terpanggil")
	}

	info := s.Info()
	if info.Files != 1 || info.Bytes != int64(len(payload)) {
		t.Fatalf("statistik salah: %+v", info)
	}
	if info.LastFile != "DSC_0001.JPG" {
		t.Fatalf("lastFile salah: %s", info.LastFile)
	}
	cl.send("QUIT")
	cl.expect(221)
}

// Nama file kembar tidak boleh menimpa foto lama (nomor kamera bisa berulang).
func TestNoOverwrite(t *testing.T) {
	root := t.TempDir()
	_, port, _ := newServer(t, root)
	if err := os.WriteFile(filepath.Join(root, "A.JPG"), []byte("lama"), 0o644); err != nil {
		t.Fatal(err)
	}

	cl := dial(t, port)
	cl.send("USER kamera")
	cl.expect(331)
	cl.send("PASS rahasia123")
	cl.expect(230)
	cl.send("PASV")
	m := pasvRe.FindStringSubmatch(cl.expect(227))
	p1, _ := strconv.Atoi(m[5])
	p2, _ := strconv.Atoi(m[6])
	data, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p1*256+p2), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cl.send("STOR A.JPG")
	cl.expect(150)
	_, _ = data.Write([]byte("baru"))
	_ = data.Close()
	cl.expect(226)

	if b, _ := os.ReadFile(filepath.Join(root, "A.JPG")); string(b) != "lama" {
		t.Fatalf("foto lama tertimpa! isi sekarang: %q", string(b))
	}
	if b, err := os.ReadFile(filepath.Join(root, "A_1.JPG")); err != nil || string(b) != "baru" {
		t.Fatalf("foto baru tidak disimpan sebagai A_1.JPG (err=%v)", err)
	}
}

// Password salah ditolak, dan tanpa login perintah apa pun ditolak.
func TestAuthRequired(t *testing.T) {
	root := t.TempDir()
	_, port, _ := newServer(t, root)

	cl := dial(t, port)
	cl.send("STOR x.jpg") // belum login
	cl.expect(530)
	cl.send("USER kamera")
	cl.expect(331)
	cl.send("PASS salah")
	cl.expect(530)
}

// Path traversal tidak boleh keluar dari folder yang dipantau.
func TestNoTraversal(t *testing.T) {
	root := t.TempDir()
	_, port, _ := newServer(t, root)

	cl := dial(t, port)
	cl.send("USER kamera")
	cl.expect(331)
	cl.send("PASS rahasia123")
	cl.expect(230)
	cl.send("PASV")
	m := pasvRe.FindStringSubmatch(cl.expect(227))
	p1, _ := strconv.Atoi(m[5])
	p2, _ := strconv.Atoi(m[6])
	data, _ := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p1*256+p2), 3*time.Second)
	cl.send("STOR ../../keluar.jpg")
	cl.expect(150)
	_, _ = data.Write([]byte("x"))
	_ = data.Close()
	cl.expect(226)

	// harus mendarat DI DALAM root, bukan di atasnya
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "keluar.jpg")); err == nil {
		t.Fatal("file lolos keluar dari folder sumber!")
	}
	if _, err := os.Stat(filepath.Join(root, "keluar.jpg")); err != nil {
		t.Fatalf("file tidak mendarat di dalam root: %v", err)
	}
}
