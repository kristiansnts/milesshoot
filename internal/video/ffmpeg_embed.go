//go:build windows && ffmpegembed

package video

import _ "embed"

// ffmpeg.exe ditanam saat build rilis Windows (CI mengunduhnya ke ffmpeg_bin/, lihat release.yml)
// supaya laptop client tidak perlu memasang ffmpeg. Tag build `ffmpegembed` hanya dipasang di CI.
//
//go:embed ffmpeg_bin/ffmpeg.exe
var embeddedFfmpeg []byte
