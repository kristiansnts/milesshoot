package core

import (
	"strings"
	"testing"
)

func TestDestSummary(t *testing.T) {
	cfg := Settings{Destination: "fotoyu", Event: Event{Price: 25000, VideoPrice: 40000,
		Tree: &Tree{Name: "Lari Pagi", TagID: "tag-77", Location: Location{Name: "GBK"}}}}
	got := DestSummary(cfg, "kalachitrasport")
	for _, want := range []string{"tujuan=fotoyu", "akun=@kalachitrasport", `FotoTree="Lari Pagi"`, "tag=tag-77", "lokasi=GBK", "harga=Rp25000", "hargaVideo=Rp40000"} {
		if !strings.Contains(got, want) {
			t.Errorf("ringkasan tidak memuat %q: %s", want, got)
		}
	}
	if s := DestSummary(Settings{Destination: "fotoyu"}, ""); !strings.Contains(s, "belum masuk") || !strings.Contains(s, "belum dipilih") {
		t.Errorf("kondisi belum siap harus terlihat: %s", s)
	}
	if s := DestSummary(Settings{Destination: "local", LocalOutputFolder: "/x"}, ""); !strings.Contains(s, "tujuan=local") || strings.Contains(s, "akun") {
		t.Errorf("tujuan lokal: %s", s)
	}
}

func TestUploadedDetail(t *testing.T) {
	cfg := Settings{Destination: "fotoyu", Event: Event{Tree: &Tree{Name: "Lari Pagi", TagID: "tag-77"}}}
	got := uploadedDetail(cfg, UploadResult{Account: "kala", RemoteID: "c-1", Quality: "hd"}, 25000, 2048*1024)
	for _, want := range []string{"@kala", `FotoTree "Lari Pagi" (tag tag-77)`, "Rp25000", "id=c-1", "kualitas=hd", "2048KB"} {
		if !strings.Contains(got, want) {
			t.Errorf("rincian tidak memuat %q: %s", want, got)
		}
	}
	if s := uploadedDetail(Settings{Destination: "local", LocalOutputFolder: "/hasil"}, UploadResult{}, 0, 0); !strings.Contains(s, "folder lokal /hasil") {
		t.Errorf("tujuan lokal: %s", s)
	}
}
