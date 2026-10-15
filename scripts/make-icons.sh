#!/usr/bin/env bash
# Regenera los iconos de la app desde assets/brand/icon.svg:
#   assets/brand/icon-1024.png   rasterizado maestro
#   build/appicon.png            icono de macOS (Wails arma el .icns con él)
#   build/windows/icon.ico       icono de Windows, 16–256 px
#   frontend/public/favicon.png  favicon del frontend
#
# Rasteriza con Chrome headless y no con ImageMagick: el lector SVG interno de
# ImageMagick ignora los degradados y deja el icono en negro.
# Requiere Google Chrome y ImageMagick (`brew install imagemagick`). Solo macOS.
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
[[ -x "$CHROME" ]] || { echo "No se encontró Google Chrome en $CHROME" >&2; exit 1; }
command -v magick >/dev/null || { echo "Falta ImageMagick (brew install imagemagick)" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
printf '<html><body style="margin:0;background:transparent"><img src="file://%s" width="1024" height="1024"></body></html>' \
  "$ROOT_DIR/assets/brand/icon.svg" > "$TMP/icon.html"

"$CHROME" --headless=new --disable-gpu --hide-scrollbars --default-background-color=00000000 \
  --allow-file-access-from-files --window-size=1024,1024 \
  --screenshot="$ROOT_DIR/assets/brand/icon-1024.png" "file://$TMP/icon.html" 2>/dev/null

cp assets/brand/icon-1024.png build/appicon.png
magick assets/brand/icon-1024.png -define icon:auto-resize=256,128,64,48,32,24,16 build/windows/icon.ico
mkdir -p frontend/public
magick assets/brand/icon-1024.png -resize 64x64 frontend/public/favicon.png

echo "==> Iconos regenerados:"
ls -l assets/brand/icon-1024.png build/appicon.png build/windows/icon.ico frontend/public/favicon.png
