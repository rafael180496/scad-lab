package vaultgate

import (
	"encoding/base64"
	"sync"
	"unicode/utf8"

	"scadlab/backend/crypto"
	"scadlab/backend/i18n"
	"scadlab/backend/store"
)

// verifierPlaintext no significa nada: es un valor conocido que se cifra al
// crear la contraseña y se intenta abrir al desbloquear. Si el GCM autentica,
// la llave —y por tanto la contraseña— es la buena.
const verifierPlaintext = "scadlab-vault-v1"

// MinPasswordLen es el mínimo que acepta el backend. La interfaz muestra además
// un indicador de fuerza, pero el piso se exige acá: no depende de la UI.
const MinPasswordLen = 8

// ErrAlreadyInitialized: ya hay contraseña maestra (cámbiala, o restablece).
var ErrAlreadyInitialized = i18n.New(i18n.Msg{
	ES: "ya hay una contraseña maestra: cámbiala, o restablece el llavero para empezar de cero",
	EN: "a master password already exists: change it, or reset the keychain to start over",
})

// ErrWeakPassword: por debajo del piso.
var ErrWeakPassword = i18n.New(i18n.Msg{
	ES: "la contraseña maestra necesita al menos 8 caracteres",
	EN: "the master password needs at least 8 characters",
})

// ErrDecrypt: la clave guardada no se abre con la llave actual.
var ErrDecrypt = i18n.New(i18n.Msg{
	ES: "no se pudo descifrar la clave de API guardada: vuelve a escribirla en Configuración → Proveedores de IA",
	EN: "the saved API key could not be decrypted: type it again in Settings → AI providers",
})

// Status es lo que la interfaz necesita saber, sin tocar el llavero para
// calcularlo más de una vez por sesión.
type Status struct {
	// Initialized: ya se creó la contraseña maestra.
	Initialized bool
	// Unlocked: la llave está en memoria en este momento. Con «recordar» puede
	// ser false hasta el primer uso, que la recupera sola.
	Unlocked bool
	// Remembered: la llave derivada está guardada en el llavero del sistema.
	Remembered bool
	// KeyringAvailable: el llavero responde (y no falló en esta sesión).
	KeyringAvailable bool
	// KeyringDetail explica por qué no, para mostrarlo.
	KeyringDetail string
}

// Result informa lo que pasó con «Recordar en este equipo» al crear, desbloquear
// o cambiar la contraseña: la operación principal salió bien aunque el llavero
// no haya podido guardar la llave, y eso no es un error de la llamada.
type Result struct {
	// RememberFailed: se pidió recordar y el llavero no lo permitió; la app
	// pedirá la contraseña una vez por sesión.
	RememberFailed bool
	// Detail es el motivo del llavero (vacío si no hubo fallo).
	Detail string
	// LostKeys son las claves de la llave aleatoria de T1 que no se pudieron
	// migrar al crear la contraseña (hay que volver a escribirlas).
	LostKeys int
}

// Vault orquesta el llavero propio: contraseña maestra, verificador en la base,
// salt en archivo, llave en memoria y «recordar» en el llavero del SO. Es
// seguro entre goroutines.
type Vault struct {
	dir  string
	repo *store.Store
	kb   Backend
	gate *Gate

	// mu serializa las operaciones que cambian el estado (crear, desbloquear,
	// cambiar, olvidar, restablecer) y el autodesbloqueo perezoso: dos usos
	// simultáneos de una llave recordada hacen UNA lectura del llavero, no dos.
	mu        sync.Mutex
	autoTried bool

	// kmu guarda el estado del llavero visto en esta sesión. Aparte de mu a
	// propósito: Status no debe quedar esperando a un diálogo del sistema.
	// pmu hace que el sondeo ocurra una sola vez aunque lleguen dos Status juntos.
	kmu      sync.Mutex
	pmu      sync.Mutex
	probed   bool
	down     bool
	downInfo string
}

// New crea el Vault sobre el directorio de datos dir y la base. kb nil usa el
// llavero real. No toca el llavero.
func New(dir string, repo *store.Store, kb Backend) *Vault {
	if kb == nil {
		kb = OSBackend()
	}
	return &Vault{dir: dir, repo: repo, kb: kb, gate: NewGate()}
}

// ── Estado ─────────────────────────────────────────────────────────────────

// Status calcula el estado. Sondea el llavero UNA vez por sesión (una cuenta que
// no existe, nunca la real) para saber si responde; no lee la llave recordada.
func (v *Vault) Status() (Status, error) {
	meta, ok, err := v.repo.GetVaultMeta()
	if err != nil {
		return Status{}, err
	}
	avail, detail := v.keyringState()
	return Status{
		Initialized: ok, Unlocked: v.gate.Unlocked(), Remembered: ok && meta.Remember,
		KeyringAvailable: avail, KeyringDetail: detail,
	}, nil
}

func (v *Vault) keyringState() (bool, string) {
	v.pmu.Lock()
	v.kmu.Lock()
	done := v.probed
	v.kmu.Unlock()
	if !done {
		if _, err := v.kb.Get(Service, probeAccount); err != nil && !isNotFound(err) {
			v.markDown(err)
		} else {
			v.kmu.Lock()
			v.probed = true
			v.kmu.Unlock()
		}
	}
	v.pmu.Unlock()
	v.kmu.Lock()
	defer v.kmu.Unlock()
	return !v.down, v.downInfo
}

// markDown marca el llavero como no disponible durante esta sesión: nada más lo
// intenta (nunca en bucle) hasta que se reinicie la app.
func (v *Vault) markDown(err error) {
	v.kmu.Lock()
	defer v.kmu.Unlock()
	v.down = true
	v.probed = true
	v.downInfo = i18n.T(i18n.Msg{
		ES: "no se puede usar el llavero del sistema (%v)",
		EN: "the system keychain cannot be used (%v)",
	}, err)
}

func (v *Vault) isDown() bool {
	v.kmu.Lock()
	defer v.kmu.Unlock()
	return v.down
}

// ── Llave ──────────────────────────────────────────────────────────────────

// Key devuelve (una copia de) la llave. Si la app está bloqueada y había una
// llave recordada, la recupera del llavero —UNA vez por sesión, aquí, perezoso—
// y la deja en memoria. ErrNotInitialized si no hay contraseña maestra;
// ErrLocked si no se pudo recuperar sola.
func (v *Vault) Key() ([]byte, error) {
	if k, err := v.gate.Key(); err == nil {
		return k, nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	meta, ok, err := v.repo.GetVaultMeta()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotInitialized
	}
	if !v.tryAutoUnlock(meta) {
		return nil, ErrLocked
	}
	return v.gate.Key()
}

// TryAutoUnlock intenta desbloquear con la llave recordada en el llavero. Lo
// llama Key la primera vez que hace falta (no al arrancar). Todo fallo —nada
// recordado, llavero denegado, llave que ya no valida— cae en silencio a «hay
// que escribir la contraseña» y no se reintenta en esta sesión.
func (v *Vault) TryAutoUnlock() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	meta, ok, err := v.repo.GetVaultMeta()
	if err != nil || !ok {
		return false
	}
	return v.tryAutoUnlock(meta)
}

// tryAutoUnlock con v.mu tomado.
func (v *Vault) tryAutoUnlock(meta store.VaultMeta) bool {
	if v.gate.Unlocked() {
		return true
	}
	if v.autoTried || !meta.Remember || v.isDown() {
		return false
	}
	v.autoTried = true
	raw, err := v.kb.Get(Service, MasterAccount)
	switch {
	case err == nil:
	case isNotFound(err):
		// Alguien borró la entrada desde fuera: ya no está recordada.
		_ = v.repo.SetVaultRemember(false)
		return false
	default:
		// Denegado o no disponible: NO se borra nada (puede ser transitorio o un
		// «Denegar» que el usuario revierta); solo se pide la contraseña.
		v.markDown(err)
		return false
	}
	key, derr := base64.StdEncoding.DecodeString(raw)
	if derr != nil || len(key) != crypto.KeySize || !verify(key, meta) {
		// La llave guardada no abre esta base (se restauró otra, o la entrada
		// está dañada): se olvida para no reintentar en cada arranque.
		crypto.Zero(key)
		_ = v.kb.Delete(Service, MasterAccount)
		_ = v.repo.SetVaultRemember(false)
		return false
	}
	v.gate.Set(key)
	crypto.Zero(key)
	return true
}

func verify(key []byte, meta store.VaultMeta) bool {
	plain, err := crypto.Decrypt(key, meta.Verifier, meta.Nonce)
	return err == nil && string(plain) == verifierPlaintext
}

// Encrypt cifra con la llave. Pide la llave a Key: ErrNotInitialized si no hay
// contraseña maestra, ErrLocked si hay que escribirla.
func (v *Vault) Encrypt(plain string) (ciphertext, nonce []byte, err error) {
	key, err := v.Key()
	if err != nil {
		return nil, nil, err
	}
	defer crypto.Zero(key)
	return crypto.Encrypt(key, []byte(plain))
}

// Decrypt descifra con la llave (mismos errores que Encrypt).
func (v *Vault) Decrypt(ciphertext, nonce []byte) (string, error) {
	key, err := v.Key()
	if err != nil {
		return "", err
	}
	defer crypto.Zero(key)
	plain, err := crypto.Decrypt(key, ciphertext, nonce)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}

// ── Crear, desbloquear, cambiar ────────────────────────────────────────────

func (v *Vault) derive(password string, salt []byte) []byte {
	pw := []byte(password)
	defer crypto.Zero(pw)
	return crypto.DeriveKey(pw, salt)
}

// Create crea la contraseña maestra (solo la primera vez). Re-cifra con la
// llave nueva las claves que T1 dejó cifradas con la clave de datos aleatoria
// del llavero y borra esa entrada. Con remember, la llave derivada va al llavero.
func (v *Vault) Create(password string, remember bool) (Result, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok, err := v.repo.GetVaultMeta(); err != nil {
		return Result{}, err
	} else if ok {
		return Result{}, ErrAlreadyInitialized
	}
	if utf8.RuneCountInString(password) < MinPasswordLen {
		return Result{}, ErrWeakPassword
	}
	salt, err := CreateSalt(v.dir)
	if err != nil {
		return Result{}, err
	}
	key := v.derive(password, salt)
	defer crypto.Zero(key)
	ct, nonce, err := crypto.Encrypt(key, []byte(verifierPlaintext))
	if err != nil {
		return Result{}, i18n.Errorf(i18n.Msg{ES: "vaultgate: cifrando el verificador: %w", EN: "vaultgate: encrypting verifier: %w"}, err)
	}

	var res Result
	var re store.Reencrypt
	legacy := v.loadLegacyKey()
	defer crypto.Zero(legacy)
	if n, err := v.repo.CountModelServerTokens(); err == nil && n > 0 {
		re = func(c, nn []byte) ([]byte, []byte, error) {
			if legacy != nil {
				if plain, err := crypto.Decrypt(legacy, c, nn); err == nil {
					defer crypto.Zero(plain)
					return crypto.Encrypt(key, plain)
				}
			}
			res.LostKeys++
			return nil, nil, nil
		}
	}
	if err := v.repo.WriteVault(store.VaultMeta{Verifier: ct, Nonce: nonce}, re); err != nil {
		return Result{}, err
	}
	v.gate.Set(key)
	v.autoTried = true
	// La clave de datos de T1 ya no cifra nada: se borra (si no estaba, no pasa nada).
	_ = v.kb.Delete(Service, LegacyAccount)

	r := v.applyRemember(key, remember)
	r.LostKeys = res.LostKeys
	return r, nil
}

// loadLegacyKey lee la clave de datos aleatoria de T1, si existe y es válida.
func (v *Vault) loadLegacyKey() []byte {
	n, err := v.repo.CountModelServerTokens()
	if err != nil || n == 0 || v.isDown() {
		return nil
	}
	raw, err := v.kb.Get(Service, LegacyAccount)
	if err != nil {
		if !isNotFound(err) {
			v.markDown(err)
		}
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != crypto.KeySize {
		return nil
	}
	return key
}

// Unlock deriva la llave de la contraseña, la valida contra el verificador y
// desbloquea. remember decide si la llave queda (o deja de estar) en el llavero.
func (v *Vault) Unlock(password string, remember bool) (Result, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	meta, ok, err := v.repo.GetVaultMeta()
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, ErrNotInitialized
	}
	salt, err := LoadSalt(v.dir)
	if err != nil {
		return Result{}, err
	}
	key := v.derive(password, salt)
	defer crypto.Zero(key)
	if !verify(key, meta) {
		return Result{}, ErrWrongPassword
	}
	v.gate.Set(key)
	v.autoTried = true
	if remember == meta.Remember {
		return Result{}, nil
	}
	// Con el llavero caído en esta sesión no se toca nada: apagar «recordar» por
	// una negativa pasajera borraría una llave buena.
	if v.isDown() {
		return Result{RememberFailed: remember, Detail: v.downText()}, nil
	}
	return v.applyRemember(key, remember), nil
}

func (v *Vault) downText() string {
	v.kmu.Lock()
	defer v.kmu.Unlock()
	return v.downInfo
}

// applyRemember deja el llavero como pide remember. Con v.mu tomado.
func (v *Vault) applyRemember(key []byte, remember bool) Result {
	if !remember {
		_ = v.kb.Delete(Service, MasterAccount)
		_ = v.repo.SetVaultRemember(false)
		return Result{}
	}
	if v.isDown() {
		_ = v.repo.SetVaultRemember(false)
		return Result{RememberFailed: true, Detail: v.downText()}
	}
	if err := v.kb.Set(Service, MasterAccount, base64.StdEncoding.EncodeToString(key)); err != nil {
		v.markDown(err)
		_ = v.repo.SetVaultRemember(false)
		return Result{RememberFailed: true, Detail: v.downText()}
	}
	_ = v.repo.SetVaultRemember(true)
	return Result{}
}

// Change cambia la contraseña maestra: valida la vieja, deriva la llave nueva y
// re-cifra TODAS las claves guardadas en una sola transacción. Si alguna no se
// abre con la llave vieja, no se cambia nada.
func (v *Vault) Change(oldPassword, newPassword string) (Result, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	meta, ok, err := v.repo.GetVaultMeta()
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, ErrNotInitialized
	}
	if utf8.RuneCountInString(newPassword) < MinPasswordLen {
		return Result{}, ErrWeakPassword
	}
	salt, err := LoadSalt(v.dir)
	if err != nil {
		return Result{}, err
	}
	oldKey := v.derive(oldPassword, salt)
	defer crypto.Zero(oldKey)
	if !verify(oldKey, meta) {
		return Result{}, ErrWrongPassword
	}
	newKey := v.derive(newPassword, salt)
	defer crypto.Zero(newKey)
	ct, nonce, err := crypto.Encrypt(newKey, []byte(verifierPlaintext))
	if err != nil {
		return Result{}, i18n.Errorf(i18n.Msg{ES: "vaultgate: cifrando el verificador: %w", EN: "vaultgate: encrypting verifier: %w"}, err)
	}
	re := func(c, n []byte) ([]byte, []byte, error) {
		plain, err := crypto.Decrypt(oldKey, c, n)
		if err != nil {
			return nil, nil, ErrDecrypt
		}
		defer crypto.Zero(plain)
		return crypto.Encrypt(newKey, plain)
	}
	if err := v.repo.WriteVault(store.VaultMeta{Verifier: ct, Nonce: nonce, Remember: meta.Remember}, re); err != nil {
		return Result{}, err
	}
	v.gate.Set(newKey)
	v.autoTried = true
	if !meta.Remember {
		return Result{}, nil
	}
	// La llave recordada era la vieja: se reemplaza por la nueva o, si el llavero
	// no deja, se deja de recordar (una llave vieja ahí solo daría una vuelta
	// inútil al próximo arranque).
	return v.applyRemember(newKey, true), nil
}

// ── Olvidar, recordar, restablecer ─────────────────────────────────────────

// Forget borra la llave recordada del llavero («Olvidar este equipo»). La app
// sigue desbloqueada hasta que se cierre; la próxima sesión pedirá la contraseña.
func (v *Vault) Forget() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	err := v.kb.Delete(Service, MasterAccount)
	if err != nil && !isNotFound(err) {
		v.markDown(err)
		_ = v.repo.SetVaultRemember(false)
		return i18n.Errorf(i18n.Msg{
			ES: "no se pudo borrar la entrada «ScadLab» del llavero del sistema (%v): bórrala a mano desde el llavero",
			EN: "the “ScadLab” entry could not be deleted from the system keychain (%v): delete it by hand from the keychain",
		}, err)
	}
	return v.repo.SetVaultRemember(false)
}

// SetRemember enciende o apaga «Recordar en este equipo» con la app ya
// desbloqueada. Encenderlo necesita la llave en memoria (ErrLocked si no).
func (v *Vault) SetRemember(remember bool) error {
	if !remember {
		return v.Forget()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok, err := v.repo.GetVaultMeta(); err != nil {
		return err
	} else if !ok {
		return ErrNotInitialized
	}
	key, err := v.gate.Key()
	if err != nil {
		return err
	}
	defer crypto.Zero(key)
	if r := v.applyRemember(key, true); r.RememberFailed {
		return i18n.Errorf(i18n.Msg{ES: "no se pudo recordar la contraseña en este equipo: %s", EN: "could not remember the password on this device: %s"}, r.Detail)
	}
	return nil
}

// Reset borra TODAS las claves de API guardadas, el verificador, el salt y la
// entrada del llavero, y deja la app como en la primera vez. Una contraseña
// olvidada no se recupera: esto es lo único que se puede hacer. Devuelve cuántas
// claves se borraron.
func (v *Vault) Reset() (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	cleared, err := v.repo.ResetVault()
	if err != nil {
		return 0, err
	}
	err = RemoveSalt(v.dir)
	for _, acc := range []string{MasterAccount, LegacyAccount} {
		if derr := v.kb.Delete(Service, acc); derr != nil && !isNotFound(derr) {
			v.markDown(derr)
		}
	}
	v.gate.Lock()
	v.autoTried = false
	return cleared, err
}

// Close pone a cero la llave en memoria. La llama la App al cerrar.
func (v *Vault) Close() { v.gate.Lock() }
