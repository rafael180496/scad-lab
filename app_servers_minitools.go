package main

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/store"
)

// Importar los servidores de modelos de mini-tools (S12.b). Solo cuando el
// usuario lo pide y lo confirma (nunca al arrancar), y SIN las claves.
//
// mini-tools guarda sus servidores en `vault.db` (SQLite). Se abre **en solo
// lectura** (`mode=ro&immutable=1`: ni siquiera crea los archivos -wal / -shm) y
// se leen únicamente `name`, `kind`, `base_url`, `default_model` y, si existe,
// `context_tokens`. NUNCA se nombra una columna cifrada (`encrypted_token`,
// `encrypted_headers`, sus nonces…), ni se abre `salt.bin` ni el llavero de
// mini-tools: las claves se vuelven a escribir en ScadLab, que las cifra con la
// suya. Los servidores nacen sin clave.
//
// Ni en desarrollo ni en pruebas se lee el directorio real de mini-tools:
// SCADLAB_MINITOOLS_DIR lo reemplaza y las pruebas usan siempre una copia falsa.

// MiniToolsDirEnv reemplaza la carpeta de mini-tools (la que contiene vault.db).
const MiniToolsDirEnv = "SCADLAB_MINITOOLS_DIR"

// MiniToolsServer es un servidor del vault de mini-tools tal como se importaría.
type MiniToolsServer struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	BaseURL       string `json:"baseUrl"`
	DefaultModel  string `json:"defaultModel"`
	ContextTokens int    `json:"contextTokens,omitempty"`
	// Exists: ya hay un servidor con ese nombre o esa URL en ScadLab; no se duplica.
	Exists bool `json:"exists"`
	// Error dice por qué no se puede importar (tipo desconocido, URL inválida).
	Error string `json:"error,omitempty"`
}

// MiniToolsImport es lo que encontró (o importó) ImportMiniToolsServers.
type MiniToolsImport struct {
	// Found: existe el vault de mini-tools en este equipo.
	Found bool `json:"found"`
	// Path es el vault.db buscado, para decirlo en pantalla aunque no exista.
	Path     string            `json:"path"`
	Servers  []MiniToolsServer `json:"servers"`
	Imported int               `json:"imported"`
}

// ImportMiniToolsServers lista los servidores de mini-tools y, con dryRun en
// false, crea en ScadLab los que todavía no están (por nombre o por URL), sin
// clave. Con dryRun en true no escribe nada: es la vista previa que se confirma.
// Que mini-tools no esté instalado no es un error (Found=false).
func (a *App) ImportMiniToolsServers(dryRun bool) (MiniToolsImport, error) {
	if err := a.ready(); err != nil {
		return MiniToolsImport{}, err
	}
	path, err := miniToolsVaultPath()
	out := MiniToolsImport{Path: path, Servers: []MiniToolsServer{}}
	if err != nil {
		return out, err
	}
	if st, serr := os.Stat(path); serr != nil || st.IsDir() {
		return out, nil
	}
	out.Found = true
	found, err := readMiniToolsServers(path)
	if err != nil {
		return out, err
	}

	existing, err := a.store.ListModelServers()
	if err != nil {
		return out, err
	}
	names, urls := map[string]bool{}, map[string]bool{}
	for _, s := range existing {
		names[nameKey(s.Name)] = true
		urls[urlKey(s.Kind, s.BaseURL)] = true
	}
	for _, m := range found {
		if !store.ValidServerKind(m.Kind) {
			m.Error = i18n.T(i18n.Msg{ES: "tipo de servidor no admitido (%s)", EN: "unsupported server type (%s)"}, m.Kind)
		} else if names[nameKey(m.Name)] || urls[urlKey(m.Kind, m.BaseURL)] {
			m.Exists = true
		}
		if !dryRun && !m.Exists && m.Error == "" {
			saved, serr := a.SaveModelServer(store.ModelServer{
				Kind: m.Kind, Name: m.Name, BaseURL: m.BaseURL, DefaultModel: m.DefaultModel,
				ContextTokens: m.ContextTokens, Enabled: true,
			}, "", false)
			if serr != nil {
				m.Error = serr.Error()
			} else {
				out.Imported++
				// Dos entradas del vault con el mismo nombre o URL: la segunda ya existe.
				names[nameKey(saved.Name)] = true
				urls[urlKey(saved.Kind, saved.BaseURL)] = true
			}
		}
		out.Servers = append(out.Servers, m)
	}
	return out, nil
}

func nameKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// urlKey normaliza la URL como lo hace el alta de un servidor, para que
// «http://x/v1/» y «http://x/v1» sean el mismo.
func urlKey(kind, raw string) string {
	u := llm.NormalizeBaseURLFor(kind, raw)
	if norm, err := llm.NormalizeBaseURL(u); err == nil {
		u = norm
	}
	return strings.ToLower(strings.TrimSpace(u))
}

// miniToolsVaultPath es <config del SO>/mini-tools/vault.db, o el que diga
// SCADLAB_MINITOOLS_DIR.
func miniToolsVaultPath() (string, error) {
	dir := strings.TrimSpace(os.Getenv(MiniToolsDirEnv))
	if dir == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return "", i18n.Errorf(i18n.Msg{ES: "no se pudo ubicar la carpeta de configuración: %v", EN: "could not locate the configuration folder: %v"}, err)
		}
		dir = filepath.Join(cfg, "mini-tools")
	}
	return filepath.Join(dir, "vault.db"), nil
}

// miniToolsDSN abre el vault de solo lectura e inmutable. La ruta va como URI
// con sus caracteres escapados («Application Support» lleva un espacio).
func miniToolsDSN(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	p := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && !strings.HasPrefix(p, "/") {
		p = "/" + p // file:///C:/Users/…
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro&immutable=1"}
	return u.String()
}

// miniToolsColumns son las ÚNICAS columnas que se leen de model_servers. name,
// kind y base_url existen desde la primera versión de la tabla; las otras dos se
// agregaron después y pueden faltar en un vault viejo.
var (
	miniToolsRequired = []string{"name", "kind", "base_url"}
	miniToolsOptional = []string{"default_model", "context_tokens"}
)

// miniToolsQuery arma el SELECT con las columnas permitidas que la tabla tiene.
// Es una función aparte para que una prueba compruebe que NO nombra ninguna
// columna cifrada.
func miniToolsQuery(have map[string]bool) string {
	cols := append([]string(nil), miniToolsRequired...)
	for _, c := range miniToolsOptional {
		if have[c] {
			cols = append(cols, c)
		} else if c == "context_tokens" {
			cols = append(cols, "0 AS context_tokens")
		} else {
			cols = append(cols, "'' AS "+c)
		}
	}
	return "SELECT " + strings.Join(cols, ", ") + " FROM model_servers ORDER BY name COLLATE NOCASE"
}

// readMiniToolsServers lee los servidores del vault.
func readMiniToolsServers(path string) ([]MiniToolsServer, error) {
	db, err := sql.Open("sqlite", miniToolsDSN(path))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// Solo los NOMBRES de las columnas de la tabla (el esquema, no sus datos).
	rows, err := db.Query(`SELECT name FROM pragma_table_info('model_servers')`)
	if err != nil {
		return nil, miniToolsReadError(err)
	}
	have := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		have[n] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, c := range miniToolsRequired {
		if !have[c] {
			return nil, i18n.New(i18n.Msg{ES: "el vault de mini-tools no tiene la tabla de servidores de modelos (¿es de una versión muy vieja?)", EN: "the mini-tools vault has no model servers table (is it a very old version?)"})
		}
	}

	data, err := db.Query(miniToolsQuery(have))
	if err != nil {
		return nil, miniToolsReadError(err)
	}
	defer data.Close()
	var out []MiniToolsServer
	for data.Next() {
		var m MiniToolsServer
		var defModel sql.NullString
		var ctx sql.NullInt64
		if err := data.Scan(&m.Name, &m.Kind, &m.BaseURL, &defModel, &ctx); err != nil {
			return nil, err
		}
		m.Name, m.Kind, m.BaseURL = strings.TrimSpace(m.Name), strings.TrimSpace(m.Kind), strings.TrimSpace(m.BaseURL)
		m.DefaultModel = strings.TrimSpace(defModel.String)
		m.ContextTokens = store.NormalizeServerContext(m.Kind, int(ctx.Int64))
		if m.Name == "" {
			continue
		}
		out = append(out, m)
	}
	return out, data.Err()
}

func miniToolsReadError(err error) error {
	return i18n.Errorf(i18n.Msg{
		ES: "no se pudo leer el vault de mini-tools (cierra mini-tools si está abierto e inténtalo de nuevo): %v",
		EN: "could not read the mini-tools vault (close mini-tools if it is open and try again): %v",
	}, err)
}
