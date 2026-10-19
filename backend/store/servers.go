package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"scadlab/backend/i18n"
)

// Servidores de modelos del chat (S12): Ollama, cualquier servidor compatible
// con la API de OpenAI y la API de Anthropic. Reemplazan a las tres filas de
// chat de ai_providers (que siguen ahí, sin uso, para los CLIs y por si hay que
// volver atrás): la migración las copia con el MISMO id para que
// chats.provider_id de los chats viejos siga resolviendo.
//
// La clave de API vive cifrada (encrypted_token + token_nonce, AES-256-GCM con
// la clave de datos del llavero del sistema, ver backend/secrets). Este paquete
// no cifra ni descifra: recibe y devuelve bytes opacos. Nunca está en
// ModelServer, que es lo que cruza hacia el frontend: solo HasToken.

// Tipos de servidor (coinciden con backend/llm).
const (
	ServerOllama    = "ollama"
	ServerOpenAI    = "openai"
	ServerAnthropic = "anthropic"
)

// ModelServer es un servidor de modelos tal como lo ve la interfaz.
type ModelServer struct {
	// ID es "ollama", "openai" o "anthropic" (migrados de ai_providers) o
	// "srv-<hex>" (los creados desde S12).
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Preset es la clave del atajo de la interfaz con el que se creó
	// («gemini», «openrouter»…). Es informativo: el comportamiento sale de Kind
	// y BaseURL.
	Preset  string `json:"preset"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	// APIKeyEnv es la alternativa a la clave guardada: el NOMBRE de la variable
	// de entorno que la tiene. Si hay clave guardada, gana la guardada.
	APIKeyEnv string `json:"apiKeyEnv"`
	// HasToken dice si hay una clave guardada. Nunca la clave.
	HasToken     bool   `json:"hasToken"`
	DefaultModel string `json:"defaultModel"`
	// ContextTokens es el num_ctx que se le pide a Ollama (0 = el default de
	// ScadLab, 16384). Los demás tipos lo ignoran.
	ContextTokens int   `json:"contextTokens"`
	Enabled       bool  `json:"enabled"`
	SortOrder     int   `json:"sortOrder"`
	UpdatedAt     int64 `json:"updatedAt"`
}

// MaxContextTokens es el tope que se acepta guardar: ningún modelo local pasa
// de unos pocos millones y un valor mayor es un error de tipeo.
const MaxContextTokens = 4_194_304

var (
	// ErrServerNotFound: el servidor pedido no existe.
	ErrServerNotFound = i18n.New(i18n.Msg{ES: "el servidor de modelos no existe", EN: "the model server does not exist"})
	// ErrServerName: falta el nombre.
	ErrServerName = i18n.New(i18n.Msg{ES: "el servidor necesita un nombre", EN: "the server needs a name"})
)

// TokenWrite dice qué hacer con la clave guardada al escribir un servidor.
// El valor cero la conserva.
type TokenWrite struct {
	// Set reemplaza la clave por Cipher/Nonce (ya cifrados).
	Set    bool
	Cipher []byte
	Nonce  []byte
	// Clear la borra. Set gana si vienen las dos.
	Clear bool
}

const serverCols = `id, kind, preset, name, base_url, api_key_env, encrypted_token IS NOT NULL, default_model, context_tokens, enabled, sort_order, updated_at`

func scanServer(sc interface{ Scan(...any) error }) (ModelServer, error) {
	var m ModelServer
	err := sc.Scan(&m.ID, &m.Kind, &m.Preset, &m.Name, &m.BaseURL, &m.APIKeyEnv, &m.HasToken, &m.DefaultModel, &m.ContextTokens, &m.Enabled, &m.SortOrder, &m.UpdatedAt)
	return m, err
}

// ListModelServers devuelve los servidores en su orden (sort_order, nombre).
func (s *Store) ListModelServers() ([]ModelServer, error) {
	rows, err := s.db.Query(`SELECT ` + serverCols + ` FROM model_servers ORDER BY sort_order, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModelServer{}
	for rows.Next() {
		m, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetModelServer lee uno por id; ErrServerNotFound si no existe.
func (s *Store) GetModelServer(id string) (ModelServer, error) {
	m, err := scanServer(s.db.QueryRow(`SELECT `+serverCols+` FROM model_servers WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ModelServer{}, ErrServerNotFound
	}
	return m, err
}

// ValidServerKind dice si kind es uno de los tres tipos.
func ValidServerKind(kind string) bool {
	return kind == ServerOllama || kind == ServerOpenAI || kind == ServerAnthropic
}

// NormalizeServerContext acota la ventana de contexto: solo Ollama la admite
// (en los demás se guarda 0, para que no quede un valor viejo de cuando el
// servidor era de otro tipo) y un valor negativo o absurdo se acota en vez de
// rechazarse.
func NormalizeServerContext(kind string, n int) int {
	if kind != ServerOllama || n < 0 {
		return 0
	}
	return min(n, MaxContextTokens)
}

// SaveModelServer crea (id vacío) o actualiza un servidor y devuelve cómo
// quedó. Valida lo que es de la base: nombre no vacío y único, tipo conocido.
// La URL y el nombre de la variable de entorno los valida quien la recibe de la
// interfaz (app_servers.go).
//
// En un alta, Enabled y los demás campos se guardan tal cual y el servidor va
// al final de la lista (un proveedor nuevo no se cuela delante de los que el
// usuario ya ordenó). En una edición no se toca SortOrder.
func (s *Store) SaveModelServer(in ModelServer, tok TokenWrite) (ModelServer, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return ModelServer{}, ErrServerName
	}
	if !ValidServerKind(in.Kind) {
		return ModelServer{}, i18n.Errorf(i18n.Msg{ES: "tipo de servidor desconocido %q (usa ollama, openai o anthropic)", EN: "unknown server kind %q (use ollama, openai or anthropic)"}, in.Kind)
	}
	in.ContextTokens = NormalizeServerContext(in.Kind, in.ContextTokens)
	in.DefaultModel = strings.TrimSpace(in.DefaultModel)
	in.APIKeyEnv = strings.TrimSpace(in.APIKeyEnv)
	in.Preset = strings.TrimSpace(in.Preset)

	var taken string
	switch err := s.db.QueryRow(`SELECT id FROM model_servers WHERE name = ? COLLATE NOCASE AND id <> ?`, in.Name, in.ID).Scan(&taken); {
	case err == nil:
		return ModelServer{}, i18n.Errorf(i18n.Msg{ES: "ya hay un servidor de modelos llamado %q", EN: "there is already a model server named %q"}, in.Name)
	case !errors.Is(err, sql.ErrNoRows):
		return ModelServer{}, err
	}

	t := now()
	if in.ID == "" {
		id, err := newServerID()
		if err != nil {
			return ModelServer{}, err
		}
		var next int
		if err := s.db.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) + 1 FROM model_servers`).Scan(&next); err != nil {
			return ModelServer{}, err
		}
		var cipher, nonce []byte
		if tok.Set {
			cipher, nonce = tok.Cipher, tok.Nonce
		}
		if _, err := s.db.Exec(`INSERT INTO model_servers
			(id, kind, preset, name, base_url, api_key_env, encrypted_token, token_nonce, default_model, context_tokens, enabled, sort_order, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, in.Kind, in.Preset, in.Name, in.BaseURL, in.APIKeyEnv, cipher, nonce, in.DefaultModel, in.ContextTokens, in.Enabled, next, t, t); err != nil {
			return ModelServer{}, err
		}
		return s.GetModelServer(id)
	}

	set := []string{"kind = ?", "preset = ?", "name = ?", "base_url = ?", "api_key_env = ?", "default_model = ?", "context_tokens = ?", "enabled = ?", "updated_at = ?"}
	args := []any{in.Kind, in.Preset, in.Name, in.BaseURL, in.APIKeyEnv, in.DefaultModel, in.ContextTokens, in.Enabled, t}
	switch {
	case tok.Set:
		set = append(set, "encrypted_token = ?", "token_nonce = ?")
		args = append(args, tok.Cipher, tok.Nonce)
	case tok.Clear:
		set = append(set, "encrypted_token = NULL", "token_nonce = NULL")
	}
	args = append(args, in.ID)
	res, err := s.db.Exec(`UPDATE model_servers SET `+strings.Join(set, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return ModelServer{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ModelServer{}, ErrServerNotFound
	}
	return s.GetModelServer(in.ID)
}

// DeleteModelServer borra un servidor y su clave cifrada. Los chats que lo
// usaron conservan su provider_id (no hay FK a propósito) y siguen legibles.
// Borrar uno que no existe no es un error.
func (s *Store) DeleteModelServer(id string) error {
	_, err := s.db.Exec(`DELETE FROM model_servers WHERE id = ?`, id)
	return err
}

// ModelServerToken devuelve la clave cifrada y su nonce; ambos nil si no hay
// clave guardada (un servidor local sin autenticación es el caso normal).
// Existe aparte de GetModelServer para que el cifrado no viaje en ModelServer.
func (s *Store) ModelServerToken(id string) (cipher, nonce []byte, err error) {
	err = s.db.QueryRow(`SELECT encrypted_token, token_nonce FROM model_servers WHERE id = ?`, id).Scan(&cipher, &nonce)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrServerNotFound
	}
	return cipher, nonce, err
}

func newServerID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "srv-" + hex.EncodeToString(b), nil
}

// LastModel es el último servidor y modelo que se eligieron en el chat.
type LastModel struct {
	ServerID string `json:"serverId"`
	Model    string `json:"model"`
}

// lastModelKey es el ajuste global; el de cada proyecto vive en
// workspaces.last_model (TEXT con el mismo JSON).
const lastModelKey = "lastModel"

// SetLastModel recuerda la elección para el proyecto y, a la vez, como la
// global (la de un proyecto sin elección propia todavía). Un workspace que no
// existe es ErrNotFound; con workspaceID 0 solo se guarda la global.
func (s *Store) SetLastModel(workspaceID int64, lm LastModel) error {
	raw, err := json.Marshal(lm)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op tras Commit
	if workspaceID != 0 {
		res, err := tx.Exec(`UPDATE workspaces SET last_model = ? WHERE id = ?`, string(raw), workspaceID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
	}
	if _, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, lastModelKey, string(raw), now()); err != nil {
		return err
	}
	return tx.Commit()
}

// GetLastModel devuelve la elección del proyecto; si no tiene (o es ilegible),
// la global; si tampoco, el valor vacío.
func (s *Store) GetLastModel(workspaceID int64) (LastModel, error) {
	if workspaceID != 0 {
		var raw string
		err := s.db.QueryRow(`SELECT last_model FROM workspaces WHERE id = ?`, workspaceID).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return LastModel{}, err
		}
		if lm, ok := decodeLastModel(raw); ok {
			return lm, nil
		}
	}
	var raw string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, lastModelKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return LastModel{}, nil
	}
	if err != nil {
		return LastModel{}, err
	}
	lm, _ := decodeLastModel(raw)
	return lm, nil
}

// decodeLastModel tolera un valor corrupto o vacío: es una conveniencia de la
// interfaz, no un dato que valga romper el chat.
func decodeLastModel(raw string) (LastModel, bool) {
	var lm LastModel
	if raw == "" || json.Unmarshal([]byte(raw), &lm) != nil || lm.ServerID == "" {
		return LastModel{}, false
	}
	return lm, true
}
