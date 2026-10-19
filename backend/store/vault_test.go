package store

import (
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// La migración 7 es aditiva: una base de la versión 6 con un servidor y una clave
// cifrada sube a la 7 sin perder nada, crea vault_meta vacía, y abrirla otra vez
// no hace nada.
func TestMigration7AddsVaultMetaKeepingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, desc TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version > 6 {
			break
		}
		if err := s.apply(m); err != nil {
			t.Fatalf("migración %d: %v", m.version, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO model_servers (id, kind, name, base_url, encrypted_token, token_nonce, created_at, updated_at)
		VALUES ('srv-viejo', 'openai', 'Viejo', 'http://x/v1', x'0102', x'0304', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	for i := 0; i < 2; i++ {
		st, err := Open(path)
		if err != nil {
			t.Fatalf("apertura %d: %v", i, err)
		}
		if v, _ := st.SchemaVersion(); v != 7 {
			t.Fatalf("versión %d, esperaba 7", v)
		}
		if _, ok, err := st.GetVaultMeta(); ok || err != nil {
			t.Fatalf("vault_meta debe nacer vacía: %v %v", ok, err)
		}
		c, n, err := st.ModelServerToken("srv-viejo")
		if err != nil || !bytes.Equal(c, []byte{1, 2}) || !bytes.Equal(n, []byte{3, 4}) {
			t.Fatalf("la clave cifrada cambió: %x %x %v", c, n, err)
		}
		st.Close()
	}
}

func TestWriteVaultReencryptsAndRollsBackOnError(t *testing.T) {
	s := openTemp(t)
	mk := func(name string, c byte) string {
		m, err := s.SaveModelServer(ModelServer{Kind: ServerOpenAI, Name: name, BaseURL: "http://x/v1"}, TokenWrite{Set: true, Cipher: []byte{c}, Nonce: []byte{c}})
		if err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	a, b := mk("A", 1), mk("B", 2)

	// Un error a la mitad no cambia nada.
	calls := 0
	err := s.WriteVault(VaultMeta{Verifier: []byte("v"), Nonce: []byte("n")}, func(c, n []byte) ([]byte, []byte, error) {
		calls++
		if calls == 2 {
			return nil, nil, errors.New("falla")
		}
		return []byte{9}, []byte{9}, nil
	})
	if err == nil {
		t.Fatal("debía fallar")
	}
	if _, ok, _ := s.GetVaultMeta(); ok {
		t.Fatal("no debe quedar verificador")
	}
	for _, id := range []string{a, b} {
		if c, _, _ := s.ModelServerToken(id); len(c) != 1 || c[0] == 9 {
			t.Fatalf("%s quedó re-cifrada a medias: %x", id, c)
		}
	}

	// Éxito: re-cifra y borra la que devuelve (nil, nil, nil).
	err = s.WriteVault(VaultMeta{Verifier: []byte("v"), Nonce: []byte("n"), Remember: true}, func(c, n []byte) ([]byte, []byte, error) {
		if c[0] == 2 {
			return nil, nil, nil
		}
		return []byte{7}, []byte{7}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok, _ := s.GetVaultMeta()
	if !ok || !m.Remember || string(m.Verifier) != "v" || m.CreatedAt == 0 {
		t.Fatalf("meta: %+v", m)
	}
	if c, _, _ := s.ModelServerToken(a); len(c) != 1 || c[0] != 7 {
		t.Fatalf("A: %x", c)
	}
	if got, _ := s.GetModelServer(b); got.HasToken {
		t.Fatal("B debía quedar sin clave")
	}

	if err := s.SetVaultRemember(false); err != nil {
		t.Fatal(err)
	}
	if m, _, _ := s.GetVaultMeta(); m.Remember {
		t.Fatal("SetVaultRemember(false)")
	}
	created := m.CreatedAt
	// Cambiar la contraseña conserva created_at.
	if err := s.WriteVault(VaultMeta{Verifier: []byte("v2"), Nonce: []byte("n2")}, nil); err != nil {
		t.Fatal(err)
	}
	if m, _, _ := s.GetVaultMeta(); string(m.Verifier) != "v2" || m.CreatedAt != created {
		t.Fatalf("meta tras el cambio: %+v", m)
	}

	n, err := s.ResetVault()
	if err != nil || n != 1 {
		t.Fatalf("ResetVault: %d, %v", n, err)
	}
	if _, ok, _ := s.GetVaultMeta(); ok {
		t.Fatal("queda el verificador")
	}
	if cnt, _ := s.CountModelServerTokens(); cnt != 0 {
		t.Fatalf("quedan %d claves", cnt)
	}
	if list, _ := s.ListModelServers(); len(list) < 2 {
		t.Fatal("los servidores deben quedar, sin clave")
	}
}
