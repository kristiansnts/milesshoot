package video

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Logf menerima catatan aktivitas (level: info|warn|error|ok). Dipasang main.go ke log aplikasi;
// bawaannya tidak melakukan apa-apa supaya paket ini tetap bisa dites sendiri.
var Logf = func(level, msg string) {}

var (
	ffInfoOnce sync.Once
	ffInfoVer  string
)

// FfmpegInfo: lokasi & baris versi ffmpeg (untuk laporan masalah). Kosong bila tidak ada.
func FfmpegInfo() (path, version string) {
	path = findFfmpeg()
	if path == "" {
		return "", ""
	}
	ffInfoOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(ctx, path, "-version").Output()
		ffInfoVer = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	})
	return path, ffInfoVer
}

// lastLines: n baris terakhir yang tidak kosong dari keluaran ffmpeg, untuk catatan galat.
func lastLines(s string, n int) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			keep = append(keep, t)
		}
	}
	if len(keep) > n {
		keep = keep[len(keep)-n:]
	}
	return strings.Join(keep, " | ")
}
