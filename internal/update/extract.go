package update

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxExtractBytes = 1 << 30

// Extract membuka zip ke dest dengan penjagaan: tanpa path keluar dari dest (zip-slip),
// tanpa symlink, dan dengan batas total ukuran. Hanya bit eksekusi yang dipertahankan.
func Extract(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("zip pembaruan tidak terbaca: %w", err)
	}
	defer zr.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	root, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	var total int64
	for _, f := range zr.File {
		name := f.Name
		if strings.HasPrefix(name, "__MACOSX/") || strings.HasSuffix(name, ".DS_Store") || strings.HasPrefix(filepath.Base(name), "._") {
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip pembaruan memuat symlink (%s) — ditolak", name)
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("zip pembaruan memuat path berbahaya (%s) — ditolak", name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > maxExtractBytes {
			return fmt.Errorf("isi zip pembaruan melewati batas ukuran")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.Mode()&0o111 != 0 {
			mode = 0o755
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		_, cerr := io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
		rc.Close()
		if err := out.Close(); err != nil && cerr == nil {
			cerr = err
		}
		if cerr != nil {
			return cerr
		}
	}
	return nil
}

// FindApp mengembalikan satu-satunya bundle .app (macOS) di tingkat atas dir hasil Extract.
func FindApp(dir string) (string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var apps []string
	for _, e := range ents {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".app") {
			apps = append(apps, filepath.Join(dir, e.Name()))
		}
	}
	if len(apps) != 1 {
		return "", fmt.Errorf("zip pembaruan harus berisi tepat satu .app (ditemukan %d)", len(apps))
	}
	return apps[0], nil
}

// FindPayload: berkas yang akan dipasang di dalam hasil Extract — .app (macOS) atau milesshoot.exe (Windows).
func FindPayload(dir, goos string) (string, error) {
	if goos == "windows" {
		p := filepath.Join(dir, "milesshoot.exe")
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			return "", fmt.Errorf("zip pembaruan harus berisi milesshoot.exe di tingkat atas")
		}
		return p, nil
	}
	return FindApp(dir)
}
