package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"

	"scadlab/backend/crypto"
	"scadlab/backend/i18n"
	"scadlab/backend/store"
	"scadlab/backend/vaultgate"
)

// countingKeychain cuenta las lecturas del llavero por cuenta: la llave recordada
// se lee UNA vez por sesión, no una por llamada.
type countingKeychain struct {
	vaultgate.Backend
	mu   sync.Mutex
	gets map[string]int
}

func newCountingKeychain() *countingKeychain {
	return &countingKeychain{Backend: vaultgate.OSBackend(), gets: map[string]int{}}
}

func (c *countingKeychain) Get(service, account string) (string, error) {
	c.mu.Lock()
	c.gets[account]++
	c.mu.Unlock()
	return c.Backend.Get(service, account)
}

func (c *countingKeychain) reads(account string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gets[account]
}

// reopen simula cerrar la app y abrirla de nuevo: una App nueva sobre la misma
// carpeta de datos y la misma base, con un llavero propio sin nada en memoria.
func reopen(t *testing.T, a *App, kb vaultgate.Backend) *App {
	t.Helper()
	_ = a.store.Close()
	st, err := store.Open(filepath.Join(a.dataDir, "scadlab.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := &App{ctx: context.Background(), store: st, dataDir: a.dataDir}
	vaultServices.Store(b, vaultgate.New(b.dataDir, st, kb))
	t.Cleanup(func() {
		vaultServices.Delete(b)
		serverServices.Delete(b)
	})
	return b
}

// vaultFixture arma un servidor OpenAI simulado, una App con una clave de API
// ya guardada y un chat para conversar con él.
func vaultFixture(t *testing.T, remember bool) (a *App, f *fakeOpenAI, saved store.ModelServer, chatID int64) {
	t.Helper()
	keyring.MockInit()
	a = newTestApp(t)
	f = &fakeOpenAI{chunks: []string{"Hola"}}
	srv := f.server(t)
	if _, err := a.CreateMasterPassword(testMasterPassword, remember); err != nil {
		t.Fatal(err)
	}
	var err error
	if saved, err = a.SaveModelServer(openAIServer(srv.URL), secretKey, false); err != nil {
		t.Fatal(err)
	}
	c, err := a.NewChat(mustWorkspace(t, a), saved.ID, "Alfa-model")
	if err != nil {
		t.Fatal(err)
	}
	return a, f, saved, c.ID
}

func code(err error) string { return i18n.Code(err) }

// Primera vez con «Recordar»: la contraseña se crea al guardar la primera clave y
// después, tras un reinicio, TODO funciona sin pedir nada —probar, listar modelos
// y chatear— con una sola lectura del llavero.
func TestVaultFirstTimeThenRestartNeedsNothing(t *testing.T) {
	keyring.MockInit()
	a := newTestApp(t)
	f := &fakeOpenAI{chunks: []string{"Hola ", "mundo"}}
	srv := f.server(t)

	st, err := a.VaultStatus()
	if err != nil || st.Initialized || st.Unlocked || st.Remembered {
		t.Fatalf("estado inicial: %+v, %v", st, err)
	}
	// Guardar la primera clave pide crear la contraseña maestra.
	if _, err := a.SaveModelServer(openAIServer(srv.URL), secretKey, false); code(err) != vaultgate.CodeUninitialized {
		t.Fatalf("la primera clave sin contraseña maestra: %v", err)
	}
	res, err := a.CreateMasterPassword(testMasterPassword, true)
	if err != nil || res.RememberFailed || res.LostKeys != 0 {
		t.Fatalf("CreateMasterPassword: %+v, %v", res, err)
	}
	saved, err := a.SaveModelServer(openAIServer(srv.URL), secretKey, false)
	if err != nil || !saved.HasToken {
		t.Fatalf("SaveModelServer: %+v, %v", saved, err)
	}
	// Una segunda clave tampoco pide nada.
	other := openAIServer(srv.URL)
	other.Name = "Otra nube"
	second, err := a.SaveModelServer(other, "otra-"+secretKey, false)
	if err != nil {
		t.Fatal(err)
	}
	chat, err := a.NewChat(mustWorkspace(t, a), saved.ID, "Alfa-model")
	if err != nil {
		t.Fatal(err)
	}

	kc := newCountingKeychain()
	b := reopen(t, a, kc)
	st, _ = b.VaultStatus()
	if !st.Initialized || st.Unlocked || !st.Remembered {
		t.Fatalf("tras reiniciar: %+v", st)
	}
	if kc.reads(vaultgate.MasterAccount) != 0 {
		t.Fatal("arrancar o preguntar el estado no debe leer la llave")
	}

	ev := captureChatEvents(b)
	if models, err := b.ListServerModels(saved.ID, true); err != nil || len(models) != 2 {
		t.Fatalf("ListServerModels: %+v, %v", models, err)
	}
	if f.lastAuth() != "Bearer "+secretKey {
		t.Errorf("el servidor no recibió la clave guardada: %q", f.lastAuth())
	}
	if pr, err := b.TestModelServer(saved, ""); err != nil || !pr.OK {
		t.Fatalf("TestModelServer: %+v, %v", pr, err)
	}
	for i := 0; i < 3; i++ {
		if _, err := b.SendChatMessage(chat.ID, "hola", "", nil, SendOptions{}); err != nil {
			t.Fatalf("SendChatMessage %d: %v", i, err)
		}
		waitDone(t, ev)
	}
	if _, err := b.SaveModelServer(second, "tercera-clave", false); err != nil {
		t.Fatal(err)
	}
	if n := kc.reads(vaultgate.MasterAccount); n != 1 {
		t.Fatalf("la llave se leyó %d veces del llavero, esperaba 1", n)
	}
	if st, _ = b.VaultStatus(); !st.Unlocked {
		t.Fatalf("tras el primer uso debe estar desbloqueada: %+v", st)
	}
}

// Sin «Recordar»: la sesión siguiente queda bloqueada, los tres caminos que
// descifran devuelven vault-locked (la interfaz abre «Desbloquear» y reintenta),
// un envío bloqueado no deja mensaje huérfano y, desbloqueada, todo sigue.
func TestVaultWithoutRememberAsksOncePerSession(t *testing.T) {
	a, f, saved, chatID := vaultFixture(t, false)
	kc := newCountingKeychain()
	b := reopen(t, a, kc)
	ev := captureChatEvents(b)

	st, _ := b.VaultStatus()
	if !st.Initialized || st.Unlocked || st.Remembered {
		t.Fatalf("estado: %+v", st)
	}
	if _, err := b.ListServerModels(saved.ID, true); code(err) != vaultgate.CodeLocked {
		t.Errorf("ListServerModels: %v", err)
	}
	if _, err := b.TestModelServer(saved, ""); code(err) != vaultgate.CodeLocked {
		t.Errorf("TestModelServer: %v", err)
	}
	if _, err := b.SendChatMessage(chatID, "hola", "", nil, SendOptions{}); code(err) != vaultgate.CodeLocked {
		t.Errorf("SendChatMessage: %v", err)
	}
	if v, _ := b.GetChat(chatID); len(v.Messages) != 0 {
		t.Errorf("un envío bloqueado no guarda el mensaje: %+v", v.Messages)
	}
	// Una clave nueva también pide la contraseña, no la crea de nuevo.
	if _, err := b.SaveModelServer(openAIServer("http://localhost:1"), "otra", false); code(err) != vaultgate.CodeLocked {
		t.Errorf("SaveModelServer: %v", err)
	}
	// Sin clave guardada, un servidor no necesita la contraseña para nada.
	local, err := b.SaveModelServer(store.ModelServer{Kind: store.ServerOllama, Name: "Local", BaseURL: "http://localhost:1", Enabled: true}, "", false)
	if err != nil {
		t.Fatalf("un servidor sin clave no debe pedir la contraseña: %v", err)
	}
	_ = local

	if _, err := b.UnlockVault("no es esa contraseña", false); code(err) != vaultgate.CodeWrongPassword {
		t.Fatalf("contraseña incorrecta: %v", err)
	}
	if _, err := b.UnlockVault(testMasterPassword, false); err != nil {
		t.Fatal(err)
	}
	// «Reintentar la acción» tras desbloquear.
	if _, err := b.SendChatMessage(chatID, "hola", "", nil, SendOptions{}); err != nil {
		t.Fatalf("tras desbloquear: %v", err)
	}
	waitDone(t, ev)
	if pr, err := b.TestModelServer(saved, ""); err != nil || !pr.OK || f.lastAuth() != "Bearer "+secretKey {
		t.Errorf("tras desbloquear: %+v, %v, %q", pr, err, f.lastAuth())
	}
	if n := kc.reads(vaultgate.MasterAccount); n != 0 {
		t.Errorf("sin recordar no se lee la llave del llavero (%d)", n)
	}
}

// El llavero que niega el acceso al arrancar: se intenta UNA vez, nunca en bucle,
// y la app cae a pedir la contraseña sin romperse. «Recordar» no se apaga.
func TestVaultDeniedKeychainAtStartupFallsBackWithoutLoop(t *testing.T) {
	a, _, saved, _ := vaultFixture(t, true)
	keyring.MockInitWithError(errors.New("acceso denegado por el usuario"))
	kc := newCountingKeychain()
	b := reopen(t, a, kc)

	for i := 0; i < 5; i++ {
		if _, err := b.TestModelServer(saved, ""); code(err) != vaultgate.CodeLocked {
			t.Fatalf("vuelta %d: %v", i, err)
		}
	}
	if n := kc.reads(vaultgate.MasterAccount); n != 1 {
		t.Fatalf("se intentó leer %d veces, esperaba 1", n)
	}
	st, _ := b.VaultStatus()
	if st.KeyringAvailable || st.KeyringDetail == "" || !st.Remembered {
		t.Fatalf("estado: %+v", st)
	}
	if _, err := b.UnlockVault(testMasterPassword, true); err != nil {
		t.Fatal(err)
	}
	if pr, err := b.TestModelServer(saved, ""); err != nil || !pr.OK {
		t.Fatalf("tras desbloquear: %+v, %v", pr, err)
	}
}

func TestVaultForgetThisDevice(t *testing.T) {
	a, _, saved, _ := vaultFixture(t, true)
	if err := a.ForgetThisDevice(); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(vaultgate.Service, vaultgate.MasterAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("la entrada del llavero sigue: %v", err)
	}
	if st, _ := a.VaultStatus(); st.Remembered || !st.Unlocked {
		t.Fatalf("esta sesión sigue desbloqueada: %+v", st)
	}
	b := reopen(t, a, nil)
	if _, err := b.TestModelServer(saved, ""); code(err) != vaultgate.CodeLocked {
		t.Fatalf("la sesión siguiente pide la contraseña: %v", err)
	}
	// Con la sesión desbloqueada se vuelve a recordar y el siguiente arranque ya no pregunta.
	if _, err := b.UnlockVault(testMasterPassword, false); err != nil {
		t.Fatal(err)
	}
	if err := b.SetRememberDevice(true); err != nil {
		t.Fatal(err)
	}
	c := reopen(t, b, nil)
	if pr, err := c.TestModelServer(saved, ""); err != nil || !pr.OK {
		t.Fatalf("recordado de nuevo: %+v, %v", pr, err)
	}
}

// Cambiar la contraseña re-cifra todas las claves: siguen descifrando con la
// llave nueva (recordada) y la contraseña vieja ya no abre nada.
func TestVaultChangeMasterPasswordKeepsKeysWorking(t *testing.T) {
	a, f, saved, _ := vaultFixture(t, true)
	other := openAIServer("http://localhost:1")
	other.Name = "Otra"
	o, err := a.SaveModelServer(other, "otra-clave-0123", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ChangeMasterPassword("no es la vieja", "contraseña-nueva-7"); code(err) != vaultgate.CodeWrongPassword {
		t.Fatalf("vieja incorrecta: %v", err)
	}
	if _, err := a.ChangeMasterPassword(testMasterPassword, "corta"); err == nil {
		t.Fatal("la nueva es débil")
	}
	if res, err := a.ChangeMasterPassword(testMasterPassword, "contraseña-nueva-7"); err != nil || res.RememberFailed {
		t.Fatalf("ChangeMasterPassword: %+v, %v", res, err)
	}
	if pr, _ := a.TestModelServer(saved, ""); !pr.OK || f.lastAuth() != "Bearer "+secretKey {
		t.Fatalf("misma sesión: %+v %q", pr, f.lastAuth())
	}

	b := reopen(t, a, nil)
	if pr, err := b.TestModelServer(saved, ""); err != nil || !pr.OK || f.lastAuth() != "Bearer "+secretKey {
		t.Fatalf("tras reiniciar (llave recordada nueva): %+v, %v, %q", pr, err, f.lastAuth())
	}
	if tok, err := b.serverToken(o, o.ID); err != nil || tok != "otra-clave-0123" {
		t.Fatalf("la segunda clave: %q, %v", tok, err)
	}
	// Sin la llave recordada solo abre la contraseña nueva.
	_ = b.ForgetThisDevice()
	c := reopen(t, b, nil)
	if _, err := c.UnlockVault(testMasterPassword, false); code(err) != vaultgate.CodeWrongPassword {
		t.Fatalf("la vieja no debe abrir: %v", err)
	}
	if _, err := c.UnlockVault("contraseña-nueva-7", false); err != nil {
		t.Fatal(err)
	}
}

func TestVaultResetErasesKeysAndStartsOver(t *testing.T) {
	a, _, saved, _ := vaultFixture(t, true)
	n, err := a.ResetVault()
	if err != nil || n != 1 {
		t.Fatalf("ResetVault: %d, %v", n, err)
	}
	list, _ := a.ListModelServers()
	for _, s := range list {
		if s.HasToken {
			t.Errorf("%s conserva su clave", s.Name)
		}
	}
	if _, err := os.Stat(filepath.Join(a.dataDir, vaultgate.SaltFile)); err == nil {
		t.Error("queda vault.salt")
	}
	if _, err := keyring.Get(vaultgate.Service, vaultgate.MasterAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Error("queda la entrada del llavero")
	}
	st, _ := a.VaultStatus()
	if st.Initialized || st.Unlocked || st.Remembered {
		t.Fatalf("estado: %+v", st)
	}
	// Guardar una clave vuelve a pedir crear la contraseña, y se puede crear una distinta.
	if _, err := a.SaveModelServer(saved, "otra", false); code(err) != vaultgate.CodeUninitialized {
		t.Fatalf("%v", err)
	}
	if _, err := a.CreateMasterPassword("otra-contraseña-nueva", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveModelServer(saved, secretKey, false); err != nil {
		t.Fatal(err)
	}
}

// Migración desde T1: la clave cifrada con la llave aleatoria «ScadLab /
// model-servers-key» se re-cifra al crear la contraseña y esa entrada se borra.
func TestVaultMigratesT1DataKey(t *testing.T) {
	keyring.MockInit()
	a := newTestApp(t)
	f := &fakeOpenAI{}
	srv := f.server(t)
	legacy := bytes.Repeat([]byte{0x42}, 32)
	if err := keyring.Set(vaultgate.Service, vaultgate.LegacyAccount, base64.StdEncoding.EncodeToString(legacy)); err != nil {
		t.Fatal(err)
	}
	ct, nonce, _ := crypto.Encrypt(legacy, []byte(secretKey))
	saved, err := a.store.SaveModelServer(openAIServer(srv.URL), store.TokenWrite{Set: true, Cipher: ct, Nonce: nonce})
	if err != nil {
		t.Fatal(err)
	}
	// Antes de crear la contraseña, usar esa clave pide crearla.
	if _, err := a.ListServerModels(saved.ID, true); code(err) != vaultgate.CodeUninitialized {
		t.Fatalf("%v", err)
	}
	res, err := a.CreateMasterPassword(testMasterPassword, true)
	if err != nil || res.LostKeys != 0 {
		t.Fatalf("CreateMasterPassword: %+v, %v", res, err)
	}
	if pr, err := a.TestModelServer(saved, ""); err != nil || !pr.OK || f.lastAuth() != "Bearer "+secretKey {
		t.Fatalf("la clave de T1 no sobrevivió: %+v, %v, %q", pr, err, f.lastAuth())
	}
	if _, err := keyring.Get(vaultgate.Service, vaultgate.LegacyAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Error("la entrada vieja debía borrarse")
	}
}

// Ni la contraseña ni la llave derivada ni la clave de API aparecen en el archivo
// .db (con su WAL), en los errores ni en el JSON que cruza hacia el frontend.
func TestVaultSecretsNeverLeak(t *testing.T) {
	a, _, saved, _ := vaultFixture(t, true)
	const newPw = "contraseña-nueva-7"

	var seen []string // todo lo que el frontend o un log podrían ver
	add := func(v any, err error) {
		raw, _ := json.Marshal(v)
		seen = append(seen, string(raw))
		if err != nil {
			seen = append(seen, err.Error())
		}
	}
	st, err := a.VaultStatus()
	add(st, err)
	add(a.UnlockVault("equivocada-equivocada", true))
	add(a.CreateMasterPassword(testMasterPassword, true)) // ya existe
	add(a.ChangeMasterPassword("equivocada-equivocada", newPw))
	add(a.ChangeMasterPassword(testMasterPassword, newPw))
	list, err := a.ListModelServers()
	add(list, err)
	add(saved, nil)
	add(a.TestModelServer(saved, ""))
	// La app nueva arranca con la llave recordada; olvidada, la siguiente sesión
	// queda bloqueada y su error y sus respuestas tampoco filtran nada.
	b := reopen(t, a, nil)
	add(b.TestModelServer(saved, ""))
	_ = b.ForgetThisDevice()
	c := reopen(t, b, nil)
	_, lockErr := c.TestModelServer(saved, "")
	add(nil, lockErr)
	add(c.UnlockVault("equivocada-equivocada", false))

	// Todos los valores secretos, en claro y en las codificaciones habituales.
	salt, err := vaultgate.LoadSalt(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string][]byte{"clave de API": []byte(secretKey), "contraseña": []byte(testMasterPassword), "contraseña nueva": []byte(newPw)}
	for name, pw := range map[string]string{"": testMasterPassword, "n": newPw} {
		k := crypto.DeriveKey([]byte(pw), salt)
		secrets["llave"+name] = k
		secrets["llave b64"+name] = []byte(base64.StdEncoding.EncodeToString(k))
		secrets["llave hex"+name] = []byte(hex.EncodeToString(k))
	}
	for name, s := range secrets {
		for i, text := range seen {
			if strings.Contains(text, string(s)) {
				t.Errorf("%s aparece en lo que ve el frontend (#%d): %.120s", name, i, text)
			}
		}
		for _, f := range []string{"scadlab.db", "scadlab.db-wal", "scadlab.db-shm"} {
			if raw, err := os.ReadFile(filepath.Join(a.dataDir, f)); err == nil && bytes.Contains(raw, s) {
				t.Errorf("%s está en %s", name, f)
			}
		}
	}
}

// La clave de API y la contraseña tampoco se escriben en el log del proceso.
func TestVaultDoesNotLogSecrets(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	a, _, saved, _ := vaultFixture(t, true)
	_, _ = a.TestModelServer(saved, "")
	_, _ = a.ChangeMasterPassword(testMasterPassword, "contraseña-nueva-7")
	if s := buf.String(); strings.Contains(s, secretKey) || strings.Contains(s, testMasterPassword) || strings.Contains(s, "contraseña-nueva-7") {
		t.Fatalf("hay un secreto en el log: %s", s)
	}
}
