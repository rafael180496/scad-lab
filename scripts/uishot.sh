#!/usr/bin/env bash
# Captura una vista de la interfaz sin abrir la app.
#
# Por qué existe (igual que en mini-tools): la app es de escritorio, y
# fotografiarla desde afuera pide el permiso de grabación de pantalla de macOS,
# que un proceso automatizado no tiene. Acá la app se monta en Chrome headless
# con los bindings de Wails simulados y datos inventados (frontend/src/uishot.tsx).
#
#   ./scripts/uishot.sh main                     # workspace con checkpoints renderizados
#   ./scripts/uishot.sh render-error 1440 900    # con tamaño
#   ./scripts/uishot.sh settings-ai              # Configuración, pestaña IA
#   UISHOT_LANG=es ./scripts/uishot.sh main      # en español (por defecto, inglés)
#   UISHOT_THEME=light ./scripts/uishot.sh main  # tema claro (por defecto, oscuro)
#   UISHOT_PLATFORM=windows ./scripts/uishot.sh main
#   UISHOT_SCALE=150 ./scripts/uishot.sh main    # interfaz al 150 % (90|100|115|130|150)
#   UISHOT_OUT=/ruta/x.png ./scripts/uishot.sh main
#
# Vistas: las claves de VIEWS en frontend/src/uishot.tsx.
set -euo pipefail

VIEW="${1:-main}"
W="${2:-1440}"
H="${3:-900}"
OUT="${UISHOT_OUT:-${TMPDIR:-/tmp}/uishot-$VIEW.png}"
LANG_UI="${UISHOT_LANG:-en}"
THEME="${UISHOT_THEME:-dark}"
PLATFORM="${UISHOT_PLATFORM:-darwin}"
SCALE="${UISHOT_SCALE:-100}"
PORT="${UISHOT_PORT:-5198}"
BUDGET="${UISHOT_BUDGET:-4000}"

CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
[ -x "$CHROME" ] || { echo "No se encontró Google Chrome en $CHROME"; exit 1; }

cd "$(dirname "$0")/../frontend"

# Perfil propio y desechable: con el perfil por defecto, si el Chrome del usuario está abierto
# la llamada se le entrega a él y no se captura nada.
PROFILE="$(mktemp -d "${TMPDIR:-/tmp}/uishot-chrome.XXXXXX")"
trap 'rm -rf "$PROFILE"' EXIT

# Servidor propio y efímero en otro puerto: no pisa un `wails dev` abierto.
# UISHOT_KEEP_SERVER=1 lo reutiliza si ya está levantado (help-shots.sh).
if ! curl -sf "http://localhost:$PORT/uishot.html" >/dev/null 2>&1; then
    pnpm exec vite --port "$PORT" --strictPort >"${TMPDIR:-/tmp}/uishot-vite.log" 2>&1 &
    VITE_PID=$!
    trap 'rm -rf "$PROFILE"; kill $VITE_PID 2>/dev/null || true' EXIT
    for _ in $(seq 1 60); do
        curl -sf "http://localhost:$PORT/uishot.html" >/dev/null && break
        sleep 0.5
    done
fi

# Perfil propio y desechable: con el perfil por defecto, si el Chrome del usuario está abierto
# la llamada se le entrega a él y no se captura nada.
# --virtual-time-budget deja que React monte y que las promesas de los mocks
# resuelvan antes de disparar: sin eso la foto sale en blanco.
# WebGL (visor 3D) en headless: sin GPU, por SwiftShader vía ANGLE; si no, el
# canvas sale negro o vacío.
"$CHROME" --headless=new --disable-gpu --hide-scrollbars \
    --use-angle=swiftshader --enable-unsafe-swiftshader --use-gl=angle \
    --user-data-dir="$PROFILE" --run-all-compositor-stages-before-draw \
    --window-size="$W,$H" \
    --virtual-time-budget="$BUDGET" \
    --screenshot="$OUT" \
    "http://localhost:$PORT/uishot.html?view=$VIEW&lang=$LANG_UI&theme=$THEME&platform=$PLATFORM&scale=$SCALE" >/dev/null 2>&1

[ -s "$OUT" ] || { echo "La captura salió vacía. Log de vite:"; tail -5 "${TMPDIR:-/tmp}/uishot-vite.log"; exit 1; }
echo "$OUT"
