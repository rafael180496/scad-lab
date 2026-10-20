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
#
# Barra lateral (vista sidebar-long: workspace y cadena de checkpoints con nombres de 60
# caracteres): tras empujar de lado sus listas, enfocar todos los nodos del árbol, abrir el
# renombrado en línea con un texto larguísimo y pedir scrollIntoView horizontal, falla si algún
# contenedor marcado con data-lock quedó con scrollLeft != 0, si una lista de la barra tiene
# contenido más ancho que su caja (scrollWidth > clientWidth: nombres cortados sin forma de
# verlos), si el primer rótulo de la barra no está en su sitio (x = 12) o si, en macOS, el botón de
# la barra lateral queda debajo de los semáforos (x < 76).
#
# Editor de STL (S13): vistas mesh-* y mesh-edit-w320…1100 (el editor solo, en un panel de ese ancho): ni la barra, ni el panel lateral,
# ni el pie, ni el editor entero pueden tener contenido más ancho que su caja.
#
# Chat (S12.b): la barra superior es de una sola línea y falla si su contenido es más ancho que ella (un
# control cortado o con scroll horizontal), lo mismo la lista de mensajes y el panel entero. Las vistas
# chat-w320/480/720/1200 son el chat solo, en un panel de ese ancho (se fija en el contenedor: Chrome headless
# no baja de ~500 px de ventana): con poke=1 abren el
# selector de proveedor, el de modelo, el del modo, el de herramientas (solo chat-sw*), el menú «…» y la lista de conversaciones, y fallan si
# alguno de esos desplegables se sale de la ventana.
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
    for v in main sidebar-long projects-tabs projects-plus-menu terminal chat chat-agent-steps chat-attachments chat-conversations chat-from-image chat-no-vision chat-compare chat-trace vault-create vault-unlock problems settings-general empty dock-custom dock-hidden dock-view-menu dock-minimized dock-move-menu viewer-parts print-panel plate-overflow tree-menu tree-active-branch trash diff-parent mesh-import mesh-edit mesh-cut mesh-hole mesh-ai workspace-new-printer printer-picker workspace-config settings-printing export-3mf; do
        for s in "1920 1000" "1440 900" "1280 720" "1024 640"; do
            COMBOS="$COMBOS$v $s
"
        done
    done
    # El chat solo, del ancho del panel más angosto (320) al más ancho (1200): ver arriba.
    # (más los anchos justo a cada lado de los cortes de useChatTier: 480, 720 y 900)
    # El editor de STL solo (S13), del ancho de un panel angosto (320) a uno ancho: la barra, el panel y el pie no desbordan.
    for c in "mesh-edit-w320 520 700" "mesh-edit-w480 640 700" "mesh-edit-w639 760 700" "mesh-edit-w640 760 700" "mesh-edit-w720 900 760" "mesh-edit-w1100 1300 800"; do
        COMBOS="$COMBOS$c
"
    done
    # chat-sw*: lo mismo con un servidor de modelos (S17): suma el selector del perfil de herramientas y el aviso de contexto.
    for c in "chat-w320 520 700" "chat-w479 600 700" "chat-w480 640 700" "chat-narrow 520 760" "chat-w719 800 700" "chat-w720 900 760" "chat-w899 1000 760" "chat-w900 1000 760" "chat-w1200 1400 800" "chat-sw320 520 700" "chat-sw479 600 700" "chat-sw480 640 700" "chat-sw719 800 700" "chat-sw720 900 760" "chat-sw899 1000 760" "chat-sw900 1000 760" "chat-sw1200 1400 800"; do
        COMBOS="$COMBOS$c
"
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
if d.get("lockMoved"): bad.append("contenedores desplazados: "+", ".join(d["lockMoved"]))
for i,b in enumerate(d.get("sideBoxes") or []):
    if b["sw"]>b["cw"]: bad.append("lista %d de la barra lateral con contenido más ancho que su caja (%d > %d)"%(i,b["sw"],b["cw"]))
    if b["sl"]: bad.append("lista %d de la barra lateral desplazada en horizontal (%d)"%(i,b["sl"]))
if d.get("sideTitleLeft") not in (None,12): bad.append("el rótulo de la barra lateral está en x=%s (esperaba 12)"%d["sideTitleLeft"])
if d.get("platform")=="darwin" and d.get("toggleLeft") is not None and d["toggleLeft"]<76: bad.append("el botón de la barra lateral (x=%d) queda bajo los semáforos"%d["toggleLeft"])
for b in d.get("chatBoxes") or []:
    if b["sw"]>b["cw"]: bad.append("chat: %s con contenido más ancho que su caja (%d > %d)"%(b["name"],b["sw"],b["cw"]))
for b in d.get("meshBoxes") or []:
    if b["sw"]>b["cw"]: bad.append("editor de STL: %s con contenido más ancho que su caja (%d > %d)"%(b["name"],b["sw"],b["cw"]))
pr=d.get("popRects")
if pr is not None:
    if not pr.get("done"): bad.append("chat: el recorrido de desplegables no terminó (%s)"%list(pr.keys()))
    for name,r in pr.items():
        if name=="done" or r is None: continue
        if r[0]<0 or r[1]<0 or r[2]>d["iw"] or r[3]>d["ih"]: bad.append("chat: el desplegable %s se sale de la ventana: %s en %dx%d"%(name,r,d["iw"],d["ih"]))
if d["footerBottom"] not in (d["ih"],None): bad.append("barra de estado termina en %s (alto %d)"%(d["footerBottom"],d["ih"]))
if d["footerRight"] not in (d["iw"],None): bad.append("barra de estado termina a la derecha en %s (ancho %d)"%(d["footerRight"],d["iw"]))
print("OK     " if not bad else "FALLA  ", "; ".join(bad))
')
    echo "$verdict $view ${w}x${h} scale=$SCALE  $json"
    case "$verdict" in FALLA*) fail=1 ;; esac
done <<<"$COMBOS"
exit $fail
