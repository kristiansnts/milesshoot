//go:build darwin

package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// swapScript berjalan terpisah dari aplikasi: menunggu proses lama keluar, menyalin versi baru
// ke samping bundle lama, menukar, dan mengembalikan versi lama bila ada langkah yang gagal.
// Semua nilai masuk lewat argumen posisi ($1..$5), tidak pernah disisipkan ke teks skrip.
const swapScript = `
PID="$1"; BUNDLE="$2"; SRC="$3"; LOG="$4"; RELAUNCH="$5"
log() { printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >> "$LOG"; }
reopen() { [ "$RELAUNCH" = "1" ] && open "$BUNDLE"; }
i=0
while kill -0 "$PID" 2>/dev/null; do
  i=$((i+1))
  if [ "$i" -gt 200 ]; then log "GAGAL: aplikasi lama tidak menutup dalam 60 dtk"; exit 1; fi
  sleep 0.3
done
NEW="$BUNDLE.update-new"; OLD="$BUNDLE.update-old"
rm -rf "$NEW" "$OLD"
if ! ditto "$SRC" "$NEW"; then log "GAGAL: menyalin versi baru"; rm -rf "$NEW"; reopen; exit 1; fi
if ! mv "$BUNDLE" "$OLD"; then log "GAGAL: memindahkan versi lama"; rm -rf "$NEW"; reopen; exit 1; fi
if ! mv "$NEW" "$BUNDLE"; then
  log "GAGAL: memasang versi baru — versi lama dikembalikan"
  mv "$OLD" "$BUNDLE"; reopen; exit 1
fi
rm -rf "$OLD"
log "OK: terpasang di $BUNDLE"
reopen
exit 0
`

// CurrentBundle: lokasi .app yang sedang berjalan.
func CurrentBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	for p := exe; p != "/" && p != "."; p = filepath.Dir(p) {
		if strings.HasSuffix(p, ".app") {
			return p, nil
		}
	}
	return "", errors.New("aplikasi tidak berjalan dari paket .app (mode pengembangan) — pembaruan otomatis dilewati")
}

// CanReplace: folder tempat .app berada harus bisa ditulis (mis. /Applications milik pemakai admin,
// atau ~/Applications). Dicek sebelum mengunduh supaya galat muncul lebih awal.
func CanReplace() error {
	b, err := CurrentBundle()
	if err != nil {
		return err
	}
	dir := filepath.Dir(b)
	f, err := os.CreateTemp(dir, ".milesshoot-write-test-*")
	if err != nil {
		return fmt.Errorf("folder %s tidak bisa ditulis — pindahkan aplikasi ke Applications lalu coba lagi", dir)
	}
	f.Close()
	return os.Remove(f.Name())
}

// Apply menjadwalkan penukaran lalu segera kembali; pemanggil WAJIB menutup aplikasi sesudahnya.
// newApp = .app hasil Extract. logPath = tempat skrip menulis hasilnya.
func Apply(newApp, bundle string, pid int, logPath string, relaunch bool) error {
	if bundle == "" {
		var err error
		if bundle, err = CurrentBundle(); err != nil {
			return err
		}
	}
	if fi, err := os.Stat(filepath.Join(newApp, "Contents", "MacOS")); err != nil || !fi.IsDir() {
		return errors.New("paket .app baru tidak lengkap")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	rl := "0"
	if relaunch {
		rl = "1"
	}
	cmd := exec.Command("/bin/sh", "-c", swapScript, "sh", strconv.Itoa(pid), bundle, newApp, logPath, rl)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // lepas dari aplikasi supaya tetap hidup setelah ia keluar
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gagal menjalankan pemasang: %w", err)
	}
	return cmd.Process.Release()
}
