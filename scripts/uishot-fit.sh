#!/usr/bin/env bash
# Comprueba que el documento NO se desplaza ni mide más que la ventana
# (el bug del documento corrido hacia arriba y a la izquierda).
#
#   ./scripts/uishot-fit.sh                         # vistas y tamaños de la tabla
#   ./scripts/uishot-fit.sh terminal 1024 640       # una combinación
#   UISHOT_SCALE=150 ./scripts/uishot-fit.sh        # con otra escala de interfaz
#
# Por cada combinación abre el banco con poke=1 (que intenta desplazar la
# página con scrollTo, scrollTop, focus() y scrollIntoView) y lee el informe
# data-docfit que deja uishot.tsx. Falla si scrollWidth/scrollHeight superan la
# ventana, si hay desplazamiento, si la cabecera/barra de estado no quedaron
# en su sitio (cabecera en 0,0; barra de estado pegada al borde inferior) o si el
# contenido de la cabecera es más ancho que ella (las pestañas de proyecto tienen que
# acortarse en vez de empujar los botones de exportar fuera de la ventana).
set -euo pipefail
cd "$(dirname "$0")/../frontend"

CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
[ -x "$CHROME" ] || { echo "No se encontró Google Chrome en $CHROME"; exit 1; }
# La ventana headless reserva 87 px de barra de pestañas: se suman para que el
# viewport mida exactamente el alto pedido (como la ventana real de Wails).
# Perfil propio y desechable: sin él Chrome headless comparte el perfil por defecto con el Chrome
# del usuario (si está abierto, la llamada se le entrega a él y no devuelve nada).
PROFILE="$(mktemp -d "${TMPDIR:-/tmp}/uishot-chrome.XXXXXX")"
trap 'rm -rf "$PROFILE"' EXIT
PORT="${UISHOT_PORT:-5199}"
SCALE="${UISHOT_SCALE:-100}"

if ! curl -sf "http://localhost:$PORT/uishot.html" >/dev/null 2>&1; then
    pnpm exec vite --port "$PORT" --strictPort >"${TMPDIR:-/tmp}/uishot-fit-vite.log" 2>&1 &
    VITE_PID=$!
    trap 'rm -rf "$PROFILE"; kill $VITE_PID 2>/dev/null || true; lsof -ti "tcp:$PORT" -sTCP:LISTEN 2>/dev/null | xargs kill 2>/dev/null || true' EXIT
    for _ in $(seq 1 60); do curl -sf "http://localhost:$PORT/uishot.html" >/dev/null && break; sleep 0.5; done
fi

if [ $# -ge 3 ]; then
    COMBOS="$1 $2 $3"
else
    COMBOS=""
    for v in main projects-tabs projects-plus-menu terminal chat problems settings-general empty dock-custom dock-hidden dock-view-menu dock-minimized dock-move-menu viewer-parts print-panel plate-overflow tree-menu tree-active-branch trash diff-parent; do
        for s in "1920 1000" "1440 900" "1280 720" "1024 640"; do
            COMBOS="$COMBOS$v $s
"
        done
    done
fi

# Pasada de calentamiento: en frío Vite transforma las dependencias en la
# primera carga y esa página sale vacía (sin informe). Se descarta.
"$CHROME" --headless=new --disable-gpu --use-angle=swiftshader --enable-unsafe-swiftshader --use-gl=angle \
    --user-data-dir="$PROFILE" --window-size=1280,800 --virtual-time-budget=8000 --dump-dom \
    "http://localhost:$PORT/uishot.html?view=terminal&lang=es" >/dev/null 2>&1 || true

fail=0
while read -r view w h; do
    [ -z "${view:-}" ] && continue
    dom=$("$CHROME" --headless=new --disable-gpu --hide-scrollbars \
        --use-angle=swiftshader --enable-unsafe-swiftshader --use-gl=angle \
        --user-data-dir="$PROFILE" --window-size="$w,$((h + 87))" --virtual-time-budget=4000 --dump-dom \
        "http://localhost:$PORT/uishot.html?view=$view&lang=es&scale=$SCALE&poke=1" 2>/dev/null || true)
    json=$(printf '%s' "$dom" | grep -o "data-docfit='[^']*'\|data-docfit=\"[^\"]*\"" | head -1 | sed -e "s/^data-docfit=.//" -e 's/.$//' | sed 's/&quot;/"/g' || true)
    if [ -z "$json" ]; then echo "FALLA  $view ${w}x${h}: sin informe"; fail=1; continue; fi
    verdict=$(printf '%s' "$json" | python3 -c '
import json,sys
d=json.loads(sys.stdin.read())
bad=[]
if d["sw"]>d["iw"]: bad.append("scrollWidth %d > ancho %d"%(d["sw"],d["iw"]))
if d["sh"]>d["ih"]: bad.append("scrollHeight %d > alto %d"%(d["sh"],d["ih"]))
if d["sx"] or d["sy"] or d["rootTop"]: bad.append("desplazado (%d,%d,root %d)"%(d["sx"],d["sy"],d["rootTop"]))
if d["headerTop"] not in (0,None) or d["headerLeft"] not in (0,None): bad.append("cabecera en (%s,%s)"%(d["headerLeft"],d["headerTop"]))
if d.get("headerSw") and d["headerSw"]>d["headerCw"]: bad.append("la cabecera desborda: contenido %d > ancho %d"%(d["headerSw"],d["headerCw"]))
if d["footerBottom"] not in (d["ih"],None): bad.append("barra de estado termina en %s (alto %d)"%(d["footerBottom"],d["ih"]))
if d["footerRight"] not in (d["iw"],None): bad.append("barra de estado termina a la derecha en %s (ancho %d)"%(d["footerRight"],d["iw"]))
print("OK     " if not bad else "FALLA  ", "; ".join(bad))
')
    echo "$verdict $view ${w}x${h} scale=$SCALE  $json"
    case "$verdict" in FALLA*) fail=1 ;; esac
done <<<"$COMBOS"
exit $fail
