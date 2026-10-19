package vaultgate

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"

	"scadlab/backend/crypto"
	"scadlab/backend/i18n"
	"scadlab/backend/store"
)

const pw = "una-contraseña-larga-1"

// countBackend cuenta cuántas veces se LEE cada cuenta del llavero: el contrato
// es una lectura de la llave por sesión, no una por llamada.
type countBackend struct {
	Backend
	mu   sync.Mutex
	gets map[string]int
}

func newCount() *countBackend { return &countBackend{Backend: OSBackend(), gets: map[string]int{}} }

func (c *countBackend) Get(service, account string) (string, error) {
	c.mu.Lock()
	c.gets[account]++
	c.mu.Unlock()
	return c.Backend.Get(service, account)
}

func (c *countBackend) reads(account string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gets[account]
}

// env arma una instalación de prueba: carpeta de datos temporal, base real y el
// llavero SIMULADO (nunca el del sistema).
type env struct {
	t   *testing.T
	dir string
	db  *store.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	return &env{t: t, dir: dir, db: openDB(t, dir)}
}

func openDB(t *testing.T, dir string) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(dir, "scadlab.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// restart simula cerrar y volver a abrir la app: base nueva sobre los mismos
// archivos y un Vault nuevo (sin nada en memoria).
func (e *env) restart(kb Backend) *Vault {
	e.t.Helper()
	e.db.Close()
	e.db = openDB(e.t, e.dir)
	return New(e.dir, e.db, kb)
}

func (e *env) vault(kb Backend) *Vault { return New(e.dir, e.db, kb) }

func codeOf(err error) string { return i18n.Code(err) }

func remembered(t *testing.T) []byte {
	t.Helper()
	raw, err := keyring.Get(Service, MasterAccount)
	if err != nil {
		t.Fatalf("la llave no está en el llavero: %v", err)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != crypto.KeySize {
		t.Fatalf("llave recordada inválida: %d, %v", len(key), err)
	}
	return key
}

func TestFirstTimeCreateRemembersTheDerivedKeyNotThePassword(t *testing.T) {
	e := newEnv(t)
	v := e.vault(nil)

	if st, _ := v.Status(); st.Initialized || st.Unlocked || !st.KeyringAvailable {
		t.Fatalf("estado inicial: %+v", st)
	}
	if _, _, err := v.Encrypt("sk-x"); codeOf(err) != CodeUninitialized {
		t.Fatalf("sin contraseña maestra: %v", err)
	}

	res, err := v.Create(pw, true)
	if err != nil || res.RememberFailed {
		t.Fatalf("Create: %+v, %v", res, err)
	}
	st, _ := v.Status()
	if !st.Initialized || !st.Unlocked || !st.Remembered {
		t.Fatalf("tras crear: %+v", st)
	}
	// Lo que hay en el llavero es la llave derivada, no la contraseña.
	key := remembered(t)
	raw, _ := keyring.Get(Service, MasterAccount)
	if strings.Contains(raw, pw) {
		t.Fatal("la contraseña está en el llavero")
	}
	salt, err := LoadSalt(e.dir)
	if err != nil || !bytes.Equal(key, crypto.DeriveKey([]byte(pw), salt)) {
		t.Fatalf("la llave recordada no es la derivada de la contraseña y el salt: %v", err)
	}
	if info, err := os.Stat(SaltPath(e.dir)); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) || info.Size() != 16 {
		t.Fatalf("vault.salt: %v %v", info, err)
	}

	ct, nonce, err := v.Encrypt("sk-abc")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := v.Decrypt(ct, nonce); err != nil || got != "sk-abc" {
		t.Fatalf("round trip: %q, %v", got, err)
	}
	if _, err := v.Create(pw, true); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("crear dos veces: %v", err)
	}
}

func TestCreateRejectsAShortPasswordAndLeavesNothing(t *testing.T) {
	e := newEnv(t)
	if _, err := e.vault(nil).Create("corta", true); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("contraseña corta: %v", err)
	}
	if _, ok, _ := e.db.GetVaultMeta(); ok {
		t.Fatal("no debe quedar verificador")
	}
	if _, err := os.Stat(SaltPath(e.dir)); err == nil {
		t.Fatal("no debe quedar salt")
	}
}

// Reinicio: la app nueva no pregunta nada, lee el llavero UNA sola vez (perezoso,
// en el primer uso) y después sirve todo desde memoria.
func TestRestartAutoUnlocksLazilyAndReadsTheKeychainOnce(t *testing.T) {
	e := newEnv(t)
	v := e.vault(nil)
	if _, err := v.Create(pw, true); err != nil {
		t.Fatal(err)
	}
	ct, nonce, _ := v.Encrypt("sk-persistida")

	cb := newCount()
	v2 := e.restart(cb)
	st, _ := v2.Status()
	if !st.Initialized || st.Unlocked || !st.Remembered {
		t.Fatalf("recién arrancada: %+v", st)
	}
	if n := cb.reads(MasterAccount); n != 0 {
		t.Fatalf("arrancar o pedir el estado no debe leer la llave (%d lecturas)", n)
	}
	for i := 0; i < 25; i++ {
		got, err := v2.Decrypt(ct, nonce)
		if err != nil || got != "sk-persistida" {
			t.Fatalf("vuelta %d: %q, %v", i, got, err)
		}
		if _, _, err := v2.Encrypt("otra"); err != nil {
			t.Fatal(err)
		}
	}
	if n := cb.reads(MasterAccount); n != 1 {
		t.Fatalf("la llave se leyó %d veces del llavero, esperaba 1", n)
	}
	if st, _ = v2.Status(); !st.Unlocked {
		t.Fatalf("tras el primer uso debe estar desbloqueada: %+v", st)
	}
	// El sondeo de disponibilidad usa otra cuenta, y una sola vez.
	for i := 0; i < 5; i++ {
		_, _ = v2.Status()
	}
	if n := cb.reads(probeAccount); n != 1 {
		t.Fatalf("el sondeo se hizo %d veces", n)
	}
}

func TestConcurrentFirstUseReadsTheKeychainOnce(t *testing.T) {
	e := newEnv(t)
	v := e.vault(nil)
	if _, err := v.Create(pw, true); err != nil {
		t.Fatal(err)
	}
	ct, nonce, _ := v.Encrypt("sk-x")
	cb := newCount()
	v2 := e.restart(cb)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := v2.Decrypt(ct, nonce); err != nil || got != "sk-x" {
				t.Errorf("Decrypt: %q, %v", got, err)
			}
		}()
	}
	wg.Wait()
	if n := cb.reads(MasterAccount); n != 1 {
		t.Fatalf("%d lecturas en paralelo, esperaba 1", n)
	}
}

func TestWithoutRememberStaysLockedUntilUnlocked(t *testing.T) {
	e := newEnv(t)
	v := e.vault(nil)
	if _, err := v.Create(pw, false); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(Service, MasterAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("sin recordar no debe haber entrada: %v", err)
	}
	ct, nonce, _ := v.Encrypt("sk-1")

	cb := newCount()
	v2 := e.restart(cb)
	if _, err := v2.Decrypt(ct, nonce); codeOf(err) != CodeLocked || !errors.Is(err, ErrLocked) {
		t.Fatalf("debe pedir desbloquear: %v", err)
	}
	if n := cb.reads(MasterAccount); n != 0 {
		t.Fatalf("sin recordar no se lee el llavero (%d)", n)
	}
	if _, err := v2.Unlock("contraseña equivocada", false); codeOf(err) != CodeWrongPassword || !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("contraseña incorrecta: %v", err)
	}
	if _, err := v2.Decrypt(ct, nonce); codeOf(err) != CodeLocked {
		t.Fatalf("sigue bloqueada: %v", err)
	}
	if _, err := v2.Unlock(pw, false); err != nil {
		t.Fatal(err)
	}
	if got, err := v2.Decrypt(ct, nonce); err != nil || got != "sk-1" {
		t.Fatalf("tras desbloquear: %q, %v", got, err)
	}
}

func TestUnlockCanTurnRememberOn(t *testing.T) {
	e := newEnv(t)
	if _, err := e.vault(nil).Create(pw, false); err != nil {
		t.Fatal(err)
	}
	v2 := e.restart(nil)
	if res, err := v2.Unlock(pw, true); err != nil || res.RememberFailed {
		t.Fatalf("Unlock: %+v, %v", res, err)
	}
	remembered(t)
	if st, _ := v2.Status(); !st.Remembered {
		t.Fatalf("%+v", st)
	}
	// Y la próxima vez no pregunta.
	v3 := e.restart(nil)
	if _, _, err := v3.Encrypt("x"); err != nil {
		t.Fatalf("debía desbloquearse sola: %v", err)
	}
}

// Llavero denegado al crear: la contraseña se crea igual, no se recuerda nada y
// la app pide la contraseña una vez por sesión.
func TestDeniedKeychainOnCreateFallsBackToThePassword(t *testing.T) {
	e := newEnv(t)
	keyring.MockInitWithError(errors.New("denegado por el usuario"))
	v := e.vault(nil)
	res, err := v.Create(pw, true)
	if err != nil || !res.RememberFailed || res.Detail == "" {
		t.Fatalf("Create con el llavero denegado: %+v, %v", res, err)
	}
	if strings.Contains(res.Detail, pw) {
		t.Fatal("el detalle repite la contraseña")
	}
	st, _ := v.Status()
	if !st.Initialized || !st.Unlocked || st.Remembered || st.KeyringAvailable {
		t.Fatalf("estado: %+v", st)
	}
	if _, _, err := v.Encrypt("sk"); err != nil {
		t.Fatalf("esta sesión funciona igual: %v", err)
	}
	cb := newCount()
	v2 := e.restart(cb)
	if _, _, err := v2.Encrypt("sk"); codeOf(err) != CodeLocked {
		t.Fatalf("la sesión siguiente pide la contraseña: %v", err)
	}
	if n := cb.reads(MasterAccount); n != 0 {
		t.Fatalf("no se recordó nada, no hay qué leer (%d)", n)
	}
}

// El llavero que dijo que sí al crear y NIEGA después: se prueba una vez, se
// marca caído y no se vuelve a intentar (ni borra la llave buena); la contraseña
// desbloquea y la bandera de recordar queda como estaba.
func TestDeniedKeychainAtStartupFailsOnceWithoutLoop(t *testing.T) {
	e := newEnv(t)
	if _, err := e.vault(nil).Create(pw, true); err != nil {
		t.Fatal(err)
	}

	keyring.MockInitWithError(errors.New("acceso denegado"))
	cb := newCount()
	v2 := e.restart(cb)
	for i := 0; i < 10; i++ {
		if _, _, err := v2.Encrypt("x"); codeOf(err) != CodeLocked {
			t.Fatalf("vuelta %d: %v", i, err)
		}
	}
	if n := cb.reads(MasterAccount); n != 1 {
		t.Fatalf("se intentó leer %d veces, esperaba 1 (sin bucle)", n)
	}
	st, _ := v2.Status()
	if st.KeyringAvailable || st.KeyringDetail == "" || !st.Remembered {
		t.Fatalf("estado tras la negativa: %+v", st)
	}
	if res, err := v2.Unlock(pw, true); err != nil || res.RememberFailed {
		t.Fatalf("desbloquear con la contraseña: %+v, %v", res, err)
	}
	if _, _, err := v2.Encrypt("x"); err != nil {
		t.Fatal(err)
	}
	if st, _ = v2.Status(); !st.Remembered {
		t.Fatal("una negativa pasajera no debe apagar «recordar»")
	}
}

func TestForgetThisDeviceDeletesTheKeychainEntry(t *testing.T) {
	e := newEnv(t)
	v := e.vault(nil)
	if _, err := v.Create(pw, true); err != nil {
		t.Fatal(err)
	}
	ct, nonce, _ := v.Encrypt("sk-1")
	if err := v.Forget(); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(Service, MasterAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("la entrada sigue: %v", err)
	}
	if st, _ := v.Status(); st.Remembered || !st.Unlocked {
		t.Fatalf("la sesión actual sigue desbloqueada: %+v", st)
	}
	// Olvidar dos veces no es un error.
	if err := v.Forget(); err != nil {
		t.Fatal(err)
	}
	v2 := e.restart(nil)
	if _, err := v2.Decrypt(ct, nonce); codeOf(err) != CodeLocked {
		t.Fatalf("la sesión siguiente pide la contraseña: %v", err)
	}
	// Y se puede volver a recordar con la sesión desbloqueada.
	if _, err := v2.Unlock(pw, false); err != nil {
		t.Fatal(err)
	}
	if err := v2.SetRemember(true); err != nil {
		t.Fatal(err)
	}
	remembered(t)
	if err := v2.SetRemember(false); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(Service, MasterAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("SetRemember(false) debe borrar la entrada")
	}
	// Con la app bloqueada no se puede encender.
	if err := e.restart(nil).SetRemember(true); codeOf(err) != CodeLocked {
		t.Fatalf("recordar sin llave: %v", err)
	}
}

func saveToken(t *testing.T, st *store.Store, name string, ct, nonce []byte) store.ModelServer {
	t.Helper()
	s, err := st.SaveModelServer(store.ModelServer{Kind: store.ServerOpenAI, Name: name, BaseURL: "http://localhost:1/v1", Enabled: true},
		store.TokenWrite{Set: true, Cipher: ct, Nonce: nonce})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestChangePasswordReencryptsEveryKey(t *testing.T) {
	e := newEnv(t)
	v := e.vault(nil)
	if _, err := v.Create(pw, true); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i, plain := range []string{"sk-uno", "sk-dos", "sk-tres"} {
		ct, nonce, _ := v.Encrypt(plain)
		ids = append(ids, saveToken(t, e.db, []string{"A", "B", "C"}[i], ct, nonce).ID)
	}
	oldRemembered := remembered(t)

	if _, err := v.Change("no es la vieja", "otra-contraseña-2"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("contraseña vieja incorrecta: %v", err)
	}
	if _, err := v.Change(pw, "corta"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("contraseña nueva débil: %v", err)
	}
	if _, err := v.Change(pw, "otra-contraseña-2"); err != nil {
		t.Fatal(err)
	}
	// La llave recordada es la nueva.
	if bytes.Equal(remembered(t), oldRemembered) {
		t.Fatal("el llavero conserva la llave vieja")
	}

	// Reinicio: la llave recordada nueva abre las tres claves.
	v2 := e.restart(nil)
	for i, want := range []string{"sk-uno", "sk-dos", "sk-tres"} {
		ct, nonce, err := e.db.ModelServerToken(ids[i])
		if err != nil {
			t.Fatal(err)
		}
		if got, err := v2.Decrypt(ct, nonce); err != nil || got != want {
			t.Fatalf("clave %d: %q, %v", i, got, err)
		}
	}
	// Sin llavero, solo la contraseña nueva desbloquea.
	_ = keyring.Delete(Service, MasterAccount)
	v3 := e.restart(nil)
	if _, err := v3.Unlock(pw, false); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("la vieja no debe abrir: %v", err)
	}
	if _, err := v3.Unlock("otra-contraseña-2", false); err != nil {
		t.Fatal(err)
	}
}

// Si una clave no se puede abrir con la llave vieja, no se cambia NADA: ni el
// verificador ni las demás claves.
func TestChangePasswordIsAtomic(t *testing.T) {
	e := newEnv(t)
	v := e.vault(nil)
	if _, err := v.Create(pw, false); err != nil {
		t.Fatal(err)
	}
	ct, nonce, _ := v.Encrypt("sk-buena")
	good := saveToken(t, e.db, "Buena", ct, nonce)
	// Una clave cifrada con otra llave: no abre.
	bad, bn, _ := crypto.Encrypt(bytes.Repeat([]byte{9}, 32), []byte("sk-ajena"))
	saveToken(t, e.db, "Mala", bad, bn)

	if _, err := v.Change(pw, "otra-contraseña-2"); err == nil {
		t.Fatal("debe fallar")
	}
	if _, err := e.restart(nil).Unlock(pw, false); err != nil {
		t.Fatalf("la contraseña vieja debe seguir valiendo: %v", err)
	}
	c2, n2, _ := e.db.ModelServerToken(good.ID)
	if !bytes.Equal(c2, ct) || !bytes.Equal(n2, nonce) {
		t.Fatal("una clave quedó re-cifrada a medias")
	}
}

func TestResetClearsKeysVerifierSaltAndKeychain(t *testing.T) {
	e := newEnv(t)
	v := e.vault(nil)
	if _, err := v.Create(pw, true); err != nil {
		t.Fatal(err)
	}
	ct, nonce, _ := v.Encrypt("sk-1")
	s := saveToken(t, e.db, "Nube", ct, nonce)

	n, err := v.Reset()
	if err != nil || n != 1 {
		t.Fatalf("Reset: %d, %v", n, err)
	}
	if _, ok, _ := e.db.GetVaultMeta(); ok {
		t.Error("queda el verificador")
	}
	if _, err := os.Stat(SaltPath(e.dir)); err == nil {
		t.Error("queda el salt")
	}
	if _, err := keyring.Get(Service, MasterAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Error("queda la entrada del llavero")
	}
	got, _ := e.db.GetModelServer(s.ID)
	if got.HasToken {
		t.Error("queda la clave del servidor")
	}
	if st, _ := v.Status(); st.Initialized || st.Unlocked {
		t.Fatalf("estado: %+v", st)
	}
	if _, _, err := v.Encrypt("x"); codeOf(err) != CodeUninitialized {
		t.Fatalf("tras restablecer: %v", err)
	}
	// Se puede empezar de cero con otra contraseña.
	if _, err := v.Create("contraseña-nueva-9", false); err != nil {
		t.Fatal(err)
	}
}

func TestSaltMissingIsNotReportedAsWrongPassword(t *testing.T) {
	e := newEnv(t)
	if _, err := e.vault(nil).Create(pw, false); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(SaltPath(e.dir))
	if _, err := e.restart(nil).Unlock(pw, false); !errors.Is(err, ErrSaltMissing) {
		t.Fatalf("esperaba ErrSaltMissing: %v", err)
	}
}

// Una llave recordada que no abre esta base (se restauró otra copia) se olvida
// para no reintentar en cada arranque.
func TestRememberedKeyThatNoLongerValidatesIsForgotten(t *testing.T) {
	e := newEnv(t)
	if _, err := e.vault(nil).Create(pw, true); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(Service, MasterAccount, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))); err != nil {
		t.Fatal(err)
	}
	v2 := e.restart(nil)
	if _, _, err := v2.Encrypt("x"); codeOf(err) != CodeLocked {
		t.Fatalf("%v", err)
	}
	if _, err := keyring.Get(Service, MasterAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Error("la llave inválida sigue en el llavero")
	}
	if st, _ := v2.Status(); st.Remembered {
		t.Error("debe dejar de figurar como recordada")
	}
}

func TestRememberedEntryDeletedFromOutsideTurnsRememberOff(t *testing.T) {
	e := newEnv(t)
	if _, err := e.vault(nil).Create(pw, true); err != nil {
		t.Fatal(err)
	}
	_ = keyring.Delete(Service, MasterAccount)
	v2 := e.restart(nil)
	if _, _, err := v2.Encrypt("x"); codeOf(err) != CodeLocked {
		t.Fatalf("%v", err)
	}
	if st, _ := v2.Status(); st.Remembered {
		t.Error("debe dejar de figurar como recordada")
	}
}

// Migración desde T1: las claves cifradas con la llave aleatoria «ScadLab /
// model-servers-key» se re-cifran con la llave nueva y la entrada vieja se borra.
func TestCreateMigratesKeysEncryptedWithTheT1DataKey(t *testing.T) {
	e := newEnv(t)
	legacy := bytes.Repeat([]byte{5}, 32)
	if err := keyring.Set(Service, LegacyAccount, base64.StdEncoding.EncodeToString(legacy)); err != nil {
		t.Fatal(err)
	}
	ct, nonce, _ := crypto.Encrypt(legacy, []byte("sk-de-t1"))
	s := saveToken(t, e.db, "Viejo", ct, nonce)

	v := e.vault(nil)
	if _, _, err := v.Encrypt("x"); codeOf(err) != CodeUninitialized {
		t.Fatal("sin contraseña maestra no se cifra")
	}
	res, err := v.Create(pw, true)
	if err != nil || res.LostKeys != 0 {
		t.Fatalf("Create: %+v, %v", res, err)
	}
	c2, n2, err := e.db.ModelServerToken(s.ID)
	if err != nil || bytes.Equal(c2, ct) {
		t.Fatalf("la clave no se re-cifró: %v", err)
	}
	if got, err := v.Decrypt(c2, n2); err != nil || got != "sk-de-t1" {
		t.Fatalf("clave migrada: %q, %v", got, err)
	}
	if _, err := keyring.Get(Service, LegacyAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Error("la entrada vieja debe borrarse")
	}
}

func TestCreateDropsT1KeysWhoseDataKeyIsGone(t *testing.T) {
	e := newEnv(t)
	ct, nonce, _ := crypto.Encrypt(bytes.Repeat([]byte{5}, 32), []byte("sk-sin-llave"))
	s := saveToken(t, e.db, "Perdido", ct, nonce)
	res, err := e.vault(nil).Create(pw, false)
	if err != nil || res.LostKeys != 1 {
		t.Fatalf("Create: %+v, %v", res, err)
	}
	if got, _ := e.db.GetModelServer(s.ID); got.HasToken {
		t.Error("una clave que no se puede abrir no debe quedar guardada")
	}
}

func TestPasswordAndKeyNeverAppearInErrorsOrDetails(t *testing.T) {
	e := newEnv(t)
	keyring.MockInitWithError(errors.New("boom"))
	v := e.vault(nil)
	res, _ := v.Create(pw, true)
	_, err1 := v.Unlock("otra-contraseña-x", false)
	_, err2 := v.Change("otra-contraseña-x", "nueva-contraseña-2")
	key, _ := v.Key()
	for _, s := range []string{res.Detail, errText(err1), errText(err2)} {
		if strings.Contains(s, pw) || strings.Contains(s, "otra-contraseña") || strings.Contains(s, "nueva-contraseña") || strings.Contains(s, base64.StdEncoding.EncodeToString(key)) {
			t.Fatalf("hay un secreto en %q", s)
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
