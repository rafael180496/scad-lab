#!/usr/bin/env bash
# Chequeo de textos sin i18n, frontend y backend (ver claude/05-i18n.md).
# Sin línea base: cualquier hallazgo falla. Se corrige en el diccionario
# (frontend/src/i18n/es.ts y en.ts) o con i18n.Msg en Go.
#
#   ./scripts/i18n-check.sh                              los dos chequeos
#   ./scripts/i18n-check.sh --list components/Foo.tsx    hallazgos de un archivo del frontend
#   ./scripts/i18n-check.sh --list backend/store/x.go    hallazgos de un archivo Go
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
if [ "${1:-}" = "--list" ]; then
    case "${2:-}" in
        *.go) exec python3 "$here/i18n-check-go.py" --list "$2" ;;
        *) cd "$here/../frontend" && exec node scripts/i18n-check.mjs --list "$2" ;;
    esac
fi
status=0
(cd "$here/../frontend" && node scripts/i18n-check.mjs) || status=1
python3 "$here/i18n-check-go.py" || status=1
exit $status
