package core

import (
	"testing"
	"time"
)

func TestRankSince(t *testing.T) {
	now := time.Now()
	if got := rankSince("all"); got != 0 {
		t.Fatalf("all harus 0 (tanpa batas), dapat %d", got)
	}
	today := rankSince("today")
	y, m, d := now.Date()
	wantToday := time.Date(y, m, d, 0, 0, 0, 0, time.Local).UnixMilli()
	if today != wantToday {
		t.Fatalf("today salah: dapat %d, mau %d", today, wantToday)
	}
	if got := rankSince("d7"); got >= now.UnixMilli() || got <= now.AddDate(0, 0, -8).UnixMilli() {
		t.Fatalf("d7 di luar rentang wajar: %d", got)
	}
	if got := rankSince("tak-dikenal"); got != 0 {
		t.Fatalf("mode tak dikenal harus 0 (tanpa batas), dapat %d", got)
	}
}

func TestRankKey(t *testing.T) {
	// karakter aneh di ID pohon dibersihkan supaya aman jadi nama file
	if got := rankKey("abc-123", "today"); got != "abc-123-today" {
		t.Fatalf("dapat %q", got)
	}
	if got := rankKey("../../etc/passwd", "all"); got != "etcpasswd-all" {
		t.Fatalf("karakter path traversal harus dibersihkan, dapat %q", got)
	}
}

func TestSortedRows(t *testing.T) {
	members := map[string]*RankRow{
		"a": {ID: "a", Username: "zidan", Count: 5},
		"b": {ID: "b", Username: "budi", Count: 10},
		"c": {ID: "c", Username: "ahmad", Count: 10}, // seri dengan b → diurutkan nama
	}
	order := []string{"a", "b", "c"}
	rows := sortedRows(members, order)
	if len(rows) != 3 {
		t.Fatalf("harus 3 baris, dapat %d", len(rows))
	}
	// terbanyak dulu; seri → nama abjad
	if rows[0].Username != "ahmad" || rows[0].Count != 10 {
		t.Fatalf("peringkat 1 salah: %+v", rows[0])
	}
	if rows[1].Username != "budi" || rows[1].Count != 10 {
		t.Fatalf("peringkat 2 salah: %+v", rows[1])
	}
	if rows[2].Username != "zidan" || rows[2].Count != 5 {
		t.Fatalf("peringkat 3 salah: %+v", rows[2])
	}
}

func TestParseFotoyuTime(t *testing.T) {
	if got := parseFotoyuTime(""); got != 0 {
		t.Fatalf("kosong harus 0, dapat %d", got)
	}
	if got := parseFotoyuTime("bukan-tanggal"); got != 0 {
		t.Fatalf("format rusak harus 0, dapat %d", got)
	}
	want := time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC).UnixMilli()
	if got := parseFotoyuTime("2026-03-15T10:30:00Z"); got != want {
		t.Fatalf("RFC3339: dapat %d, mau %d", got, want)
	}
}

// Simulasi agregasi daun tanpa jaringan: pastikan video ikut dihitung, "last" diambil
// yang paling baru, dan berhenti saat daun sudah lebih tua dari rentang.
func TestAggregationLogic(t *testing.T) {
	type fakeLeaf struct {
		createdAt string
		kind      string
		memberID  string
		username  string
	}
	now := time.Now()
	leaves := []fakeLeaf{
		{now.Format(time.RFC3339), "photo", "u1", "budi"},
		{now.Add(-1 * time.Hour).Format(time.RFC3339), "video", "u1", "budi"},
		{now.Add(-2 * time.Hour).Format(time.RFC3339), "photo", "u2", "sari"},
		{now.AddDate(0, 0, -10).Format(time.RFC3339), "photo", "u1", "budi"}, // di luar rentang 7 hari
	}
	since := rankSince("d7")
	members := map[string]*RankRow{}
	order := []string{}
	scanned := 0
	for _, l := range leaves {
		tms := parseFotoyuTime(l.createdAt)
		if since != 0 && tms != 0 && tms < since {
			break // FotoYu mengirim terbaru dulu → berhenti di sini
		}
		row, ok := members[l.memberID]
		if !ok {
			row = &RankRow{ID: l.memberID, Username: l.username}
			members[l.memberID] = row
			order = append(order, l.memberID)
		}
		row.Count++
		if l.kind == "video" {
			row.Videos++
		}
		if tms > row.LastAt {
			row.LastAt = tms
		}
		scanned++
	}
	if scanned != 3 {
		t.Fatalf("harus berhenti sebelum daun 10 hari lalu, scanned=%d", scanned)
	}
	rows := sortedRows(members, order)
	if len(rows) != 2 {
		t.Fatalf("harus 2 akun, dapat %d", len(rows))
	}
	budi := rows[0]
	if budi.Username != "budi" || budi.Count != 2 || budi.Videos != 1 {
		t.Fatalf("budi salah: %+v", budi)
	}
}
