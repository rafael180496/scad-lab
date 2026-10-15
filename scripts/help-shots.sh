#!/usr/bin/env bash
# Regenera las capturas de la ayuda en los dos idiomas: inglés en
# docs/screenshots/ y español en docs/screenshots/es/ (la ayuda en español,
# index.html, usa las de es/; la inglesa de S10 usará las de la raíz).
#
#   ./scripts/help-shots.sh              todas las de la tabla
#   ./scripts/help-shots.sh render-error solo esa
#
# Cada fila dice qué vista del banco (frontend/src/uishot.tsx) produce el
# archivo y con qué tamaño. Una captura nueva de la ayuda = una fila acá y,
# si hace falta, una vista nueva en el banco. Nunca una captura de la app real.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# archivo           vista             ancho alto
SHOTS='
app-main            main              1440  900
app-empty           empty             1280  800
render-error        render-error      1440  900
render-missing      render-missing    1280  800
viewer              viewer            1440  900
viewer-wireframe    viewer-wireframe  1440  900
viewer-split        viewer-split      1440  900
viewer-top          viewer-top        1440  900
editor              editor            1440  900
editor-edit         editor-edit       1440  900
editor-search       editor-search     1440  900
problems            problems          1440  900
save-checkpoint     save-checkpoint   1440  900
new-workspace       newworkspace      1280  800
settings-general    settings-general  1280  820
settings-openscad   settings-openscad 1280  820
settings-ai         settings-ai       1280  900
settings-printing   settings-printing 1280  860
settings-skills     settings-skills   1280  860
settings-about      settings-about    1280  700
app-main-light      main              1440  900
'

ONLY="${1:-}"
PORT=5198
mkdir -p docs/screenshots/es

# Un solo vite para todas las capturas.
(cd frontend && pnpm exec vite --port "$PORT" --strictPort >"${TMPDIR:-/tmp}/uishot-vite.log" 2>&1) &
VITE_PID=$!
# vite corre como nieto (subshell → pnpm → node): además del grupo, se corta
# lo que siga escuchando en el puerto para no dejar un servidor huérfano.
trap 'kill $VITE_PID 2>/dev/null || true; pkill -P $VITE_PID 2>/dev/null || true; lsof -ti "tcp:$PORT" -sTCP:LISTEN 2>/dev/null | xargs kill 2>/dev/null || true' EXIT
for _ in $(seq 1 60); do
    curl -sf "http://localhost:$PORT/uishot.html" >/dev/null && break
    sleep 0.5
done
# Pasada de calentamiento: en frío Vite transforma three y el visor 3D en la
# primera carga, y esa foto sale con el canvas vacío. Se descarta.
UISHOT_PORT=$PORT UISHOT_OUT="${TMPDIR:-/tmp}/uishot-warmup.png" ./scripts/uishot.sh viewer 1440 900 >/dev/null || true

echo "$SHOTS" | while read -r file view w h; do
    [ -z "$file" ] && continue
    [ -n "$ONLY" ] && [ "$ONLY" != "$file" ] && continue
    theme=dark
    case "$file" in *-light) theme=light ;; esac
    for lang in en es; do
        dir=docs/screenshots
        [ "$lang" = es ] && dir=docs/screenshots/es
        UISHOT_PORT=$PORT UISHOT_LANG=$lang UISHOT_THEME=$theme UISHOT_OUT="$ROOT/$dir/$file.png" \
            ./scripts/uishot.sh "$view" "$w" "$h" >/dev/null
        echo "  $dir/$file.png"
    done
done
