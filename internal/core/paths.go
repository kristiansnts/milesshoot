package core

import (
	"path/filepath"
	"strings"
)

var videoExtSet = map[string]bool{
	".mp4": true, ".mov": true, ".m4v": true, ".avi": true, ".mts": true,
	".m2ts": true, ".mkv": true, ".3gp": true, ".webm": true,
}

func isVideo(path string) bool { return videoExtSet[strings.ToLower(filepath.Ext(path))] }

// AllWatchFolders: semua folder sumber aktif — WatchFolder (utama) + WatchFolders
// (tambahan), kosong & duplikat disaring. Dipakai di mana pun yang dulunya cuma
// membaca WatchFolder sendirian (scanner, hires, pratinjau saringan, dst).
func AllWatchFolders(s Settings) []string {
	out := []string{}
	if s.WatchFolder != "" {
		out = append(out, s.WatchFolder)
	}
	for _, f := range s.WatchFolders {
		if f != "" && f != s.WatchFolder {
			out = append(out, f)
		}
	}
	return out
}

// RootFor: folder sumber (dari AllWatchFolders) yang menaungi p, atau "" bila p
// tidak ada di bawah folder sumber mana pun yang sedang aktif.
func RootFor(s Settings, p string) string {
	if p == "" {
		return ""
	}
	for _, root := range AllWatchFolders(s) {
		rel, err := filepath.Rel(root, p)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return root
		}
	}
	return ""
}

// ArchiveDir: lokasi folder "Sudah upload" untuk sebuah file ("" bila fitur mati / tak ada
// folder sumber). Dihitung relatif ke folder sumber TEMPAT file itu berasal (penting begitu
// ada lebih dari satu folder sumber — tiap folder punya "Sudah upload" sendiri).
// Video dipisah ke foldernya sendiri; ganjil/genap ke folder masing-masing.
func ArchiveDir(s Settings, filePath string) string {
	root := s.WatchFolder
	if filePath != "" {
		if r := RootFor(s, filePath); r != "" {
			root = r
		}
	}
	if !s.AfterUpload.Move || root == "" {
		return ""
	}
	name := s.AfterUpload.Folder
	if filePath != "" && isVideo(filePath) {
		v := s.AfterUpload.FolderVideo
		if v == "" {
			v = "Sudah upload video"
		}
		return filepath.Join(root, v)
	}
	if filePath != "" && s.Parity.Mode != "all" {
		switch ParityOf(filePath) {
		case "odd":
			name = s.AfterUpload.FolderOdd
		case "even":
			name = s.AfterUpload.FolderEven
		}
	}
	return filepath.Join(root, name)
}

// InArchive: apakah path ada di dalam salah satu folder "Sudah upload" (selalu dilewati
// pemindai) — dicek relatif ke folder sumber mana pun yang menaungi path tersebut.
func InArchive(s Settings, p string) bool {
	if p == "" {
		return false
	}
	root := RootFor(s, p)
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	first := strings.ToLower(strings.SplitN(filepath.ToSlash(rel), "/", 2)[0])
	names := []string{s.AfterUpload.Folder, s.AfterUpload.FolderOdd, s.AfterUpload.FolderEven, s.AfterUpload.FolderVideo}
	for _, n := range names {
		if n == "" {
			continue
		}
		if strings.ToLower(cleanFolderName(n, "")) == first {
			return true
		}
	}
	return false
}
