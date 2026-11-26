package update

// PublicKeyB64: kunci publik ed25519 (base64) pasangan dari kunci privat rilis.
// Dibuat dengan: go run ./cmd/relsign keygen ~/.milesshoot-release/update.key
// JANGAN diganti sembarangan: aplikasi yang sudah terpasang hanya menerima pembaruan yang
// ditandatangani kunci pasangan ini. Kalau diganti, pembeli lama harus memasang ulang manual.
const PublicKeyB64 = "5t2nma/B04jU/h5SgV78pfbTCrDaS+LXoOIAbR7vzSc="
