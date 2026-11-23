package core

import "testing"

// Antrean kosong: tidak ada kecepatan, tidak ada ETA, "lama upload" nol — ini keadaan
// diam (sama seperti layar "Tidak ada yang diunggah" di MAS EDY).
func TestSpeedIdle(t *testing.T) {
	eng, err := NewEngine(t.TempDir(), NopEmitter{})
	if err != nil {
		t.Fatal(err)
	}
	sp := eng.Queue.Speed(0)
	if sp.Mbps != 0 || sp.PerMin != 0 || sp.EtaSec != -1 || sp.ElapsedSec != 0 {
		t.Fatalf("antrean kosong seharusnya semua nol/-1, dapat %+v", sp)
	}
}

// "Lama upload" mulai berjalan begitu ada file pending, dan berhenti (kembali ke 0)
// begitu antrean kosong lagi — supaya layar idle selalu menampilkan 0:00.
func TestSpeedElapsedTracksActiveSession(t *testing.T) {
	eng, err := NewEngine(t.TempDir(), NopEmitter{})
	if err != nil {
		t.Fatal(err)
	}
	q := eng.Queue

	sp := q.Speed(3) // 3 file pending → sesi mulai
	if sp.ElapsedSec != 0 {
		t.Fatalf("baru mulai, elapsed seharusnya 0, dapat %d", sp.ElapsedSec)
	}
	// mundurkan activeSince secara manual (akses field sepaket) buat simulasikan "5 detik lalu"
	q.statsMu.Lock()
	q.activeSince -= 5000
	q.statsMu.Unlock()

	sp = q.Speed(3) // masih ada pending → sesi tetap sama, bukan direset
	if sp.ElapsedSec < 4 || sp.ElapsedSec > 6 {
		t.Fatalf("elapsed seharusnya ~5 detik, dapat %d", sp.ElapsedSec)
	}

	sp = q.Speed(0) // antrean kosong → sesi berakhir
	if sp.ElapsedSec != 0 {
		t.Fatalf("antrean kosong seharusnya elapsed balik ke 0, dapat %d", sp.ElapsedSec)
	}
}

// Mbps dihitung dari byte yang benar-benar terunggah dalam jendela 30 detik terakhir —
// sampel yang sudah lewat 120 detik harus dibuang (jendela bergulir).
func TestSpeedMbpsFromRecentWindow(t *testing.T) {
	eng, err := NewEngine(t.TempDir(), NopEmitter{})
	if err != nil {
		t.Fatal(err)
	}
	q := eng.Queue
	now := nowMs()

	q.statsMu.Lock()
	// 10 MB terkirim 2 detik lalu → harus ikut terhitung (dalam jendela 30 dtk)
	q.sentWindow = append(q.sentWindow, sentSample{t: now - 2000, bytes: 10_000_000})
	// 50 MB terkirim 100 detik lalu → sudah basi, harus diabaikan
	q.sentWindow = append(q.sentWindow, sentSample{t: now - 100000, bytes: 50_000_000})
	q.statsMu.Unlock()

	sp := q.Speed(0)
	if sp.Mbps <= 0 {
		t.Fatalf("mbps seharusnya > 0 dari sampel 2 detik lalu, dapat %v", sp.Mbps)
	}
	// 10MB*8bit / minimal rentang 5 detik / 1e6 = 16 Mbps — beri toleransi longgar
	if sp.Mbps > 20 {
		t.Fatalf("mbps tidak masuk akal (sampel basi ikut terhitung?): %v", sp.Mbps)
	}
}

// ETA butuh minimal 3 sampel "selesai" dalam 120 detik terakhir supaya tidak melompat-lompat
// dari satu titik data saja — di bawah itu harus tetap -1 ("belum bisa diperkirakan").
func TestSpeedEtaNeedsEnoughSamples(t *testing.T) {
	eng, err := NewEngine(t.TempDir(), NopEmitter{})
	if err != nil {
		t.Fatal(err)
	}
	q := eng.Queue
	now := nowMs()

	q.statsMu.Lock()
	q.doneTimes = []int64{now - 1000, now - 500}
	q.statsMu.Unlock()
	if sp := q.Speed(10); sp.EtaSec != -1 {
		t.Fatalf("dengan cuma 2 sampel, ETA seharusnya -1, dapat %d", sp.EtaSec)
	}

	q.statsMu.Lock()
	q.doneTimes = []int64{now - 90000, now - 60000, now - 30000, now - 1000}
	q.statsMu.Unlock()
	sp := q.Speed(10)
	if sp.EtaSec < 0 {
		t.Fatalf("dengan 4 sampel + pending>0, ETA seharusnya terhitung, dapat %d", sp.EtaSec)
	}
}
