# fotonaik — Rencana Kerja (PR)

Clone desktop MAS EDY (auto-upload FotoYu) · Go + Wails + Svelte · 1 lisensi = 1 akun.
Sumber asli untuk referensi port: `~/Library/Application Support/MAS-EDY/`.

---

## ✅ Sudah jadi (teruji)
- **Core engine** (`internal/core/`): config, store (MD5 dedup), scanner (polling), processor (resize + LUT), uploader (local + fotoyu adapter), queue (paralel + retry/backoff), engine. Emitter interface → core tak impor Wails.
- **Video** (`internal/video/`): ffmpeg → 17 frame @1440 → zip store (persis web FotoYu).
- **FotoYu client** (`internal/fotoyu/`): login + refresh single-flight, upload foto/video 3-langkah, SearchTrees, Fee, Storage, TreeLeaves, ListResend/SendResend.
- **Bridge Wails** (`app.go`) + **server HP** (`internal/server/`, SSE + /api, bind 0.0.0.0:4577) + wiring (`main.go`).
- **Frontend Svelte minimal**: Unggah (live), Pengaturan (folder/tujuan/login/cari FotoTree/performa), Lisensi (desain), Riwayat, Placeholder.
- **Build universal macOS** (arm64 + x86_64) jalan. `wails build -platform darwin/universal`.

### Catatan teknis (jangan lupa)
- Data dir: `~/Library/Application Support/fotonaik/`. Server HP: `:4577`.
- Svelte 5 → pakai `mount()` di `main.ts`, BUKAN `new App()` (ini bikin layar putih).
- Layar tanpa rail kanan pakai class `.single`, bukan `.layout` (`.layout` nyisihin 326px rail).
- `core.VideoPrepareFn = video.Prepare` diwire di `main.go` (hindari import cycle).
- Password FTP disimpan di file `data/ftp-pass` (0600), TIDAK ikut ke JSON config (pelajaran dari kebocoran app asli).

---

## 📋 Dikerjakan besok

### ✅ GRUP 1 — UI config lengkap · SELESAI 2026-11-17
Layar **Unggah** sekarang punya: panel *Sumber foto*, *Tujuan unggah*, dan rail *Opsi unggah*.
Diuji end-to-end lewat `POST/GET /api/config` (round-trip benar, normalisasi jalan, `ftp.pass` tetap tak bocor).

- [x] **Harga foto & video** + rincian *Pembeli bayar / Kamu terima* (via `app.Fee`, muncul bila sudah login + FotoTree dipilih).
- [x] **Event name**, **Tanggal/Waktu** (kosong = ikut jam kamera).
- [x] **Deskripsi** + **Tag nickname** (chip tambah/hapus; `@` dibuang & kosong difilter di `normalize()`).
- [x] **Unggah video** toggle (`Video.Enabled`).
- [x] **Unggah otomatis** switch (tersambung ke pause/resume antrean).
- [x] **Urutan unggah** (terbaru/terlama).
- [x] **Nomor file ganjil/genap** + switch *Hanya yang dipilih*.
- [x] **Kompresi** 1440/4096.
- [x] **Preset** — field `Presets []Preset` DITAMBAHKAN ke `core.Settings` + normalize (maks 30, nama kosong difilter). Simpan/pakai/hapus dari UI.
- [x] **Sumber foto**: folder dipantau + ganti, *Ikut subfolder*, baris Kamera Wi-Fi (disabled, nunggu GRUP 2).
- [x] Tombol *Pilih/Ganti* FotoTree → lompat ke Pengaturan (event `goto` dari Unggah ke App).
- [x] **Warna (LUT) picker** — SELESAI 2026-11-18, bersamaan dengan import LUT (lihat GRUP 2).

**Catatan implementasi:** patch config pakai JSON parsial bersarang (`{"event":{"price":123}}`) — `json.Unmarshal` ke struct yang sudah terisi hanya menimpa field yang ada, jadi aman. Slice (nicknames/presets) diganti utuh — memang disengaja.
CSS baru: `.sw` (switch), `.optrow`/`.optseg`, `.srcrow`, `.chip2`, `.feebox`, `.ta`, `.fgrid`, `.sect`.

### GRUP 2 — Backend baru
- [x] **Server FTP (kamera Wi-Fi)** — SELESAI 2026-11-17. `internal/ftp/` (package mandiri, tidak impor `core` → di-wire di `main.go` seperti video).
  - USER/PASS, PASV/EPSV + PORT, STOR/APPE, LIST/NLST/MLSD, CWD/MKD/PWD, SIZE/MDTM, RNFR/RNTO. DELE/RMD sengaja ditolak.
  - Tulis ke `.nama.part` lalu rename → scanner tak pernah ambil file setengah jadi. Nama kembar → `A_1.JPG` (foto lama tak tertimpa). Idle 90 dtk → batal (kamera keluar jangkauan).
  - Port 21 → mundur otomatis ke 2121/2221 bila dipakai.
  - **Password tidak pernah lewat /api jaringan** — hanya via perintah desktop `app.FTPInfo()`. UI baris "Kamera Wi-Fi" di Unggah menampilkan alamat/port/user + password (tombol lihat/sembunyikan) + switch on/off.
  - 4 tes lulus: STOR utuh + callback + statistik, anti-timpa, auth wajib, **anti path-traversal**.
  - Diuji end-to-end dengan `ftplib`: kirim JPEG 1800×1200 → mendarat di folder → scanner → dedup → resize **1440×960** → upload → pindah ke "Sudah upload". ✅
- [x] **Perbaikan terkait**: config yang diubah dari HP sekarang ikut memicu efek samping (`server.AfterConfig` → `app.applySideEffects`: rescan, parity, restart FTP). Sebelumnya hanya jalan dari jendela desktop.
- [x] **Watermark** — SELESAI 2026-11-18. `internal/core/processor.go`, dipanggil dari `Prepare()` setelah resize+LUT supaya posisi/ukuran konsisten dengan hasil akhir.
  - **Teks**: digambar dulu di kanvas native pakai `golang.org/x/image/font/basicfont` (sudah ada di go.mod lewat LUT, tidak ada dependensi baru), lalu diskalakan (`xdraw.CatmullRom`) ke lebar target (`Size` = persen lebar foto) — supaya tajam di segala ukuran, bukan bitmap font kecil dipaksa besar.
  - **Logo**: baca file gambar dari `Watermark.LogoPath` (field baru di `config.go`, lewat `app.WatermarkPickLogo()` yang pakai `wrt.OpenFileDialog`), diskalakan sama seperti teks.
  - Opacity diterapkan lewat `draw.DrawMask` dengan `image.Uniform` alpha seragam — jadi tidak perlu mengubah alpha piksel tile satu per satu.
  - Posisi `br/bl/tr/tl/c` dengan margin otomatis (2% lebar foto, minimal 10px).
  - **Pengaman di `normalize()`**: watermark yang di-"nyalakan" tapi tanpa isi (teks kosong untuk mode text, atau belum pilih logo untuk mode logo) otomatis jatuh ke `Enabled=false` — supaya switch yang menyala di UI tidak diam-diam tidak berefek apa pun. Size di-clamp 5-60%, Opacity 5-100%.
  - UI: panel baru di Pengaturan → Opsi → Watermark (toggle, jenis teks/logo, posisi 5 pilihan, slider ukuran & transparansi, tombol "Pilih gambar" untuk logo).
  - 6 tes unit (posisi tidak menggambar di luar area, opacity meredupkan piksel, teks kosong = noop, file logo hilang = noop aman, logo sungguhan tergambar di posisi benar, normalize menonaktifkan watermark kosong + clamp nilai aneh).
  - Catatan kecil sambil di sini: perbaiki komentar `HiresSetAuto` yang masih bilang "tiap 3 menit" padahal sudah diubah ke 5 menit sebelumnya — cuma komentar, tidak ada perubahan perilaku.
- [x] **Import pustaka LUT** — SELESAI 2026-11-18. `internal/core/lut.go` (fungsi pustaka baru: `LUTDir`, `ListLUTs`, `ImportLUT`, `DeleteLUT`) + `app.go` (`ListLUTs()`, `ImportLUT()` pakai `wrt.OpenFileDialog`, `DeleteLUT(name)`, `SetLUT(role, path)`).
  - **Validasi sungguhan, bukan cuma cek ekstensi**: `ImportLUT` memanggil `parseCube` langsung ke file sumber sebelum menyalin — file `.cube` yang isinya rusak/bukan LUT ditolak dengan pesan, tidak ikut masuk pustaka.
  - Nama bentrok otomatis dapat sufiks (`Warna.cube`, `Warna 2.cube`, dst) — tidak pernah menimpa LUT lama tanpa sepengetahuan user.
  - `DeleteLUT` membatasi `name` ke basename saja (`filepath.Base` + tolak `..`) — **anti path-traversal**, supaya tak bisa dipakai menghapus file sembarang di luar `data/luts/`. Dites eksplisit (`../config.json`, `/etc/passwd`, dll).
  - Menghapus LUT yang lagi dipakai (`cfg.LUT.Photo`/`Video`) otomatis mengosongkan field itu di `app.DeleteLUT` — supaya setelan tidak diam-diam menunjuk ke file yang sudah hilang.
  - `SetLUT("photo"|"video", path)` dipisah dari `ImportLUT()` — import cuma menambah ke pustaka, pemilihan "LUT mana dipakai untuk foto/video" adalah langkah terpisah, sesuai pola `HiresAddFolder`/`AddFolder` yang sudah ada.
  - **UI (sekalian mengerjakan "Warna (LUT) picker" dari GRUP 1)**: panel baru "Warna (LUT)" di Pengaturan → Opsi — daftar pustaka (chip + hapus), tombol "Impor .cube", dropdown pilih LUT untuk foto & video terpisah, slider kekuatan (0-100%, field `lut.strength` yang sudah ada sebelumnya tapi belum ada UI-nya).
  - 5 tes unit: impor+list, nama bentrok disufiks bukan ditimpa, file tak valid ditolak, hapus LUT, dan anti path-traversal saat hapus.
  - Reuse: `fileExists`/`copyFile` sudah ada di `queue.go` dari fitur lain — tidak didefinisikan ulang.
- [x] **Progres kecepatan** — SELESAI 2026-11-18. **Catatan scope**: user mengoreksi — bukan grafik riwayat 60 menit, cuma angka hidup gaya MAS EDY (banner + 7 ring: terunggah/antre/gagal/foto-menit/Mbps/sisa-waktu/lama-upload). Grafik historis TIDAK dikerjakan (di luar scope yang diminta).
  - **Backend** (`internal/core/queue.go`): `UploadQueue.Speed(pending int) Speed` — port ringkas dari `speed()` di `queue.js`. `Speed{Mbps, PerMin, EtaSec, ElapsedSec}` ditambahkan ke `core.Status.Speed`, dihitung tiap kali `Engine.Status()` dipanggil (sudah di-broadcast tiap 1.5 detik lewat ticker yang ada di `app.go`, jadi tidak perlu loop baru).
    - Mbps: jumlah byte hasil olah yang terkirim dalam jendela 30 detik terakhir (rentang minimal 5 detik supaya tak melonjak di awal sesi) — sampel disimpan di `sentWindow []sentSample`, dibuang otomatis setelah 120 detik.
    - EtaSec: `-1` kalau belum bisa diperkirakan (butuh ≥3 unggahan selesai dalam 120 detik terakhir) — supaya UI menampilkan "—" bukan angka ngawur dari satu sampel.
    - ElapsedSec ("lama upload"): mulai berjalan otomatis begitu ada file pending/uploading, balik ke 0 begitu antrean kosong — tidak perlu tombol "mulai sesi" manual.
    - **Jebakan mutex yang dihindari**: `Speed()` menghitung ulang "foto/menit" secara inline alih-alih memanggil `SpeedPerMinute()` yang sudah ada — `SpeedPerMinute()` mengunci `statsMu` sendiri, dan `Speed()` sudah memegang kunci itu → kalau dipanggil, persis pola self-deadlock yang pernah bikin Ranking macet total (lihat catatan GRUP 5). Jadi sengaja diduplikasi kecil, bukan dipanggil.
  - **UI** (`Unggah.svelte`): panel 3-kartu lama (Terunggah/Antre/Gagal) **diganti**, bukan ditambah di sebelahnya — biar tidak ada dua tempat menampilkan angka yang sama. Banner status (ikon + judul + subjudul, warna ikut status: hijau mengunggah / kuning dijeda-atau-kosong) + grid 7 ring kecil, warna per-metrik dari token CSS yang sudah ada (`--good/--warn/--bad/--accent/--ci-but/--ci-lil/--muted`). Isi ring (selain "terunggah") relatif ke nilai tertinggi yang pernah terlihat sesi ini ("peak", pola yang sama dipakai MAS EDY) — supaya ring tetap bermakna walau tak ada batas atas tetap.
  - **Diverifikasi visual**: `vite dev` + Chrome headless lewat CDP (`capture_web_screenshot`) — tampilan kosong/idle cocok dengan referensi screenshot MAS EDY yang diberikan user (banner "Tidak ada yang diunggah" + 7 ring 0/`—`/`0:00`).
  - 4 tes unit baru (`speed_test.go`): idle semua nol, elapsed mengikuti sesi aktif (mulai & berhenti otomatis), Mbps cuma dari sampel 30 detik terakhir (sampel basi diabaikan), ETA butuh ≥3 sampel.
- [x] **EXIF tanggal asli** — SELESAI 2026-11-17. `internal/core/exif.go`, pembaca EXIF minimal tanpa dependensi.
  - Kenapa ini dikerjakan duluan: ini **perbaikan kebenaran**, bukan fitur. mtime berubah saat foto disalin dari kartu / dikirim lewat FTP, jadi (1) tanggal yang dikirim ke FotoYu bisa salah dan (2) saringan tanggal menyaring pakai waktu transfer, bukan jam jepret.
  - Urutan tag: `DateTimeOriginal` (0x9003) → `DateTimeDigitized` (0x9004) → `DateTime` (0x0132). Dukung susunan byte II & MM.
  - `FileDate(path)` = EXIF bila ada, kalau tidak mtime. Dipakai di `processor.Prepare` (OriginalAt→FotoYu), `queue.AddFile` (saringan), dan `PreviewFilters`.
  - 4 tes unit (II/MM, Digitized, fallback mtime, **EXIF menang atas mtime**, saringan "hari ini" menolak file yang dijepret bulan lalu tapi disalin hari ini).
  - **Divalidasi ke 400 foto asli di Mac ini**: 62 punya tanggal EXIF → semua terbaca; 338 tanpa tag tanggal → fallback mtime. Dicocokkan ke `mdls` macOS: selisih tepat 7 jam (WIB vs UTC) di semua sampel → nilai benar.
  - Temuan: banyak JPEG (hasil edit/kiriman chat) punya segmen Exif tapi **tanpa** tag tanggal sama sekali — fallback mtime wajib ada, bukan opsional.

### ✅ GRUP 1b — Pembeda dari MAS EDY: pisah operasi vs konfigurasi · SELESAI 2026-11-17
MAS EDY menjejalkan semua setelan di layar Unggah. fotonaik memisahkannya — ini pembeda struktural utama.
- [x] **Unggah = layar operasi.** Hanya: 3 kartu status, panel *Sesi ini* (ringkasan **baca-saja**: folder, tujuan+FotoTree, harga, saringan aktif, kamera Wi-Fi — tiap baris ada tombol **Ubah** yang lompat ke tab Pengaturan yang tepat), aktivitas, cincin progres, switch unggah otomatis, tombol Jeda, dan tombol "Coba lagi N yang gagal" (muncul hanya bila ada yang gagal).
- [x] **Pengaturan = semua setelan**, dengan sub-tab: **Sumber** (folder, subfolder, pindah-setelah-unggah, Kamera Wi-Fi/FTP lengkap) · **Tujuan unggah** (lokal/FotoYu, preset, cari FotoTree, nama event, tanggal/waktu, harga+fee, deskripsi, nickname) · **Saring file** · **Opsi** (otomatis, urutan, video, kompresi, performa, tema) · **Akun**.
- [x] Navigasi: `Unggah` kirim event `goto {screen, tab}` → App set layar + tab Pengaturan.
- [x] Saringan aktif tampil sebagai chip di Unggah — menjawab "kenapa foto saya tidak naik?" tanpa buka Pengaturan.

### ✅ GRUP 1c — Saring berdasarkan TANGGAL (fitur baru, tidak ada di MAS EDY) · SELESAI 2026-11-17
- [x] `DateFilter{Mode,From,To}` di `core.Settings` — mode **all / today / since / range**.
- [x] Normalisasi defensif: mode yang tanggalnya tak sah otomatis jatuh ke `all` (tidak diam-diam menyaring semua).
- [x] `DateAllowed()` di `paths.go`; dicek di `queue.AddFile` dengan set `skippedDate` + `DateChanged()` untuk pindai ulang saat saringan diubah.
- [x] UI di Pengaturan → Saring file, bersama ganjil/genap.
- [x] 3 tes lulus termasuk uji pipeline: foto bulan lalu dilewati saat mode "hari ini", lalu ikut terpindai setelah saringan dimatikan.
- Catatan: tanggal diambil dari **mtime file**. Setelah EXIF dikerjakan (GRUP 2), ganti ke `DateTimeOriginal` supaya lebih akurat.

### ✅ GRUP 1d — Saring FORMAT & NAMA (glob/regex) · SELESAI 2026-11-17
Semua saringan kini berkumpul di Pengaturan → **Saring file**: nomor (ganjil/genap) · tanggal · format · nama.
- [x] `FormatFilter{Mode,Exts}` — `all` / `only` dengan daftar ekstensi (chip JPG/PNG/HEIC/WEBP/TIF/MP4/MOV). JPG otomatis mencakup JPEG. Daftar kosong → jatuh ke `all`.
- [x] `NameFilter{Mode,Pattern,Invert}` — mode **contains / glob / regex**, pencocokan abaikan besar-kecil huruf, plus **Kebalikan** (yang cocok justru dilewati).
  - glob: `upload-*`, `DSC_*.jpg` · regex: `^upload-.+\.jpg$`, `_\d{4}\.`
  - Regex pakai RE2 (Go) → tidak bisa catastrophic backtracking. Hasil kompilasi di-cache (maks 64 pola).
- [x] **Pratinjau langsung** (`app.FilterPreview(patch)` → `core.PreviewFilters`): menguji saringan ke isi folder sumber **tanpa menyimpan**, menampilkan "N dari M file cocok" + contoh nama + pesan error pola. Ini yang bikin regex aman dipakai orang awam.
- [x] Pengaman: pola rusak/kosong & daftar format kosong → jatuh ke `all`, jangan diam-diam mengosongkan antrean. Saat pola rusak, `NameAllowed` mengembalikan true (tidak menyaring apa pun).
- [x] `FiltersChanged()` menggantikan ParityChanged/DateChanged di `applySideEffects` — semua set "dilewati" dibersihkan sekaligus saat setelan berubah.
- [x] 5 tes unit + uji end-to-end dengan app sungguhan: glob `upload-*` → tepat 2 file; regex `_\d{4}\.` → hanya DSC; **invert** → persis komplemennya.
- Catatan: saat menguji, file hasil `ffmpeg testsrc` dengan parameter sama punya MD5 identik → kena anti-duplikat. Pakai ukuran berbeda tiap file saat bikin data uji.

### ✅ GRUP 2b — Log & mode debug · SELESAI 2026-11-17
Ditambahkan untuk menguji sambungan kamera.
- [x] **`core.FileLogger`** — Emitter tambahan; semua catatan masuk berkas harian `<dataDir>/log/fotonaik-YYYY-MM-DD.log` (simpan 14 hari) + ring 2000 baris di memori untuk layar Log.
- [x] **`Settings.Debug`** → `ftp.Settings.Debug`: mencatat tiap perintah & balasan (`-> USER`, `<- 331 …`). **Password disamarkan** (`PASS ******`).
- [x] **Layar Log** di sidebar: saringan level, pencarian teks, salin ke clipboard, buka folder log, toggle mode debug, dan petunjuk singkat cara menguji kamera.
- [x] Perintah: `LogTail(n)`, `LogFile()`, `OpenLogFolder()`.
- [x] **`ftp.selfCheck()`** — PENTING: di macOS dua proses bisa sama-sama "berhasil" mengikat port 21, tapi hanya satu yang menerima koneksi. Tanpa ini fotonaik melaporkan "menyala di port 21" padahal kamera tersambung ke aplikasi lain dan log kita bersih — jebakan yang sangat membingungkan. Sekarang server menyambung ke dirinya sendiri, membaca banner, dan bila bukan milik kita → log ERROR yang menyebut nama aplikasi perebutnya. Probe ini dibuat senyap (tidak tercatat sebagai "kamera").
- Terbukti: log menangkap percakapan penuh USER→PASS→TYPE→PASV→STOR→QUIT lalu berlanjut "Masuk antrean → Terunggah".

### ✅ GRUP 2c — Kamera kabel (PTP/tether) macOS · SELESAI 2026-11-17
- [x] **Helper Swift** `mac/fotonaik-kamera.swift` — ImageCaptureCore (API resmi Apple, sama yang dipakai Image Capture). Build universal: `swiftc -swift-version 5 -O -framework ImageCaptureCore -target arm64-apple-macos11|x86_64-apple-macos11` lalu `lipo -create` → `build/helpers/fotonaik-kamera` (~240 KB).
  - Keluaran: satu JSON per baris ke stdout — `ready` / `connected{model}` / `catalog{ignored}` / `file{path}` / `disconnected` / `error{msg}`.
  - **Hanya jepretan BARU** yang diunduh: isi kartu saat tersambung dijadikan patokan (`baseline`) dan diabaikan.
  - Catatan API: `deviceBrowser(_:didRemove:moreGoing:)` (bukan `moreComing`), dan `ICCameraDeviceDelegate` butuh banyak stub wajib + 3 metode `ICDeviceDelegate`.
- [x] **`internal/usbcam`** (package mandiri, tanpa impor core — di-wire di main seperti ftp/video): spawn helper, baca baris, nyalakan ulang bila mati (5 dtk), callback `OnFile`/`OnConnect`/`OnChange`.
- [x] `Settings.UsbCam{Enabled,AutoSource}`. Saat kamera tersambung & AutoSource → folder sumber otomatis dialihkan ke `<dataDir>/kamera-usb` (dicatat jelas di log supaya tidak misterius).
- [x] UI: panel "Kamera kabel (USB / tether)" di Pengaturan → Sumber + baris ringkas di Unggah. Perintah `UsbCamState`/`UsbCamToggle`.
- [x] **Diuji dengan kamera sungguhan**: Nikon **Z6 III** terdeteksi lewat helper yang dibundel di `.app`, folder sumber otomatis beralih. Unduh jepretan baru menunggu uji jepret oleh user.
- ⚠️ **Langkah build manual**: helper harus disalin ke `fotonaik.app/Contents/MacOS/` lalu `codesign --force --deep --sign -` SETELAH `wails build`. Perlu dijadikan postBuildHook di `wails.json`.
- ❌ **Windows belum** — butuh PowerShell + WPD, dan tidak bisa diuji dari Mac. Jangan diklaim selesai sampai dicoba di mesin Windows.

### ✅ GRUP 2d — Kartu memori / card reader · SELESAI 2026-11-17
- [x] `usbcam.DriveWatcher` — pantau drive ber-folder `DCIM` tiap 3 dtk (`/Volumes/*` di macOS, huruf drive D:–Z: di Windows). Nama DCIM dicocokkan abaikan besar-kecil huruf. Hitung foto dibatasi 20.000 supaya kartu besar tak bikin lambat.
- [x] Perintah `CardDrives()` + `UseCardDrive(path)`; UI daftar kartu di Pengaturan → Sumber (label, jumlah foto, tombol Pakai).
- [x] **Dua pengaman disengaja** saat kartu dipakai jadi sumber:
  1. Antrean **DIJEDA** — kartu bisa berisi ribuan foto lama; user periksa saringan dulu baru tekan Lanjutkan.
  2. `afterUpload.move` **dimatikan** — jangan pernah menulis/memindahkan apa pun di kartu milik orang.
- [x] 3 tes unit (findDCIM huruf campur, hitung foto abaikan RAW/tersembunyi/non-foto, isPhotoName).
- [x] **BUG DIPERBAIKI (dilaporkan user saat tes):** card reader ikut terdeteksi jalur tether sehingga UI terus menulis "kamera kabel tersambung" padahal kamera sudah dicabut. Sebabnya ImageCaptureCore menganggap mass-storage sebagai `ICCameraDevice` juga. Helper sekarang melewati perangkat ber-`transportType == ICDeviceTransport.transportTypeMassStorage` (emit `skip`), jadi satu perangkat hanya diurus satu jalur.
  - Catatan API: konstantanya `ICDeviceTransport.transportTypeMassStorage.rawValue` (dibandingkan ke `String?`), bukan `ICTransportTypeMassStorage`.

### GRUP 3 — UI murni (derivasi dari status)
- [ ] **Checklist "Siap berangkat"** (ref asli: kartu 1/6) — akun FotoYu, event/FotoTree, harga, folder, kamera, uji internet (speedtest). Butuh command `app.SpeedTest()` (port `speedTest()` dari server.js) untuk uji internet.
- [ ] **Mode Lapangan** (fullscreen angka besar) — sudah ada di mockup artifact, port ke Svelte sebagai overlay.
- [ ] **Thumbnail foto terbaru** — serve via `/thumb/<md5>` (processor sudah simpan thumbnail) + command/endpoint.

### GRUP 4 — Lisensi (backend) · model 1 lisensi = 1 akun self-serve
Ref: `src/license.js` (tapi SEDERHANAKAN — buang model owner-bagi-ke-tim).
- [ ] Trial 500 foto (hitung dari `store` done count), status aktif/trial.
- [ ] Kode perangkat (device id) + aktivasi kode lisensi (Ed25519 verify seperti asli).
- [ ] Alur self-serve: pilih paket → bayar (QRIS/transfer) → lisensi aktif untuk 1 akun. TANPA panel kelola-akun di app user.
- [ ] Panel admin/penjual = TERPISAH (web/dashboard sendiri), bukan di app fotografer.
- [ ] Sambungkan layar `Lisensi.svelte` (sekarang desain statis) ke backend.

### GRUP 5 — Layar yang masih placeholder
- [x] **Hi-res** — SELESAI 2026-11-17. `internal/core/hires.go` (`HiresManager`; nama `Hires` sudah dipakai struct config).
  - **Kenapa didahulukan:** ini satu-satunya fitur sisa yang langsung berhubungan dengan uang masuk — selama file asli belum dikirim, pesanan yang sudah terjual menggantung.
  - **Pencocokan jadi murah** karena `signature` FotoYu = MD5 file asli = kunci store kita → `store.Get(sig)` langsung dapat path (kasus 90%). Cadangan: indeks nama di folder sumber + folder tambahan, diverifikasi MD5. Sisanya "cocok nama" (isi beda, mis. foto sudah diedit) → perlu dicek user.
  - Folder **"Sudah upload" ikut dipindai** (file yang sudah diunggah justru dipindah ke sana) — ini wajib, mudah terlewat.
  - Cache: md5 per (path|size|mtime), `verified` & `mismatch` per signature supaya tidak menghitung ulang.
  - Perintah: `HiresState/Refresh/Send/SendAll/AddFolder/RemoveFolder`. Event `hires` → badge merah di sidebar.
  - `VideoProbeFn` disuntik di main (ffmpeg) untuk durasi/dimensi video — pola yang sama dengan `VideoPrepareFn`.
  - 6 tes unit (cocok dari store, cocok nama+MD5, cocok-nama-saja, tidak ketemu, folder tambahan, hitungan status) + **cek live read-only** ke akun asli: endpoint `/gs/v5/creations?filter=resend` valid, 0 pesanan.
  - Belum: pratinjau berdampingan untuk "cocok nama".
  - **LUT hi-res + perbesar foto kecil — SELESAI 2026-11-23.** Sebelumnya `Hires.LUT/LUTStrength/AutoTone` ada di config tapi tak dipakai kode mana pun (file asli selalu dikirim apa adanya). Sekarang `internal/core/hires_process.go` (`prepareHiresPhoto`, dipanggil dari `sendOne`):
    - LUT hi-res (`SetLUT("hires", path)` + slider kekuatan di layar Hi-res) → render resolusi penuh, JPEG q95.
    - Foto < 1 MB diperbesar ke ±2 MB (port `Q()` di hires.js, maks sisi 6000px). File di disk tak diubah; hasil di file sementara yang dihapus setelah kirim.
    - Tanpa LUT dan ≥ 1 MB → file asli dikirim apa adanya (jalur lama). LUT hilang/rusak → ERROR, bukan diam-diam kirim tanpa LUT.
    - Orientasi EXIF "dipanggang" ke piksel (re-encode membuang EXIF). 8 orientasi dites.
    - Beda dari MAS EDY: encoder JPEG Go selalu 4:2:0 (sharp memakai 4:4:4), jadi ukuran hasil bisa sedikit berbeda untuk kualitas sama.
    - Belum: Auto tone & editor hi-res (`Hires.AutoTone` masih tak berefek — jangan dimunculkan di UI).
- ✅ **BUG DIPERBAIKI 2026-11-23: `Processor.Prepare` (jalur unggah biasa) membuang orientasi EXIF.** Diuji: JPEG 3000×2000 ber-tag orientasi 6 → hasil 1440×960 tanpa tag → foto portrait terunggah terbaring. Perbaikan: `Prepare` memanggil `exifOrientation` + `orientRGBA` sebelum resize. Tes: `TestPrepareKeepsPortraitOrientation` (tag 6 → 960×1440; tanpa tag → 1440×960).
- [x] **Kirim otomatis** — SELESAI 2026-11-18. Port dari `hiresTick()` di `server.js`: tiap 5 menit (diminta user — aslinya 3 menit, diperlambat biar tidak terlalu sering membebani server FotoYu), `Refresh()` lalu — bila `Hires.Auto` nyala — `SendAllExact()`. **Hanya "cocok pasti" yang pernah dikirim otomatis**; "cocok nama" (isi beda, foto sudah diedit) selalu menunggu dicek manual, tidak terkecuali.
  - `HiresManager.StartAutoLoop()/StopAutoLoop()`, dipanggil dari `Engine.Start()/Stop()` (bukan `app.go`, supaya siklus hidupnya ikut Engine seperti Queue/Scanner).
  - `hiresAutoInterval` sengaja `var` bukan `const` supaya test bisa mempersingkatnya tanpa menunggu 5 menit sungguhan.
  - UI: toggle "Kirim otomatis" di layar Hi-res, perintah `HiresSetAuto(bool)`.
  - 2 tes: loop benar-benar berhenti setelah `Stop()` (tidak diam-diam tetap jalan), `tick()` tidak panik pada toggle true/false.
- [x] **Ranking** — SELESAI 2026-11-18. `internal/core/ranking.go` (`RankingManager`), port dari `src/ranking.js`.
  - Job background per (treeID, range): tarik daun berhalaman (`TreeLeaves`, 20/halaman — sama dengan web FotoYu), agregasi per akun (count, videos, last), berhenti begitu daun lebih tua dari rentang (FotoYu mengirim terbaru dulu → tidak perlu menyisir seluruh FotoTree yang bisa berisi puluhan ribu daun).
  - Rentang: `today`/`d7`/`d30`/`all`. Hasil di-cache ke disk (`<dataDir>/ranking/<key>.json`) supaya buka lagi tak perlu memindai ulang; job yang sedang jalan bisa dibatalkan (`Cancel`).
  - **Tambahan ke `fotoyu` client**: `TreeDetail(treeID)` (total daun + nama) dan `Leaf` diubah dari `json.RawMessage` jadi struct terstruktur (`CreatedAt`, `ContentType`, `Member{ID,Username,Photo,CreatorStatus,IsOfficial}`) — sebelumnya cuma raw JSON, tak ada yang memakainya.
  - UI: layar Ranking dengan 3 kartu (total daun, dipindai, akun muncul), tabel peringkat dengan medali 🥇🥈🥉, baris akun sendiri ditandai & disorot, tombol Batalkan saat masih memindai.
  - Perintah: `RankingStart/Status/Cancel` (pola sama dengan Hi-res: desktop-only, tidak lewat `/api/*` jaringan).
  - 5 tes unit (rentang waktu, pembersihan key dari karakter path-traversal, pengurutan+tie-break nama, parse waktu FotoYu, simulasi agregasi+stop-condition) + **cek live read-only** ke akun asli: `TreeDetail` & `TreeLeaves` dua-duanya valid, data nyata ter-parse benar (nama FotoTree, 166 daun, member username/id cocok).
  - Catatan: ini GRUP 5 terakhir — Hi-res dan Ranking sudah lengkap; Riwayat sudah ada sejak awal (lihat catatan di bawah).
- [x] **Perbaikan setelah feedback user (2026-11-18):** dua masalah pada percobaan pertama.
  1. **Reset saat pindah tab** — Svelte menghancurkan komponen Ranking tiap pindah layar, state lokal (tree, rentang) hilang. Perbaikan: `rankingTree`/`rankingRange` dipindah ke store bersama (`lib/stores.ts`) yang bertahan lintas navigasi. Dipisah jadi `load()` (tampilkan job yang masih jalan di backend / cache — dipanggil saat remount) vs `start()` (mulai pindaian baru — HANYA dipanggil dari aksi eksplisit: pilih tree, ganti rentang, tombol "Pindai ulang"). Tanpa pemisahan ini, remount akan salah memicu pindaian ulang dari nol walau hasilnya sudah ada.
  2. **Ranking harus bisa pilih event sendiri** — sebelumnya ikut FotoTree tujuan unggah di Pengaturan. Sekarang Ranking punya kotak pencarian FotoTree sendiri (persis pola Pengaturan → Tujuan unggah), independen dari FotoTree aktif untuk unggah. Default pertama kali ikut FotoTree unggah (kalau ada), tapi begitu user memilih sendiri di Ranking, pilihan itu yang dipakai — tidak ketiban default lagi.
  - Catatan timing: `onMount` meneruskan tree secara eksplisit ke `load(initial)` alih-alih membaca variabel reaktif `tree`, supaya tidak tergantung urutan microtask Svelte saat `rankingTree.set()` dan pembacaan `$rankingTree` terjadi di baris yang sama.
  - **Keterbatasan pengujian**: alur data Ranking (`RankingStart/Status`) sengaja desktop-only (pola sama dengan Hi-res/kamera kabel) — tidak lewat `/api/*` jaringan. Jadi tak bisa diuji otomatis lewat browser biasa (selalu resolve `null` dalam mode HP). Diverifikasi sebagian via CDP terhadap app sungguhan (default FotoTree terisi dari config, highlight rentang waktu benar, kotak pencarian tampil saat "Ganti" diklik) — alur pencarian+pilih+data asli perlu dicoba langsung di jendela desktop.
- [x] **Progress bar saat memindai** — ditambahkan 2026-11-18 setelah diminta user. Rentang "Semua": bar pasti (`scanned/total`, totalnya diketahui dari `TreeDetail`). Rentang waktu (hari ini/7/30): bar tak pasti/animasi geser — karena pemindaian berhenti sebelum mencapai total daun, persentase jujur tidak bisa dihitung. CSS `.bar.indet` baru di `style.css`, menghormati `prefers-reduced-motion`.
- [x] **BUG KRITIS DIPERBAIKI 2026-11-18 (dilaporkan user): Ranking macet "Memuat..." selamanya + tombol "Pindai ulang" tidak berefek.**
  - **Akar masalah: self-deadlock**, bukan race condition akibat klik cepat seperti dugaan awal. Di akhir `RankingManager.run()`: kode mengunci `j.mu`, lalu memanggil `j.view()` — padahal `view()` **juga** mengunci `j.mu` di dalamnya. `sync.Mutex` Go tidak reentrant → goroutine menunggu kunci yang dipegangnya sendiri, selamanya.
  - **Ini terjadi di SETIAP scan yang selesai**, bukan cuma saat rapid-switch — user kebetulan menemukannya duluan karena rentang 7/30 hari selesai lebih cepat (daun lama membuat pemindaian berhenti dini) sehingga lebih cepat mencapai baris finalisasi yang buggy. Tidak pernah ketahuan sebelumnya karena test yang ada cuma menyimulasikan logika agregasi secara terisolasi (`TestAggregationLogic`), tidak pernah benar-benar memanggil `Start()`→`run()` end-to-end sampai selesai.
  - **Reproduksi terkendali** (bukan cuma menebak dari screenshot): test langsung ke `RankingManager` dengan FotoTree asli user ("ALWI LOOP"), klik 7→30→hari-ini dengan jeda 300ms meniru klik manusia. Sebelum fix: `goroutine ... [sync.Mutex.Lock]` di stack trace Go — deadlock terbukti via `go test` timeout + dump goroutine. Setelah fix: ketiga job selesai dalam ~1–2 detik.
  - **Perbaikan**: pindahkan `view := j.view()` ke SETELAH `j.mu.Unlock()`.
  - **2 test regresi permanen** ditambahkan (`ranking_deadlock_test.go`): `TestRankJobViewNotReentrant` (memanggil pola lock→unlock→view() di goroutine dengan batas waktu 3 detik — kalau regresi ke pola lama, test ini macet dan gagal via timeout, bukan false-positive pass) + `TestRunFinalizesOnError`.
  - Diverifikasi juga 3 job berbeda (today/d7/d30) dijalankan BENAR-BENAR bersamaan via goroutine terhadap akun asli — semua selesai tanpa saling mengunci (today: 0 daun, d7: 6 daun, d30: 218 daun, dari total 8066 daun sepanjang masa).
- Catatan: **Riwayat sudah jadi sejak awal** (daftar record + pill status), bukan placeholder. Yang kurang cuma ringkasan per-sesi.

### GRUP 6 — Rilis
- [ ] **Windows build** via GitHub Actions (`wails build -platform windows/amd64`) — cross-compile dari Mac ribet; paling rapi di CI Windows runner. Hasil `.exe`/installer.
- [ ] **Commit** ke git + buat repo (belum pernah di-commit).
- [ ] Ikon app (`build/appicon.png`), nama & metadata bundle.
- [ ] **[auto-update] Riset: cek-update via GitHub Releases**, BUKAN auto-replace diam-diam.
  - Alasan: app desktop (bukan web) jadi tidak ada cara "refresh browser" dapat versi baru — kalau tanpa mekanisme apa pun, user harus download ulang manual tiap rilis.
  - Rencana realistis: Go app fetch manifest JSON kecil (mis. `GET https://api.github.com/repos/<org>/fotonaik/releases/latest`) saat start + polling berkala, bandingkan `version` dengan versi terpasang (embed via `ldflags -X`), kalau beda → tampilkan banner "Update tersedia vX.Y — unduh" dengan link ke installer, BUKAN auto-download-dan-replace-diri-sendiri.
  - Auto-replace penuh (seperti Sparkle di macOS / Squirrel di Windows) butuh code-signing + notarization (Apple) dan Authenticode cert (Windows) supaya OS tidak block installer tak dikenal — ini keputusan terpisah, belum ada akun signing. Tanpa signing, auto-replace diam-diam cuma akan kena Gatekeeper/SmartScreen warning, jadi untuk sekarang cek-saja (manual klik unduh) lebih realistis.
  - Belum diimplementasi — baru catatan rencana. Keputusan pending: hosting rilis (GitHub Releases vs self-host), dan apakah invest ke code-signing sebelum rilis publik.

- [x] **[multi-folder] Kompresi multi-folder konkuren** — SELESAI 2026-11-18, persis desain yang sudah ditulis di sini sebelumnya, plus progress bar per folder (diminta tambahan oleh user).
  - `Settings.WatchFolders []string` (folder tambahan; `WatchFolder` tunggal tetap jadi folder utama/primary — config lama tak perlu migrasi, default `[]`).
  - **Helper baru di `paths.go`**: `AllWatchFolders(s)` (gabung primary+tambahan, kosong & duplikat disaring) dan `RootFor(s, path)` (folder sumber mana yang menaungi sebuah path, `""` kalau tak ada yang cocok) — jadi satu sumber kebenaran dipakai di semua tempat yang tadinya cuma baca `WatchFolder` sendirian.
  - `scanner.go` `Scan()`: ditulis ulang jadi loop per folder di `AllWatchFolders`, gabung hasilnya, plus `Scanner.PerFolder []FolderScan` (status OK/jumlah media tiap folder, untuk observability).
  - `queue.go` `inCurrent()`: keanggotaan dicek via `RootFor()` ke SEMUA folder, bukan cuma `cfg.WatchFolder` lagi.
  - `paths.go` `ArchiveDir()`/`InArchive()`: **diperbaiki jadi relatif ke folder ASAL file** (`RootFor()`), bukan selalu folder utama — ini bug nyata yang baru muncul begitu ada >1 folder sumber: tanpa perbaikan ini, file dari folder tambahan akan dicoba dipindah ke "Sudah upload" di BAWAH folder utama (salah tempat / bisa gagal rename lintas-drive). Diuji eksplisit (`TestArchiveDirPerFolder`, `TestInArchivePerFolder`).
  - `queue.go` `moveToArchive()`: path relatif dihitung dari root hasil `RootFor()`, bukan `cfg.WatchFolder` tetap.
  - `hires.go` `nameIndex()` dan `filters.go` `PreviewFilters()`: diperluas pakai `AllWatchFolders()` — pencarian file asli Hi-res dan pratinjau saringan sekarang ikut menyisir semua folder sumber, bukan cuma primary.
  - **Progress bar per folder (diminta user)**: `FolderStat{Folder,Done,Pending,Failed,Total,Pct}` + `UploadQueue.FolderProgress()` di `queue.go` — menghitung ulang dari isi store tiap dipanggil (bukan counter terpisah, supaya selalu konsisten dengan `Status.Counts` global). Ditambahkan ke `core.Status.Folders`, di-refresh otomatis lewat broadcast 1.5 detik yang sudah ada.
  - **Konkurensi**: TERBUKTI tidak perlu primitif baru seperti dirancang — `processGate`/`uploadGate` di `queue.go` memang agnostik terhadap folder asal file.
  - **App commands**: `AddWatchFolder()` (dialog folder, pola sama `HiresAddFolder`), `RemoveWatchFolder(path)`.
  - **UI Pengaturan → Sumber**: panel "Folder tambahan" (chip + hapus + tombol "Tambah folder", pola sama persis dengan panel LUT/Hires) — untuk atur folder di luar konteks pemantauan real-time.
  - **UI Unggah — revisi setelah feedback user (2026-11-18)**: progres per folder awalnya ditaruh sebagai panel inline yang cuma muncul kalau >1 folder aktif. User minta pola berbeda: **modal/dialog** yang dibuka lewat tombol "Tambah folder" di baris "Folder sumber" (panel "Sesi ini") — modal berisi daftar SEMUA folder aktif (utama ditandai pil "utama", tak bisa dihapus; tambahan punya tombol ×), masing-masing dengan bar progres (hijau = berjalan/`Memproses…`, merah = `Selesai dengan N error`, abu = `Menunggu file`) + angka "selesai/total", dan tombol "+ Tambah folder" di PALING BAWAH daftar (persis diminta: "ada button tambah folder dibawahnya"). CSS modal baru (`.modal-bg`/`.modal`/`.frow`) di `style.css`.
  - **Diverifikasi visual 3x**: screenshot headless Chrome pertama (state kosong, tanpa folder) lalu kedua dengan data tiruan (2 folder: satu 12/15 berjalan hijau, satu 5/7 selesai-dengan-error merah) — keduanya cocok dengan maksud user sebelum data tiruan dibuang lagi.
  - **Tombol "Unggah" di dalam modal (diminta user)**: selain "+ Tambah folder", modal sekarang juga punya tombol "Unggah"/"Jeda unggah" (reuse `toggle()` yang sudah ada — `api.resume()`/`api.pause()`) berdampingan di baris paling bawah — supaya mulai mengunggah bisa dilakukan TANPA menutup modal, dan progress bar per folder langsung terlihat berjalan karena modal tidak pernah di-unmount (reaktivitas Svelte tetap jalan di balik layar walau dialog terbuka). Diverifikasi visual ketiga kalinya (dua tombol berdampingan + bar tetap live).
  - **Catatan build**: fitur ini sempat tidak terlihat user karena app desktop yang sedang dibuka adalah build LAMA (dari sebelum sesi fitur ini) — `wails build` tidak otomatis jalan tiap kali kode berubah, harus di-trigger manual (plus copy `fotonaik-kamera` + `codesign` ulang, lihat catatan GRUP 2c). Diingatkan di sini supaya tidak terulang: selalu rebuild+resign setelah perubahan Go/frontend sebelum klaim "sudah bisa dicek di app".
  - 5 tes unit baru (`multifolder_test.go`): `AllWatchFolders` dedup, `RootFor` 2 folder + file di luar keduanya, `ArchiveDir`/`InArchive` per-folder, dan **satu tes pipeline penuh** (`TestMultiFolderPipeline`) yang benar-benar menjalankan Engine dengan 2 folder sumber berisi file asli, menunggu ketiganya ter-upload, lalu memverifikasi `FolderProgress()` dan bahwa arsip folder B tidak nyasar ke folder A.
  - **Diverifikasi visual**: screenshot headless Chrome (`capture_browser_scenario`, klik ke tab Pengaturan → Sumber) menunjukkan panel "Folder tambahan" tampil benar.

---

## Urutan saran
1. ✅ GRUP 1 (UI config) — paling kelihatan & cepat, bikin app langsung terasa lengkap.
2. ✅ GRUP 2 FTP — supaya kamera Wi-Fi jalan (fitur inti fotografer lapangan).
3. ✅ GRUP 5 (Hi-res, Ranking).
4. **GRUP 2 sisanya** — diminta user 2026-11-18, urutan tetap seperti ini:
   1. ✅ Watermark — selesai
   2. ✅ Import pustaka LUT — selesai (sekaligus Warna (LUT) picker di UI)
   3. ✅ Progres kecepatan — selesai (scope dikoreksi user: bukan grafik riwayat, cuma angka hidup)
   4. ~~Warna (LUT) picker di UI~~ — sudah selesai bersama #2
   5. ✅ Multi-folder kompresi konkuren — selesai, termasuk progress bar per folder
5. **GRUP 4 (Lisensi)** — dimajukan ke depan GRUP 3, diminta user 2026-11-18.
6. **GRUP 3 (UI murni)** — mundur setelah GRUP 4 (dibalik dari urutan lama).
7. GRUP 6 (rilis: Windows build, commit, ikon, auto-update).
