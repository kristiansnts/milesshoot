package update

import (
	"fmt"
	"strconv"
	"strings"
)

// parseVersion membaca "v1.2.3" / "1.2.3" (tanpa pra-rilis). Tag pra-rilis ("1.2.3-beta") ditolak
// supaya pembaruan otomatis tidak pernah memasang versi percobaan.
func parseVersion(v string) ([3]int, error) {
	var out [3]int
	s := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), "v"), "V")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if s == "" || strings.ContainsAny(s, "-") {
		return out, fmt.Errorf("nomor versi %q tidak dikenali", v)
	}
	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return out, fmt.Errorf("nomor versi %q tidak dikenali", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, fmt.Errorf("nomor versi %q tidak dikenali", v)
		}
		out[i] = n
	}
	return out, nil
}

// Newer: true bila latest lebih baru dari current.
func Newer(latest, current string) (bool, error) {
	l, err := parseVersion(latest)
	if err != nil {
		return false, err
	}
	c, err := parseVersion(current)
	if err != nil {
		return false, fmt.Errorf("versi aplikasi saat ini: %w", err)
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i], nil
		}
	}
	return false, nil
}
