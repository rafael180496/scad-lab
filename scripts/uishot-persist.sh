#!/usr/bin/env bash
# «Cerrar y abrir» de la interfaz: cambia lo visual en el banco de capturas, recarga la
# página como si se cerrara y se abriera la app, y comprueba por CDP que todo vuelve como se
# dejó (checkpoint elegido de cada proyecto, estado del visor, paneles, barras, maximizado) y
# que el modo enfoque NO persiste. Lo que Go guardaría en scadlab.db, el banco lo guarda en
# localStorage (persist=reset|1 en frontend/src/uishot.tsx). No toca la base real ni abre la app.
#
#   ./scripts/uishot-persist.sh
set -euo pipefail
cd "$(dirname "$0")/../frontend"

CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
[ -x "$CHROME" ] || { echo "No se encontró Google Chrome en $CHROME"; exit 1; }
PROFILE="$(mktemp -d "${TMPDIR:-/tmp}/uishot-persist.XXXXXX")"
VITE_PORT="${UISHOT_PORT:-5197}"
CDP_PORT="${UISHOT_CDP_PORT:-9341}"
cleanup() {
    rm -rf "$PROFILE"
    [ -n "${CHROME_PID:-}" ] && kill "$CHROME_PID" 2>/dev/null || true
    [ -n "${VITE_PID:-}" ] && kill "$VITE_PID" 2>/dev/null || true
    lsof -ti "tcp:$VITE_PORT" -sTCP:LISTEN 2>/dev/null | xargs kill 2>/dev/null || true
}
trap cleanup EXIT

if ! curl -sf "http://localhost:$VITE_PORT/uishot.html" >/dev/null 2>&1; then
    pnpm exec vite --port "$VITE_PORT" --strictPort >"${TMPDIR:-/tmp}/uishot-persist-vite.log" 2>&1 &
    VITE_PID=$!
    for _ in $(seq 1 60); do curl -sf "http://localhost:$VITE_PORT/uishot.html" >/dev/null && break; sleep 0.5; done
fi

# Chrome headless (nunca visible), con su propio perfil y el puerto de depuración en loopback.
"$CHROME" --headless=new --disable-gpu --use-angle=swiftshader --enable-unsafe-swiftshader --use-gl=angle \
    --user-data-dir="$PROFILE" --remote-debugging-port="$CDP_PORT" --remote-allow-origins='*' \
    --window-size=1440,987 about:blank >/dev/null 2>&1 &
CHROME_PID=$!
for _ in $(seq 1 60); do curl -sf "http://127.0.0.1:$CDP_PORT/json/version" >/dev/null && break; sleep 0.25; done

node ../scripts/uishot-persist.mjs "$CDP_PORT" "$VITE_PORT"
