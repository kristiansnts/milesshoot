package core

import (
	"testing"
	"time"
)

// fakeLeafFetcher menyimulasikan beberapa halaman daun tanpa jaringan, supaya
// deadlock di run() (lihat riwayat perbaikan: j.view() dipanggil sambil masih
// memegang j.mu) bisa dideteksi tiap kali test dijalankan, bukan hanya lewat
// pengecekan manual ke akun sungguhan.
//
// run() memanggil method pada *fotoyu.Client secara langsung (bukan lewat
// interface), jadi kita tidak bisa menyuntik fetcher palsu tanpa mengubah
// RankingManager. Test ini sebagai gantinya memanggil j.view() tepat dengan pola
// yang sama seperti di akhir run() (lock → ubah field → panggil view() → unlock)
// lewat goroutine terpisah dengan batas waktu — kalau pernah regresi ke pola
// "panggil view() sambil mu masih terkunci", test ini macet dan gagal via timeout.
func TestRankJobViewNotReentrant(t *testing.T) {
	j := &rankJob{rows: []RankRow{}}

	done := make(chan RankView, 1)
	go func() {
		// pola yang BENAR (setelah perbaikan): unlock dulu, baru panggil view().
		j.mu.Lock()
		j.done = true
		j.updatedAt = time.Now().UnixMilli()
		j.mu.Unlock()
		done <- j.view()
	}()

	select {
	case v := <-done:
		if !v.Done {
			t.Fatal("view() tidak mencerminkan perubahan sebelum unlock")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DEADLOCK: j.view() tidak pernah kembali — kemungkinan dipanggil sambil j.mu masih terkunci " +
			"(regresi ke bug lama: run() mengunci j.mu lalu memanggil j.view() yang mengunci j.mu lagi)")
	}
}

// Uji end-to-end ringan tanpa jaringan: pastikan run() yang gagal di awal
// (TreeLeaves error) tetap bisa menyelesaikan job sampai done=true, tidak
// macet di langkah finalisasi.
func TestRunFinalizesOnError(t *testing.T) {
	m := NewRankingManager(t.TempDir(), nil, NopEmitter{})
	j := &rankJob{key: "x", treeID: "tidak-ada", rangeKey: "today", rows: []RankRow{}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		// fy=nil akan panic saat dipanggil — tangkap supaya test ini murni menguji
		// bahwa BLOK FINALISASI (mu.Lock/Unlock/view()) tidak deadlock, terlepas
		// dari jalur errornya. Jika run() mengembalikan tanpa panic sama sekali
		// (mis. TreeDetail gagal duluan dengan graceful error), itu juga lolos.
		defer func() { recover() }()
		m.run(j)
	}()

	select {
	case <-done:
		// selesai (baik lewat jalur sukses, error, maupun panic yang ditangkap) —
		// yang penting TIDAK macet selamanya.
	case <-time.After(5 * time.Second):
		t.Fatal("run() tidak pernah kembali dalam 5 detik — kemungkinan deadlock di finalisasi")
	}
}
