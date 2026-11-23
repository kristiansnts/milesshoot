package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAllWatchFolders(t *testing.T) {
	s := Settings{WatchFolder: "/a", WatchFolders: []string{"/b", "/a", "", "/c"}}
	got := AllWatchFolders(s)
	want := []string{"/a", "/b", "/c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, mau %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, mau %v", got, want)
		}
	}
}

func TestRootFor(t *testing.T) {
	s := Settings{WatchFolder: "/foto/A", WatchFolders: []string{"/foto/B"}}
	if r := RootFor(s, "/foto/A/sub/x.jpg"); r != "/foto/A" {
		t.Errorf("mau /foto/A, dapat %q", r)
	}
	if r := RootFor(s, "/foto/B/y.jpg"); r != "/foto/B" {
		t.Errorf("mau /foto/B, dapat %q", r)
	}
	if r := RootFor(s, "/lain/z.jpg"); r != "" {
		t.Errorf("file di luar folder manapun seharusnya \"\", dapat %q", r)
	}
}

// ArchiveDir untuk file di folder TAMBAHAN harus membuat "Sudah upload" di BAWAH folder
// itu sendiri, bukan selalu di bawah folder utama — ini yang dulu salah sebelum RootFor ada.
func TestArchiveDirPerFolder(t *testing.T) {
	s := Settings{
		WatchFolder:  "/foto/A",
		WatchFolders: []string{"/foto/B"},
		AfterUpload:  AfterUpload{Move: true, Folder: "Sudah upload"},
	}
	gotA := ArchiveDir(s, "/foto/A/x.jpg")
	wantA := filepath.Join("/foto/A", "Sudah upload")
	if gotA != wantA {
		t.Errorf("folder utama: mau %q, dapat %q", wantA, gotA)
	}
	gotB := ArchiveDir(s, "/foto/B/y.jpg")
	wantB := filepath.Join("/foto/B", "Sudah upload")
	if gotB != wantB {
		t.Errorf("folder tambahan: mau %q, dapat %q — seharusnya arsip ikut folder asalnya sendiri", wantB, gotB)
	}
}

// InArchive juga harus dicek relatif ke folder asal file, bukan cuma folder utama —
// supaya scanner tidak memindai ulang isi "Sudah upload" milik folder tambahan.
func TestInArchivePerFolder(t *testing.T) {
	s := Settings{
		WatchFolder:  "/foto/A",
		WatchFolders: []string{"/foto/B"},
		AfterUpload:  AfterUpload{Move: true, Folder: "Sudah upload"},
	}
	if !InArchive(s, filepath.Join("/foto/B", "Sudah upload", "y.jpg")) {
		t.Error("file di dalam 'Sudah upload' milik folder B seharusnya dianggap arsip")
	}
	if InArchive(s, "/foto/B/y.jpg") {
		t.Error("file biasa di folder B (bukan di subfolder arsip) seharusnya bukan arsip")
	}
}

// Pipeline nyata: dua folder sumber berjalan BERSAMAAN, file dari keduanya ikut
// terunggah, dan FolderProgress melaporkan hitungan terpisah per folder.
func TestMultiFolderPipeline(t *testing.T) {
	dataDir := t.TempDir()
	folderA := t.TempDir()
	folderB := t.TempDir()

	eng, err := NewEngine(dataDir, NopEmitter{})
	if err != nil {
		t.Fatal(err)
	}
	eng.Cfg.Set(func(s *Settings) {
		s.WatchFolder = folderA
		s.WatchFolders = []string{folderB}
		s.Destination = "local"
		s.LocalOutputFolder = filepath.Join(dataDir, "hasil")
		s.AutoUpload = true
		s.ScanIntervalSec = 1
	})
	eng.Start()
	defer eng.Stop()

	writeTestJPEG(t, filepath.Join(folderA, "A1.jpg"), 1)
	writeTestJPEG(t, filepath.Join(folderB, "B1.jpg"), 2)
	writeTestJPEG(t, filepath.Join(folderB, "B2.jpg"), 3)

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if eng.Store.Counts().Done >= 3 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	c := eng.Store.Counts()
	if c.Done < 3 {
		t.Fatalf("mau 3 file selesai dari 2 folder, dapat counts=%+v", c)
	}

	stats := eng.Queue.FolderProgress()
	if len(stats) != 2 {
		t.Fatalf("mau 2 entri FolderStat (A dan B), dapat %d: %+v", len(stats), stats)
	}
	byFolder := map[string]FolderStat{}
	for _, s := range stats {
		byFolder[s.Folder] = s
	}
	if byFolder[folderA].Done != 1 {
		t.Errorf("folder A seharusnya 1 selesai, dapat %+v", byFolder[folderA])
	}
	if byFolder[folderB].Done != 2 {
		t.Errorf("folder B seharusnya 2 selesai, dapat %+v", byFolder[folderB])
	}
	if byFolder[folderA].Pct != 100 || byFolder[folderB].Pct != 100 {
		t.Errorf("kedua folder seharusnya 100%% selesai: A=%d%%, B=%d%%", byFolder[folderA].Pct, byFolder[folderB].Pct)
	}

	// arsip folder B harus ada DI BAWAH folder B sendiri, bukan tercampur ke folder A
	if _, err := os.Stat(filepath.Join(folderB, "Sudah upload")); err != nil {
		t.Errorf("folder B seharusnya punya 'Sudah upload' sendiri: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folderA, "Sudah upload", "B1.jpg")); err == nil {
		t.Error("file folder B seharusnya TIDAK nyasar ke arsip folder A")
	}
}
