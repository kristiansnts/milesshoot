package video

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// extractEmbeddedFfmpeg menulis ffmpeg tertanam ke folder cache pengguna (sekali; nama berkas
// memuat hash isinya, jadi versi baru tak bentrok dengan yang lama) dan mengembalikan path-nya.
// Kosong bila build ini tidak menanam ffmpeg atau penulisan gagal.
func extractEmbeddedFfmpeg(name string) string {
	if len(embeddedFfmpeg) == 0 {
		return ""
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	sum := sha256.Sum256(embeddedFfmpeg)
	dir := filepath.Join(base, "milesshoot", "ffmpeg-"+hex.EncodeToString(sum[:4]))
	dst := filepath.Join(dir, name)
	if fi, err := os.Stat(dst); err == nil && fi.Size() == int64(len(embeddedFfmpeg)) {
		return dst
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	tmp := dst + ".part"
	if err := os.WriteFile(tmp, embeddedFfmpeg, 0o755); err != nil {
		return ""
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return ""
	}
	return dst
}
