# Merilis versi baru

Aplikasi memeriksa **GitHub Releases** di repo yang tertanam saat build, mengunduh zip, memverifikasi
**tanda tangan ed25519**, lalu memasangnya setelah pemakai menekan "Pasang & mulai ulang".

## Sekali saja

1. **Repo rilis harus PUBLIK.** Pembeli tidak punya akun/token, jadi repo privat tidak bisa diunduh.
   Disarankan: simpan kode di repo *privat*, dan terbitkan rilis di repo *publik terpisah* (hanya berisi
   file rilis), lalu set `RELEASE_REPO=pemilik/nama-repo-rilis`.
2. Kunci penanda tangan sudah dibuat di `~/.milesshoot-release/update.key` (kunci publiknya tertanam di
   `internal/update/pubkey.go`).
   - **Cadangkan** file itu (mis. password manager). Kalau hilang, pembeli yang sudah memasang tidak bisa
     lagi menerima pembaruan otomatis — mereka harus memasang ulang manual.
   - **Jangan** masukkan ke git atau bagikan. Siapa pun yang memegangnya bisa mengirim "pembaruan" ke semua pembeli.
3. `gh auth login` sudah beres (akun yang dipakai harus bisa menulis ke repo rilis).

## Tiap rilis

```
scripts/release.sh 1.1.0 "Perbaikan Video → Foto, log lebih lengkap"
```

Skrip: menjalankan tes → mengisi versi → `wails build` (dengan versi & repo) → zip `.app` → menandatangani →
`gh release create` dengan `milesshoot-darwin-arm64.zip` dan `milesshoot-windows-amd64.zip`, masing-masing + `.sig`. Windows di-build silang dari Mac (tanpa Windows). Catatan rilis tampil di aplikasi pembeli
sebagai "Yang baru".

Nomor versi harus `X.Y.Z` dan **lebih besar** dari rilis sebelumnya. Rilis bertanda *pre-release*/draft diabaikan.

## Cara kerja di sisi pembeli

- Aplikasi memeriksa 25 dtk setelah dibuka, lalu tiap 6 jam (bisa dimatikan di Pengaturan → Opsi).
- Bila ada versi baru, zip diunduh dan **diverifikasi** (tanda tangan mengikat nomor versi + nama file + sha256).
  Berkas yang tidak cocok ditolak dan dihapus.
- Pemasangan hanya atas klik pemakai. Foto yang sedang diunggah kembali ke antrean dan dilanjutkan.
- Aplikasi harus ada di folder yang bisa ditulis pemakai (mis. `/Applications` dengan akun admin, atau `~/Applications`).
- Hasil pemasangan dicatat di log ("[Update] hasil pemasangan sebelumnya …") dan di Laporan masalah.

## Batasan

- Dibuat: macOS Apple Silicon (arm64) dan Windows 64-bit (amd64). Mac Intel & Windows ARM belum.
- **Windows belum pernah dijalankan** — hanya dikompilasi. Coba di PC Windows sebelum dijual: buka, unggah, lalu uji pembaruan.
- Windows: tidak ada program pembantu kamera kabel (hanya macOS), dan **ffmpeg harus terpasang** di PC pembeli
  (masuk PATH) agar unggah video & Video → Foto jalan.
- Unduhan lewat Go tidak diberi atribut karantina, jadi pembaruan tidak memicu peringatan Gatekeeper.
  Pemasangan **pertama** dari zip yang diunduh lewat browser tetap kena Gatekeeper (aplikasi hanya ber-ad-hoc-sign).
