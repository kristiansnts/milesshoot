#!/usr/bin/env bash
# Membuat rilis macOS + Windows: build → zip → tanda tangan → GitHub Release.
#
#   scripts/release.sh 1.1.0 "Catatan rilis (opsional)"
#
# Syarat: gh (sudah login), wails, ~/.milesshoot-release/update.key (lihat RELEASING.md).
# Variabel opsional:
#   RELEASE_REPO=pemilik/nama   repo tempat rilis diterbitkan (bawaan: remote "origin")
#   RELEASE_KEY=/jalur/kunci    kunci privat penanda tangan
set -euo pipefail
cd "$(dirname "$0")/.."

# wails & alat Go lain dipasang di $(go env GOPATH)/bin, yang sering tidak ada di PATH
export PATH="$PATH:$(go env GOPATH 2>/dev/null)/bin"
command -v wails >/dev/null || { echo "wails tidak ditemukan — pasang: go install github.com/wailsapp/wails/v2/cmd/wails@latest"; exit 1; }

VERSION="${1:-}"; NOTES="${2:-}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "pemakaian: $0 X.Y.Z [catatan]  (mis. 1.1.0)"; exit 1; }
TAG="v$VERSION"
KEY="${RELEASE_KEY:-$HOME/.milesshoot-release/update.key}"
[ -f "$KEY" ] || { echo "kunci privat tidak ada: $KEY"; exit 1; }
command -v gh >/dev/null || { echo "gh belum terpasang"; exit 1; }
gh auth status >/dev/null 2>&1 || { echo "gh belum login (jalankan: gh auth login)"; exit 1; }

REPO="${RELEASE_REPO:-}"
if [ -z "$REPO" ]; then
  url="$(git remote get-url origin 2>/dev/null || true)"
  REPO="$(printf '%s' "$url" | sed -E 's#^(git@github\.com:|https://github\.com/)##; s#\.git$##')"
fi
[[ "$REPO" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]] || { echo "repo rilis tidak diketahui — set RELEASE_REPO=pemilik/nama"; exit 1; }

# Pembaruan hanya bisa diunduh pembeli kalau repo rilisnya PUBLIK.
vis="$(gh repo view "$REPO" --json visibility -q .visibility 2>/dev/null || echo UNKNOWN)"
[ "$vis" = "PUBLIC" ] || { echo "repo $REPO bukan publik ($vis) — pembeli tidak bisa mengunduh pembaruan. Pakai repo publik khusus rilis (RELEASE_REPO)."; exit 1; }
gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1 && { echo "rilis $TAG sudah ada di $REPO"; exit 1; }

GOOS_="$(go env GOOS)"; GOARCH_="$(go env GOARCH)"
[ "$GOOS_" = "darwin" ] || { echo "skrip ini membuat rilis macOS; jalankan di Mac"; exit 1; }
ASSET="milesshoot-${GOOS_}-${GOARCH_}.zip"
WIN_ASSET="milesshoot-windows-amd64.zip"

echo "==> tes"
go test ./... -count=1

echo "==> versi di wails.json → $VERSION"
python3 - "$VERSION" <<'PY'
import json, sys
p = "wails.json"; d = json.load(open(p))
d.setdefault("info", {})["productVersion"] = sys.argv[1]
json.dump(d, open(p, "w"), indent=2, ensure_ascii=False); open(p, "a").write("\n")
PY

LDFLAGS="-X main.appVersion=$VERSION -X main.updateRepo=$REPO"
rm -rf dist && mkdir -p dist
sign() {  # $1 = zip: tanda tangani, lalu pastikan kunci privat ini PASANGAN dari kunci publik tertanam
  go run ./cmd/relsign sign "$KEY" "$TAG" "$1" >/dev/null
  go run ./cmd/relsign verify "$TAG" "$1" >/dev/null || { echo "kunci privat tidak cocok dengan kunci publik tertanam — rilis dibatalkan"; exit 1; }
}

echo "==> build macOS $TAG ($REPO)"
wails build -clean -ldflags "$LDFLAGS"
APP=build/bin/milesshoot.app
# -clean menghapus isi build/bin, jadi program pembantu kamera kabel harus dimasukkan lagi ke dalam .app
[ -f build/helpers/milesshoot-kamera ] || { echo "build/helpers/milesshoot-kamera tidak ada"; exit 1; }
cp build/helpers/milesshoot-kamera "$APP/Contents/MacOS/milesshoot-kamera"
chmod +x "$APP/Contents/MacOS/milesshoot-kamera"
codesign --force --deep -s - "$APP"   # tanda tangan ad-hoc ulang karena isi bundle berubah
codesign --verify --deep --strict "$APP" || { echo "tanda tangan .app tidak valid"; exit 1; }
# --norsrc/--noextattr: jangan sertakan file bayangan "._*" di dalam zip
ditto -c -k --keepParent --norsrc --noextattr --noqtn "$APP" "dist/$ASSET"
LIST="$(unzip -Z1 "dist/$ASSET")"   # ke variabel dulu: "unzip | grep -q" bisa gagal karena pipefail + SIGPIPE
for need in milesshoot.app/Contents/MacOS/milesshoot milesshoot.app/Contents/MacOS/milesshoot-kamera milesshoot.app/Contents/Info.plist; do
  grep -qxF "$need" <<<"$LIST" || { echo "zip macOS tidak memuat $need"; exit 1; }
done
if grep -q '/\._' <<<"$LIST"; then echo "zip macOS memuat file bayangan ._*"; exit 1; fi
sign "dist/$ASSET"

echo "==> build Windows (amd64) $TAG"
wails build -platform windows/amd64 -ldflags "$LDFLAGS"   # tanpa -clean: jangan hapus build macOS
[ -f build/bin/milesshoot.exe ] || { echo "milesshoot.exe tidak terbentuk"; exit 1; }
(cd build/bin && zip -q -j "../../dist/$WIN_ASSET" milesshoot.exe)
[ "$(unzip -Z1 "dist/$WIN_ASSET")" = "milesshoot.exe" ] || { echo "zip Windows harus berisi tepat milesshoot.exe di tingkat atas"; exit 1; }
sign "dist/$WIN_ASSET"

echo "==> terbitkan $TAG di $REPO"
gh release create "$TAG" "dist/$ASSET" "dist/$ASSET.sig" "dist/$WIN_ASSET" "dist/$WIN_ASSET.sig" \
  --repo "$REPO" --title "$TAG" --notes "${NOTES:-Rilis $TAG}"
echo "selesai: https://github.com/$REPO/releases/tag/$TAG"
