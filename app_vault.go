package main

import (
	"sync"

	"scadlab/backend/i18n"
	"scadlab/backend/vaultgate"
)

// Llavero propio de ScadLab (S12, 12.10): la contraseña maestra que protege las
// claves de API. Ver backend/vaultgate y claude/19-hermes.md.
//
//   - Se CREA la primera vez que se guarda una clave de API (SaveModelServer
//     responde con el error de código vault-uninitialized y la interfaz abre
//     «Crear contraseña maestra»). Con «Recordar en este equipo» (marcado por
//     defecto) la llave derivada va al llavero del SO.
//   - Después es automático: la llave se lee del llavero UNA vez, perezosa, y
//     queda en memoria hasta cerrar.
//   - Sin recordar, o con el llavero denegado, los bindings que necesitan
//     descifrar responden vault-locked; la interfaz abre «Desbloquear» y
//     reintenta. Nunca en bucle.
//
// **La contraseña solo entra por CreateMasterPassword, UnlockVault y
// ChangeMasterPassword**; ni ella ni la llave derivada salen nunca de Go (ni en
// un JSON, ni en un error, ni en un log).

// VaultStatus es el estado del llavero propio. Sondea el llavero del sistema
// una sola vez por sesión (una cuenta que no existe) y nunca lee la llave
// recordada: Unlocked puede ser false con Remembered true hasta el primer uso.
type VaultStatus struct {
	Initialized      bool   `json:"initialized"`
	Unlocked         bool   `json:"unlocked"`
	Remembered       bool   `json:"remembered"`
	KeyringAvailable bool   `json:"keyringAvailable"`
	KeyringDetail    string `json:"keyringDetail"`
}

// VaultResult cuenta lo que pasó con «Recordar en este equipo». Que el llavero
// no pueda guardar la llave no hace fallar la operación: la contraseña quedó
// creada o cambiada y la app pedirá la contraseña una vez por sesión.
type VaultResult struct {
	RememberFailed bool   `json:"rememberFailed"`
	Detail         string `json:"detail"`
	// LostKeys son las claves de la versión anterior que no se pudieron migrar
	// (hay que volver a escribirlas); casi siempre 0.
	LostKeys int `json:"lostKeys"`
}

var vaultServices sync.Map // *App → *vaultgate.Vault

// vaultSvc devuelve el llavero de esta App, creado la primera vez que se usa
// (no toca el llavero del sistema). Quien lo llama ya pasó por a.ready().
func (a *App) vaultSvc() *vaultgate.Vault {
	if v, ok := vaultServices.Load(a); ok {
		return v.(*vaultgate.Vault)
	}
	v := vaultgate.New(a.dataDir, a.store, nil)
	actual, loaded := vaultServices.LoadOrStore(a, v)
	if !loaded {
		a.onShutdown(func() {
			v.Close()
			vaultServices.Delete(a)
		})
	}
	return actual.(*vaultgate.Vault)
}

// isVaultLockError dice si err es de los que la interfaz resuelve pidiendo la
// contraseña (crearla o escribirla) y reintentando.
func isVaultLockError(err error) bool {
	switch i18n.Code(err) {
	case vaultgate.CodeLocked, vaultgate.CodeUninitialized:
		return true
	}
	return false
}

func vaultResult(r vaultgate.Result) VaultResult {
	return VaultResult{RememberFailed: r.RememberFailed, Detail: r.Detail, LostKeys: r.LostKeys}
}

// VaultStatus dice si hay contraseña maestra, si está desbloqueada, si se
// recuerda en este equipo y si el llavero del sistema se puede usar.
func (a *App) VaultStatus() (VaultStatus, error) {
	if err := a.ready(); err != nil {
		return VaultStatus{}, err
	}
	st, err := a.vaultSvc().Status()
	if err != nil {
		return VaultStatus{}, err
	}
	return VaultStatus{
		Initialized: st.Initialized, Unlocked: st.Unlocked, Remembered: st.Remembered,
		KeyringAvailable: st.KeyringAvailable, KeyringDetail: st.KeyringDetail,
	}, nil
}

// CreateMasterPassword crea la contraseña maestra (mínimo 8 caracteres) y deja
// el llavero desbloqueado. Con remember, la llave derivada —no la contraseña— se
// guarda en el llavero del sistema para no volver a pedirla. Re-cifra las claves
// que dejó la versión anterior (llave aleatoria) y borra esa entrada.
func (a *App) CreateMasterPassword(password string, remember bool) (VaultResult, error) {
	if err := a.ready(); err != nil {
		return VaultResult{}, err
	}
	r, err := a.vaultSvc().Create(password, remember)
	return vaultResult(r), err
}

// UnlockVault desbloquea con la contraseña maestra (vault-wrong-password si no
// es). remember deja la llave en el llavero del sistema o la quita.
func (a *App) UnlockVault(password string, remember bool) (VaultResult, error) {
	if err := a.ready(); err != nil {
		return VaultResult{}, err
	}
	r, err := a.vaultSvc().Unlock(password, remember)
	return vaultResult(r), err
}

// ChangeMasterPassword cambia la contraseña y re-cifra todas las claves guardadas
// en una sola transacción: si alguna no se puede abrir, no cambia nada.
func (a *App) ChangeMasterPassword(oldPassword, newPassword string) (VaultResult, error) {
	if err := a.ready(); err != nil {
		return VaultResult{}, err
	}
	r, err := a.vaultSvc().Change(oldPassword, newPassword)
	if err == nil {
		a.serverSvc().invalidateAll()
	}
	return vaultResult(r), err
}

// ForgetThisDevice borra la llave recordada del llavero del sistema («Olvidar
// este equipo»). La sesión actual sigue desbloqueada; la próxima pide la
// contraseña.
func (a *App) ForgetThisDevice() error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.vaultSvc().Forget()
}

// SetRememberDevice enciende o apaga «Recordar en este equipo» con el llavero ya
// desbloqueado (apagarlo equivale a ForgetThisDevice).
func (a *App) SetRememberDevice(remember bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.vaultSvc().SetRemember(remember)
}

// ResetVault borra TODAS las claves de API guardadas, el verificador, el salt y
// la entrada del llavero, y devuelve cuántas claves se borraron. Los servidores
// quedan, sin clave. Es lo único que se puede hacer con una contraseña olvidada.
func (a *App) ResetVault() (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	n, err := a.vaultSvc().Reset()
	a.serverSvc().invalidateAll()
	return n, err
}
