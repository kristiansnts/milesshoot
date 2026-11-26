package core

import (
	"fmt"
	"strings"
)

// DestSummary: satu baris yang menjawab "foto ini naik ke mana": tujuan, akun FotoYu, FotoTree
// (nama, tag, lokasi), dan harga. Dipakai di log awal sesi, saat tujuan diubah, dan di laporan.
func DestSummary(cfg Settings, account string) string {
	if cfg.Destination != "fotoyu" {
		return fmt.Sprintf("tujuan=%s · folder=%q", cfg.Destination, cfg.LocalOutputFolder)
	}
	parts := []string{"tujuan=fotoyu"}
	if account != "" {
		parts = append(parts, "akun=@"+account)
	} else {
		parts = append(parts, "akun=(belum masuk)")
	}
	if t := cfg.Event.Tree; t != nil {
		tree := fmt.Sprintf("FotoTree=%q (tag=%s", t.Name, t.TagID)
		if t.Location.Name != "" {
			tree += ", lokasi=" + t.Location.Name
		}
		parts = append(parts, tree+")")
	} else {
		parts = append(parts, "FotoTree=(belum dipilih)")
	}
	price := fmt.Sprintf("harga=Rp%d", cfg.Event.Price)
	if cfg.Event.VideoPrice > 0 {
		price += fmt.Sprintf(" · hargaVideo=Rp%d", cfg.Event.VideoPrice)
	}
	parts = append(parts, price)
	return strings.Join(parts, " · ")
}

// uploadedDetail: bagian akhir baris "Terunggah: …" — ke mana persisnya foto itu naik.
func uploadedDetail(cfg Settings, res UploadResult, price int, bytesOut int64) string {
	if cfg.Destination != "fotoyu" {
		return fmt.Sprintf(" → folder lokal %s", cfg.LocalOutputFolder)
	}
	parts := []string{"@" + res.Account}
	if t := cfg.Event.Tree; t != nil {
		parts = append(parts, fmt.Sprintf("FotoTree %q (tag %s)", t.Name, t.TagID))
	}
	parts = append(parts, fmt.Sprintf("Rp%d", price))
	if res.RemoteID != "" {
		parts = append(parts, "id="+res.RemoteID)
	}
	if res.Quality != "" {
		parts = append(parts, "kualitas="+res.Quality)
	}
	parts = append(parts, fmt.Sprintf("%dKB", bytesOut/1024))
	return " → " + strings.Join(parts, " · ")
}
