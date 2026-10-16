# S10 — Pulido antes de la primera versión

Fase del [roadmap](../../claude/01-roadmap.md). **No se empaqueta ni se
publica nada en esta fase**: preparar la versión es un trigger explícito del
usuario («preparar la versión», «empaquetar», «versión oficial») y sigue
`.claude/specs/releases.md` completo (igual que mini-tools).

## Reparto (dos devs en Sonnet, en paralelo)

| Bloque | Dueño | Alcance |
|---|---|---|
| 10.A ayuda bilingüe | fullstack-dev A | `index.html` (pasa a **inglés**), `es/index.html` (nuevo: la ayuda actual en español), `README.md` (estado al día) |
| 10.B i18n estricto y limpieza | fullstack-dev B | `scripts/i18n-check*.{sh,py}` + `frontend/scripts/i18n-check.mjs`, `frontend/**`, código Go con textos sin `i18n.Msg`, `claude/05-i18n.md` |

### 10.A — ayuda en inglés y español (como mini-tools)

- Referencia: `/Users/developer/Documents/project/github_project/golang/mini-tools/index.html`
  (inglés) y `.../mini-tools/es/index.html` (español): `hreflang` alternos en el
  `<head>`, botón «Español»/«English» en la barra (`#langsw`) que conserva el
  tema/ancla actual, y las capturas: inglés `docs/screenshots/*.png`, español
  `../docs/screenshots/es/*.png`.
- `es/index.html` = la ayuda actual (español) movida, con las rutas relativas
  corregidas (capturas, favicon, enlaces a `CHANGELOG.md`, etc.).
- `index.html` = traducción completa al inglés (mismos temas, mismos `id`, mismas
  figuras con las capturas en inglés), sin texto en español salvo nombres
  propios.
- Ambas: insignia de versión y pie que describan el estado real (no «la
  fundación»: hoy están S0–S9 + paneles), sin «En desarrollo» en nada que ya
  funcione. Verificar enlaces internos (`href="#…"` → `id` existentes), que
  todas las imágenes existan, `node --check` del JS embebido, y mirar ambas
  páginas con Chrome headless.
- `README.md`: tabla de estado al día (todo lo hecho en S0–S9), requisitos,
  enlaces a la ayuda en los dos idiomas, sección de descarga apuntando a los
  assets del Release (`.../releases/download/vX.Y.Z/...`) aunque todavía no haya
  ninguno publicado (decirlo).

### 10.B — i18n estricto y limpieza

- **Go**: portar el chequeo de mini-tools (`scripts/i18n-check-go.py` +
  `scripts/i18n-check.sh` que corre ambos, con línea base): textos visibles
  (errores devueltos por bindings, mensajes a la UI) que no pasen por
  `i18n.Msg`. Corregir lo que aparezca; los que de verdad no son visibles
  (logs a stderr) se excluyen con criterio documentado.
- **Frontend**: endurecer `frontend/scripts/i18n-check.mjs` para que también
  vea textos en `toast.*(...)`, `confirmDialog(...)`, `title=`/`aria-label=`
  con expresiones de plantilla, y strings sueltos en `lib/` que lleguen a la UI.
  Corregir lo que aparezca. Detectar **claves sin uso** en `es.ts`/`en.ts` y
  borrarlas (con cuidado de las que se arman dinámicamente: documentarlas).
- **Español neutro con tú** en todo (regla de `conventions.md`): buscar voseo
  restante en UI, mensajes Go, prompts y skills.
- Limpieza: `console.log` olvidados, `TODO` viejos, código muerto evidente
  (sin refactors grandes).

## Criterio de terminado

Verificación completa del repo + `./scripts/i18n-check.sh` en verde +
`./scripts/uishot-fit.sh` OK + prueba de humo de la app. Nadie ejecuta Bambu
Studio ni empaqueta.
