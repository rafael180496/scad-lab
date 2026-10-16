#!/usr/bin/env python3
"""Textos visibles del backend escritos a mano, sin backend/i18n (ver
claude/05-i18n.md).

Sin línea base: cualquier hallazgo falla. Si algo aparece, se corrige con un
`i18n.Msg{ES, EN}`; no se «acepta».

Qué cuenta como texto visible (le llega a la interfaz o al usuario):
  - `fmt.Errorf("…")` y `errors.New("…")` con letras una vez quitados los verbos
    de formato: los errores de los bindings cruzan Wails como string y la UI los
    muestra con String(e). También los `var ErrX = errors.New(…)` de paquete
    (se resuelven con `i18n.New`, que traduce al mostrarse y conserva errors.Is).
  - Voseo en un texto en español (`ES:` de un Msg, assets/): la interfaz y los
    prompts van en español neutro con tú («elige», no «elegí»). Ver
    .claude/rules/conventions.md.
  - Campos de struct de texto (`Message`, `Title`, `Label`, `Description`,
    `Hint`, `Notice`, `Reason`, `Warning`, `Text`, `Detail`) con un literal que
    sea prosa (dos palabras o más), y las opciones nativas de Wails (`About`).

Qué NO cuenta, y por qué:
  - Los logs a stderr (`log.Printf`, `lg.Printf`, `e.log.Printf`…): son para quien
    depura, no para el usuario; van en español, como los comentarios.
  - Todo lo que esté dentro de un `i18n.Msg{…}`.
  - Los campos de texto de `backend/mcp/`: son las descripciones de las
    herramientas MCP, que lee el agente y no el usuario, y van en inglés a
    propósito (ver el comentario de backend/mcp/tools.go). Los errores de ese
    paquete sí se revisan.
  - Los archivos de test y `backend/i18n/`.
  - Una línea con `// i18n-ignore: <motivo>` (o con ese comentario en la línea
    de arriba): para lo que de verdad no se ve y no es un log (un centinela
    interno que nunca se muestra, un texto que ve la IA y no el usuario). El
    motivo es obligatorio: sin él el marcador no vale.

Uso: i18n-check-go.py [--list archivo.go]
"""
import os, re, sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CALL = re.compile(r'\b(?:fmt\.Errorf|errors\.New)\(\s*("(?:[^"\\\n]|\\.)*"|`[^`]*`)')
FIELD = re.compile(r'\b(?:Message|Title|Label|Description|Hint|Notice|Reason|Warning|Text|Detail)\s*:\s*("(?:[^"\\\n]|\\.)*"|`[^`]*`)')
VERB = re.compile(r'%[-+# 0-9.*]*[a-zA-Z%]')
LOG = re.compile(r'\b(?:log|lg|logger|slog)\.\w*(?:Print|Fatal|Panic|Info|Warn|Error|Debug)\w*\(|\.log\.\w+\(|\blog\.Print')
IGNORE = re.compile(r'//\s*i18n-ignore:\s*\S+')
MSG_OPEN = re.compile(r'\bi18n\.Msg\{|\bMsg\{')
VOSEO = re.compile(r'(?<![\w])(?:vos|sos|acá|tenés|podés|querés|sabés|hacés|ponés|decís|venís|elegís|usás|necesitás|preferís|estás|andás|elegí|creá|pedí|abrí|guardá|hacé|agregá|borrá|seleccioná|escribí|cambiá|revisá|probá|instalá|configurá|definí|reiniciá|preferí|reformulá|respondé|corregí|decí|mirá|usá|tené|poné|volvé|esperá|intentá|cerrá|activá|desactivá|confirmá|cancelá|verificá|ejecutá|descargá|importá|exportá|fijate|asegurate|dejá|pasá|corré|buscá|copiá|pegá|marcá|ajustá|completá|indicá|especificá|escogé|seguí|empezá|comenzá|iniciá|actualizá|restaurá|recuperá|duplicá|renombrá|ordená|filtrá|compará|chequeá|aceptá|presioná|apretá|tocá|arrastrá|soltá|movés|mové|sacá|salí|entrá|ingresá|rellená|leé|contame|avisame|pedime|decime)(?![\w])', re.IGNORECASE)
PROSE = re.compile(r'[^\W\d_]{2,}[ ,.:;]+[^\W\d_]{2,}')
AGENT_FACING = ('backend/mcp/',)  # solo se exime el chequeo de campos de texto
SKIP_DIRS = {'node_modules', 'frontend', 'build', 'releases', '.git', '.codegraph', 'scripts'}


def hits(path, rel=''):
    out = []
    with open(path, encoding='utf-8') as f:
        lines = f.read().split('\n')
    for n, line in enumerate(lines, 1):
        s = line.lstrip()
        if s.startswith('//'):
            continue
        if IGNORE.search(line) or (n > 1 and IGNORE.search(lines[n - 2]) and lines[n - 2].lstrip().startswith('//')):
            continue
        if LOG.search(line):
            continue
        if re.search(r'\bES:', line):
            for m in VOSEO.finditer(line):
                out.append((n, 'voseo «%s» (usa tú)' % m.group(0)))
        # Los textos de una línea que abre un Msg{…} ya están en los dos idiomas.
        msg = MSG_OPEN.search(line)
        for m in CALL.finditer(line):
            if msg and msg.start() < m.start():
                continue
            lit = m.group(1)[1:-1]
            if re.search(r'[^\W\d_]{2,}', VERB.sub('', lit)):
                out.append((n, lit[:90]))
        for m in ([] if rel.startswith(AGENT_FACING) else FIELD.finditer(line)):
            if msg and msg.start() < m.start():
                continue
            lit = m.group(1)[1:-1]
            if PROSE.search(VERB.sub('', lit)):
                out.append((n, lit[:90]))
    return out


def asset_hits(path):
    """Voseo en los textos en español que van al agente o al usuario (assets/)."""
    out = []
    with open(path, encoding='utf-8') as f:
        for n, line in enumerate(f, 1):
            for m in VOSEO.finditer(line):
                out.append((n, 'voseo «%s» (usa tú)' % m.group(0)))
    return out


def asset_files():
    for d, dirs, fs in os.walk(os.path.join(ROOT, 'assets')):
        for f in fs:
            rel = os.path.relpath(os.path.join(d, f), ROOT)
            # Los archivos en inglés no pueden tener voseo; evita falsos positivos («sos»).
            if f.endswith(('.md', '.tmpl')) and '.en.' not in f:
                yield rel


def files():
    for d, dirs, fs in os.walk(ROOT):
        dirs[:] = [x for x in dirs if x not in SKIP_DIRS and not x.startswith('.')]
        for f in fs:
            rel = os.path.relpath(os.path.join(d, f), ROOT)
            if f.endswith('.go') and not f.endswith('_test.go') and not rel.startswith('backend/i18n/'):
                yield rel


def main():
    args = sys.argv[1:]
    if args[:1] == ['--list']:
        for n, t in hits(os.path.join(ROOT, args[1]), args[1]):
            print(f'{args[1]}:{n}  {t}')
        return 0
    found = {}
    for rel in files():
        h = hits(os.path.join(ROOT, rel), rel)
        if h:
            found[rel] = h
    for rel in asset_files():
        h = asset_hits(os.path.join(ROOT, rel))
        if h:
            found[rel] = h
    total = sum(len(h) for h in found.values())
    if total:
        print('i18n (Go): texto visible sin backend/i18n (claude/05-i18n.md); usa i18n.Msg{ES, EN}, i18n.Errorf o i18n.New:', file=sys.stderr)
        for rel, h in sorted(found.items()):
            for n, t in h:
                print(f'  {rel}:{n}: {t}', file=sys.stderr)
        print(f'\n{total} texto(s) en {len(found)} archivo(s).', file=sys.stderr)
        return 1
    nfiles = sum(1 for _ in files()) + sum(1 for _ in asset_files())
    print(f'i18n (Go) OK — {nfiles} archivos revisados')
    return 0


sys.exit(main())
