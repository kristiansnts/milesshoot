package update

import "os"

// TakeApplyLog membaca lalu menghapus catatan hasil pemasangan (ditulis skrip penukar).
// Dipanggil saat aplikasi baru menyala supaya hasilnya masuk ke log aplikasi.
func TakeApplyLog(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_ = os.Remove(path)
	return string(b)
}
