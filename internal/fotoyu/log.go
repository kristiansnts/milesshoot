package fotoyu

import "strings"

// Logf menerima catatan aktivitas (level: info|warn|error|ok). Dipasang main.go ke log aplikasi;
// bawaannya tidak melakukan apa-apa supaya paket ini bisa dites sendiri.
var Logf = func(level, msg string) {}

// snip memotong teks balasan server untuk log (satu baris, maks n karakter).
func snip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
