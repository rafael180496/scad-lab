package store

import (
	"database/sql"

	"scadlab/backend/i18n"
)

// migration es un cambio de schema. Reglas duras (ver claude/04-database.md):
//
//   - Solo aditivas: CREATE TABLE IF NOT EXISTS o ALTER TABLE … ADD COLUMN …
//     DEFAULT …. Nunca DROP ni reescribir filas: scadlab.db vive en máquinas
//     reales y no se puede recrear.
//   - Una migración publicada no se edita. Un cambio posterior es una versión
//     nueva al final de la lista.
//   - Cada una corre una sola vez, dentro de su propia transacción.
type migration struct {
	version int
	desc    string
	sql     string
}

var migrations = []migration{
	{
		version: 1,
		desc:    "schema base: settings, workspaces, checkpoints, skills, proveedores de IA, materiales",
		sql: `
CREATE TABLE IF NOT EXISTS settings (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS workspaces (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT NOT NULL,
	path        TEXT NOT NULL UNIQUE,
	printer_id  TEXT NOT NULL DEFAULT 'bambu-a1-mini',
	description TEXT NOT NULL DEFAULT '',
	last_opened INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL
);

-- Índice de los .scad de cada workspace. La fuente de verdad es el archivo
-- (su encabezado // @checkpoint); esta tabla se reconstruye con un rescan.
CREATE TABLE IF NOT EXISTS checkpoints (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	file_name    TEXT NOT NULL,
	parent_file  TEXT NOT NULL DEFAULT '',
	label        TEXT NOT NULL DEFAULT '',
	description  TEXT NOT NULL DEFAULT '',
	session_id   TEXT NOT NULL DEFAULT '',
	collapsed    INTEGER NOT NULL DEFAULT 0,
	file_mtime   INTEGER NOT NULL DEFAULT 0,
	created_at   INTEGER NOT NULL,
	UNIQUE (workspace_id, file_name)
);

-- Registro de skills: las incluidas (source='builtin', vienen embebidas en el
-- binario) y las del usuario (source='user', viven en el directorio de datos).
CREATE TABLE IF NOT EXISTS skills (
	name        TEXT PRIMARY KEY,
	source      TEXT NOT NULL CHECK (source IN ('builtin','user')),
	description TEXT NOT NULL DEFAULT '',
	enabled     INTEGER NOT NULL DEFAULT 1,
	updated_at  INTEGER NOT NULL
);

-- Proveedores de IA, mismo catálogo que mini-tools: CLIs agénticos (claude,
-- opencode, codex, antigravity) y servidores de modelos (ollama, cualquier API
-- compatible con OpenAI —LM Studio, vLLM, OpenRouter…— y la API de Anthropic).
-- config es JSON propio de cada tipo. Nunca guarda una API key: guarda el
-- NOMBRE de la variable de entorno que la tiene (apiKeyEnv).
CREATE TABLE IF NOT EXISTS ai_providers (
	id         TEXT PRIMARY KEY,
	kind       TEXT NOT NULL CHECK (kind IN ('cli','ollama','openai','anthropic')),
	name       TEXT NOT NULL,
	command    TEXT NOT NULL DEFAULT '',
	config     TEXT NOT NULL DEFAULT '{}',
	enabled    INTEGER NOT NULL DEFAULT 1,
	sort_order INTEGER NOT NULL DEFAULT 0,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS materials (
	id              TEXT PRIMARY KEY,
	name            TEXT NOT NULL,
	density_g_cm3   REAL NOT NULL,
	cost_per_kg     REAL NOT NULL,
	sort_order      INTEGER NOT NULL DEFAULT 0
);

INSERT OR IGNORE INTO materials (id, name, density_g_cm3, cost_per_kg, sort_order) VALUES
	('pla',   'PLA',   1.24, 20, 1),
	('petg',  'PETG',  1.27, 22, 2),
	('abs',   'ABS',   1.04, 22, 3),
	('asa',   'ASA',   1.07, 25, 4),
	('tpu',   'TPU',   1.21, 35, 5),
	('pla-cf','PLA-CF',1.30, 35, 6);

INSERT OR IGNORE INTO ai_providers (id, kind, name, command, config, enabled, sort_order, updated_at) VALUES
	('claude',      'cli',    'Claude Code', 'claude',      '{}', 1, 1, strftime('%s','now')),
	('opencode',    'cli',    'opencode',    'opencode',    '{}', 1, 2, strftime('%s','now')),
	('codex',       'cli',    'Codex',       'codex',       '{}', 1, 3, strftime('%s','now')),
	('antigravity', 'cli',    'Antigravity', 'agy',         '{}', 1, 4, strftime('%s','now')),
	('ollama',      'ollama', 'Ollama',      '',            '{"baseUrl":"http://localhost:11434","model":""}', 1, 5, strftime('%s','now')),
	('openai',      'openai', 'OpenAI-compatible', '',      '{"baseUrl":"http://localhost:1234/v1","model":"","apiKeyEnv":"OPENAI_API_KEY"}', 0, 6, strftime('%s','now')),
	('anthropic',   'anthropic', 'Anthropic API', '',       '{"model":"claude-sonnet-5-5","apiKeyEnv":"ANTHROPIC_API_KEY"}', 0, 7, strftime('%s','now'));
`,
	},
	{
		version: 2,
		desc:    "chat de diseño con modelos: chats y chat_messages",
		sql: `
-- Un chat pertenece a un workspace y recuerda con qué proveedor y modelo se
-- abrió. provider_id no es FK a ai_providers a propósito: un proveedor se
-- puede deshabilitar o renombrar y el chat viejo tiene que seguir legible.
CREATE TABLE IF NOT EXISTS chats (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	provider_id  TEXT NOT NULL,
	model        TEXT NOT NULL DEFAULT '',
	title        TEXT NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL,
	updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chats_workspace ON chats (workspace_id, updated_at DESC);

-- Solo texto: lo que el usuario escribió y lo que contestó el modelo, en
-- markdown. El prompt de sistema NO se guarda: se reconstruye en cada turno
-- con el estado actual del workspace (checkpoints, código abierto).
CREATE TABLE IF NOT EXISTS chat_messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	chat_id    INTEGER NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
	role       TEXT NOT NULL CHECK (role IN ('user','assistant')),
	content    TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_messages_chat ON chat_messages (chat_id, id);
`,
	},
	{
		version: 3,
		desc:    "checkpoints avanzados (S3): agente y chat que crearon el checkpoint, y lo que se guarda de la papelera",
		sql: `
-- Qué agente (ai_providers.id) y qué chat crearon el checkpoint; session_id ya
-- existía. Son datos de la UI: la fuente de verdad sigue siendo el archivo, y
-- SyncCheckpoints no los toca.
ALTER TABLE checkpoints ADD COLUMN agent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE checkpoints ADD COLUMN chat_id INTEGER NOT NULL DEFAULT 0;

-- Al mandar un checkpoint a la papelera su fila del índice desaparece (el rescan
-- borra lo que ya no está en el disco). Acá queda lo que solo vive en la base
-- —rótulo, plegado, agente y sesión— para devolvérselo al restaurarlo.
CREATE TABLE IF NOT EXISTS checkpoint_trash (
	workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	trashed_name TEXT NOT NULL,
	label        TEXT NOT NULL DEFAULT '',
	collapsed    INTEGER NOT NULL DEFAULT 0,
	session_id   TEXT NOT NULL DEFAULT '',
	agent_id     TEXT NOT NULL DEFAULT '',
	chat_id      INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (workspace_id, trashed_name)
);
`,
	},
	{
		version: 4,
		desc:    "último checkpoint elegido de cada workspace (para volver a él al reabrir)",
		sql: `
-- Nombre del .scad que el usuario tenía elegido al cerrar. Es dato de la UI, no
-- del modelo: si el archivo ya no existe la interfaz lo ignora y abre el más
-- nuevo, así que no necesita FK ni limpieza al borrar o mandar a la papelera.
ALTER TABLE workspaces ADD COLUMN last_checkpoint TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 5,
		desc:    "servidores de modelos del chat (S12): model_servers, con las filas de chat de ai_providers, y el último modelo de cada workspace",
		sql: `
-- Los servidores de modelos del chat: Ollama, compatibles con OpenAI (OpenRouter,
-- Gemini, LM Studio…) y Anthropic, varios por tipo. La clave de API NUNCA va en
-- claro: encrypted_token + token_nonce son AES-256-GCM con una clave de datos que
-- vive en el llavero del sistema (backend/secrets). api_key_env es la alternativa:
-- el nombre de la variable de entorno que la tiene.
CREATE TABLE IF NOT EXISTS model_servers (
	id              TEXT PRIMARY KEY,
	kind            TEXT NOT NULL CHECK (kind IN ('ollama','openai','anthropic')),
	preset          TEXT NOT NULL DEFAULT '',
	name            TEXT NOT NULL,
	base_url        TEXT NOT NULL DEFAULT '',
	api_key_env     TEXT NOT NULL DEFAULT '',
	encrypted_token BLOB,
	token_nonce     BLOB,
	default_model   TEXT NOT NULL DEFAULT '',
	context_tokens  INTEGER NOT NULL DEFAULT 0,
	enabled         INTEGER NOT NULL DEFAULT 1,
	sort_order      INTEGER NOT NULL DEFAULT 0,
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL
);

-- Se copian las filas de chat de ai_providers CON EL MISMO ID: chats.provider_id
-- de los chats ya guardados ('ollama', 'openai', 'anthropic') sigue resolviendo.
-- ai_providers no se toca (los CLIs siguen ahí). json_valid evita que una config
-- corrupta —que decodeConfig toleraba— tumbe la migración entera.
INSERT OR IGNORE INTO model_servers
	(id, kind, preset, name, base_url, api_key_env, default_model, context_tokens, enabled, sort_order, created_at, updated_at)
SELECT
	id, kind, '', name,
	CASE
		WHEN json_valid(config) AND COALESCE(json_extract(config, '$.baseUrl'), '') <> '' THEN json_extract(config, '$.baseUrl')
		WHEN kind = 'ollama' THEN 'http://localhost:11434'
		WHEN kind = 'anthropic' THEN 'https://api.anthropic.com'
		ELSE ''
	END,
	CASE WHEN json_valid(config) THEN COALESCE(json_extract(config, '$.apiKeyEnv'), '') ELSE '' END,
	CASE WHEN json_valid(config) THEN COALESCE(json_extract(config, '$.model'), '') ELSE '' END,
	CASE WHEN json_valid(config) AND kind = 'ollama' THEN COALESCE(CAST(json_extract(config, '$.numCtx') AS INTEGER), 0) ELSE 0 END,
	enabled, sort_order, updated_at, updated_at
FROM ai_providers
WHERE kind IN ('ollama','openai','anthropic');

-- Último servidor y modelo elegidos en el chat de cada proyecto: JSON
-- {"serverId","model"} o vacío. La elección global vive en settings ('lastModel').
-- Dato de la interfaz: sin FK ni limpieza al borrar un servidor.
ALTER TABLE workspaces ADD COLUMN last_model TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 6,
		desc:    "Hermes (S12): partes, tokens y costo de cada mensaje del chat",
		sql: `
-- parts: JSON con lo que no cabe en el texto. En un mensaje del usuario, los
-- metadatos de sus adjuntos; en uno del asistente, los pasos de herramienta del
-- turno. role sigue siendo 'user' | 'assistant' (el CHECK de la migración 2 no
-- se puede cambiar de forma aditiva): los pasos viajan dentro de parts.
ALTER TABLE chat_messages ADD COLUMN parts TEXT NOT NULL DEFAULT '';
-- Tokens y costo del turno que cerró este mensaje (0 = el servidor no lo dijo).
ALTER TABLE chat_messages ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE chat_messages ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE chat_messages ADD COLUMN cost_usd REAL NOT NULL DEFAULT 0;
`,
	},
	{
		version: 7,
		desc:    "llavero propio (S12): vault_meta con el verificador de la contraseña maestra",
		sql: `
-- La contraseña maestra NUNCA se guarda, ni su llave. Esta fila guarda solo un
-- texto conocido cifrado con la llave derivada (argon2id): si se abre, la
-- contraseña es la buena. El salt vive en <datos>/vault.salt, aparte de la base.
-- remember = 1 dice que la llave derivada se guardó en el llavero del sistema
-- («Recordar en este equipo»); la llave en sí nunca está acá. Una sola fila.
CREATE TABLE IF NOT EXISTS vault_meta (
	id             INTEGER PRIMARY KEY CHECK (id = 1),
	verifier       BLOB NOT NULL,
	verifier_nonce BLOB NOT NULL,
	remember       INTEGER NOT NULL DEFAULT 0,
	created_at     INTEGER NOT NULL
);
`,
	},
	{
		version: 8,
		desc:    "CLIs como proveedores del chat (S12.b): la sesión del CLI de cada chat",
		sql: `
-- Un chat con un CLI agéntico (provider_id = 'cli:claude', 'cli:codex'…) continúa
-- con el id de conversación que el propio CLI devolvió (--resume, resume <id>…):
-- el historial lo guarda el CLI y acá solo queda el puntero. Vacío = todavía no
-- hay sesión (un chat nuevo, o uno que cambió de CLI). Los chats con un servidor
-- de modelos no lo usan.
ALTER TABLE chats ADD COLUMN cli_session TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 9,
		desc:    "modo de escritura por conversación (S12.c): chats.mode",
		sql: `
-- El modo del chat vive en la conversación, no en una preferencia global:
-- '' = «Agente» (el predeterminado: ScadLab existe para crear y modificar
-- modelos) y 'ask' = «Solo consulta», elegido a mano para ESA conversación.
-- Toda fila anterior queda en '' (Agente): ninguna conversación conserva un
-- «Solo consulta» que no eligió nadie. Una conversación nueva también nace ''.
ALTER TABLE chats ADD COLUMN mode TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 10,
		desc:    "configuración por workspace (S14): skills activas, ajustes de impresión y colores existentes",
		sql: `
-- Skills activas del workspace: JSON ["nombre", …]. '' = «sin definir»: se
-- resuelve por defecto en tiempo de ejecución (las encendidas en Configuración
-- que no son de marca + la de la marca de su impresora). Un workspace nuevo ya
-- nace con la lista explícita.
ALTER TABLE workspaces ADD COLUMN skills TEXT NOT NULL DEFAULT '';
-- Ajustes de impresión propios del workspace: JSON WorkspacePrint (colores,
-- material, relleno, perímetros, ancho de línea, diámetro de filamento). '' = nada
-- fijado: todo se hereda de Configuración → Impresión y de la impresora.
ALTER TABLE workspaces ADD COLUMN print_settings TEXT NOT NULL DEFAULT '';
-- Backfill SOLO de la columna nueva: cada workspace existente queda con la lista
-- de skills que estaban encendidas en este momento (lo que ya tenía instalado), así
-- que cambiar Configuración → Skills después no lo altera. Si la tabla skills está
-- vacía (una base que nunca la pobló) no hay nada que congelar y queda ''. No se
-- reescribe ninguna otra columna.
UPDATE workspaces SET skills = (
	SELECT json_group_array(name) FROM (SELECT name FROM skills WHERE enabled = 1 ORDER BY name)
) WHERE EXISTS (SELECT 1 FROM skills);
-- Backfill de los colores: antes de S14 la cantidad de colores de un workspace era
-- el multicolor_slots de su impresora; ahora el valor de fábrica es default_colors
-- (4 en una Bambu de la serie A, 1 en una Prusa MK4S…). Sin esto una P1S o X1C ya
-- guardada pasaría de 16 a 4 colores. Se congela lo que tenía cada una (los
-- números son los del catálogo anterior), solo donde el workspace no fijó nada, y
-- un id que no está en la lista no se toca (queda con el valor de fábrica de su
-- impresora). Los workspaces nuevos siguen naciendo con default_colors.
UPDATE workspaces SET print_settings = CASE printer_id
	WHEN 'bambu-a1-mini' THEN '{"colors":4}'
	WHEN 'bambu-a1'      THEN '{"colors":4}'
	WHEN 'bambu-p1p'     THEN '{"colors":16}'
	WHEN 'bambu-p1s'     THEN '{"colors":16}'
	WHEN 'bambu-p2s'     THEN '{"colors":16}'
	WHEN 'bambu-x1c'     THEN '{"colors":16}'
	WHEN 'bambu-x1e'     THEN '{"colors":16}'
	WHEN 'bambu-h2d'     THEN '{"colors":24}'
	WHEN 'bambu-h2s'     THEN '{"colors":24}'
END
WHERE print_settings = '' AND printer_id IN ('bambu-a1-mini','bambu-a1','bambu-p1p','bambu-p1s','bambu-p2s','bambu-x1c','bambu-x1e','bambu-h2d','bambu-h2s');
`,
	},
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		desc       TEXT NOT NULL,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return err
	}
	applied := map[int]bool{}
	rows, err := s.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := s.apply(m); err != nil {
			return i18n.Errorf(i18n.Msg{ES: "migración %d (%s): %w", EN: "migration %d (%s): %w"}, m.version, m.desc, err)
		}
	}
	return nil
}

func (s *Store) apply(m migration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op tras Commit
	if _, err := tx.Exec(m.sql); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version, desc, applied_at) VALUES (?, ?, ?)`, m.version, m.desc, now()); err != nil {
		return err
	}
	return tx.Commit()
}

// SchemaVersion es la última migración aplicada (para Configuración → Acerca de).
func (s *Store) SchemaVersion() (int, error) {
	var v sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64), err
}
