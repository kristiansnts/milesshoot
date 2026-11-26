//go:build windows

package update

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func CurrentBundle() (string, error) { return os.Executable() }

func CanReplace() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(exe), ".milesshoot-write-test-*")
	if err != nil {
		return fmt.Errorf("folder %s tidak bisa ditulis — pindahkan aplikasi ke folder milik Anda", filepath.Dir(exe))
	}
	f.Close()
	return os.Remove(f.Name())
}

// Apply (Windows): exe yang sedang berjalan boleh di-rename (tidak boleh ditimpa). Exe lama
// jadi .old (dihapus CleanupOld saat start berikutnya), exe baru ditaruh di tempatnya, lalu dijalankan.
func Apply(newExe, bundle string, pid int, logPath string, relaunch bool) error {
	exe := bundle
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("tidak bisa memindahkan versi lama: %w", err)
	}
	if err := copyFile(newExe, exe); err != nil {
		_ = os.Remove(exe)
		if rerr := os.Rename(old, exe); rerr != nil {
			return fmt.Errorf("pemasangan gagal (%v) dan versi lama tidak bisa dikembalikan: %v", err, rerr)
		}
		return fmt.Errorf("gagal menyalin versi baru: %w", err)
	}
	if relaunch {
		// jeda ±3 dtk lewat ping supaya proses lama sempat keluar (melepas port & berkas) sebelum yang baru menyala
		c := exec.Command("cmd", "/C", "ping -n 4 127.0.0.1 >nul & start \"\" \""+exe+"\"")
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		if err := c.Start(); err != nil {
			return fmt.Errorf("versi baru terpasang tapi gagal dijalankan: %w", err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// CleanupOld menghapus sisa exe lama setelah pembaruan.
func CleanupOld() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}
