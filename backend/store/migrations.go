package store

import (
	"database/sql"
	"fmt"
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
			return fmt.Errorf("migración %d (%s): %w", m.version, m.desc, err)
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
