// Package store es la base SQLite local de ScadLab (scadlab.db).
//
// Guarda lo que es de la aplicación —workspaces registrados, el índice de
// checkpoints, configuración, skills, proveedores de IA, materiales—. Los .scad
// en sí viven en disco dentro de cada workspace: la base indexa, no es la
// fuente de verdad del modelo. Si la base se pierde, un rescan del workspace
// reconstruye el árbol de checkpoints desde los encabezados `// @checkpoint`.
//
// modernc.org/sqlite es Go puro: sin CGO, así que Windows se cross-compila
// desde macOS sin toolchain de C (ver claude/03-backend.md).
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store envuelve la conexión. Es seguro para uso concurrente (database/sql).
type Store struct {
	db *sql.DB
}

// Open abre (o crea) la base y aplica las migraciones pendientes.
func Open(path string) (*Store, error) {
	// WAL: lecturas del frontend mientras un rescan escribe. busy_timeout
	// evita SQLITE_BUSY cuando dos bindings escriben a la vez.
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite admite un solo escritor; una conexión evita peleas por el lock.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close cierra la base.
func (s *Store) Close() error { return s.db.Close() }

func now() int64 { return time.Now().Unix() }
