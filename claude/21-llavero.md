# Llavero propio: contraseña maestra y «Recordar en este equipo» (S12, 12.10)

Protege las claves de API de los servidores de modelos ([19-hermes.md](19-hermes.md)).
Pedido del usuario: una contraseña que se crea **solo la primera vez** que se guarda una
clave y, después, que se guarde sola «sin que le pida a cada rato». Es el `vaultgate` de
mini-tools (portado: `crypto/argon2.go`, `vaultgate/{gate,remember,salt}.go`) con dos
diferencias: **no hay pantalla de bloqueo al arrancar** y la llave se lee del llavero del SO
**una vez, perezosa**, nunca por llamada. Reemplaza a `backend/secrets` de T1 (clave de datos
aleatoria en el llavero), que ya no existe.

## Esquema

```
contraseña maestra ──argon2id(salt, t=3, 64 MiB, 4 hilos)──▶ llave de 32 bytes ──AES-256-GCM──▶ claves de API
       │ nunca se guarda                       │ solo en memoria (Gate) y, si el usuario lo elige,
       ▼                                       ▼ en el llavero del SO (ScadLab / master-key, base64)
salt: <datos>/vault.salt (16 bytes, 0600)      verificador: vault_meta (un texto conocido cifrado con la llave)
```

- **La contraseña y la llave nunca están en la base** (`TestVaultSecretsNeverLeak` las busca,
  y sus codificaciones base64/hex, en el `.db`, el WAL, los errores y el JSON de los
  bindings) **ni en un log ni en un error** (`TestVaultDoesNotLogSecrets`). La contraseña
  solo entra por `CreateMasterPassword`, `UnlockVault` y `ChangeMasterPassword`.
- En el llavero va la **llave derivada**, no la contraseña: quien lea el llavero no puede
  probar la contraseña en otro sitio, y la app no tiene que volver a pagar el argon2 (~0,2 s).
- `vault_meta` (migración 7): `verifier`, `verifier_nonce`, `remember`, `created_at`; una fila.
  `remember` solo dice que la llave está en el llavero; la llave nunca.
- Mínimo **8 caracteres** (`vaultgate.MinPasswordLen`, espejo en `lib/passwordStrength.ts`); el
  indicador de fuerza de la UI es una guía, el piso lo exige el backend.

## Flujos

| Situación | Qué pasa |
|---|---|
| Primera clave de API | `SaveModelServer` responde con el error de código `vault-uninitialized`; la UI abre «Crear contraseña maestra» (confirmación, fuerza, **«Recordar en este equipo» marcado**) y reintenta |
| Con «Recordar» | `Create` guarda la llave en el llavero. En cada arranque **no se toca nada**; la primera vez que algo cifra o descifra, `Vault.Key()` la lee (una lectura, serializada: 24 usos en paralelo = 1 lectura) y queda en memoria |
| Sin «Recordar» | Tras reiniciar la app está bloqueada: lo que descifra responde `vault-locked`; la UI abre «Desbloquear» y reintenta **una sola vez** (`withVault`) |
| Llavero negado o caído | Se intenta **una vez por sesión** (`down` queda marcado, no hay reintentos) y se cae a pedir la contraseña. **No se borra nada**: una negativa pasajera no apaga «recordar». Cada llamada al llavero tiene tope de 60 s |
| Entrada borrada desde fuera | `remember` pasa a falso (ya no está recordada) |
| Llave guardada que no abre esta base | Se borra del llavero y `remember` pasa a falso (se restauró otra base) |
| Servidores sin clave, o con `apiKeyEnv` | No necesitan contraseña ni llavero |

**Errores con código** (`i18n.NewCoded`, viajan entre dos U+2063; `errorCode()` en el frontend):
`vault-uninitialized`, `vault-locked`, `vault-wrong-password`. `TestModelServer` devuelve los dos
primeros **como error del binding** (el resto de sus fallos van en el resultado) para que la UI pueda pedir la
contraseña y reintentar. `SendChatMessage` falla **antes** de guardar el mensaje, así que no queda uno huérfano.

## Cambiar, olvidar, restablecer

- **Cambiar** (`ChangeMasterPassword(old, new)`): valida la vieja contra el verificador, deriva la
  llave nueva (mismo salt) y, en **una transacción** (`store.WriteVault`), cambia el verificador y re-cifra todas las
  claves. Si una no abre, no cambia nada. Con «recordar», reemplaza la llave del llavero.
- **Olvidar este equipo** (`ForgetThisDevice`): borra `ScadLab / master-key` y apaga `remember`. La
  sesión actual sigue desbloqueada; la siguiente pide la contraseña. `SetRememberDevice(true)` la vuelve a guardar (necesita la app desbloqueada).
- **Restablecer** (`ResetVault`): borra **todas** las claves, el verificador, `vault.salt` y las entradas del llavero;
  la UI lo confirma con `confirmDialog({danger:true})`. Los servidores quedan, sin clave. **Una contraseña
  olvidada no se recupera** y la interfaz lo dice en los diálogos y en el panel.
- Falta `vault.salt` con contraseña creada → `ErrSaltMissing` (explica el archivo; no dice «contraseña incorrecta»).

## Migración desde T1

Las claves cifradas con la clave de datos aleatoria `ScadLab / model-servers-key` se **re-cifran al crear la
contraseña** (en la misma transacción) y esa entrada se borra. Si la clave de datos ya no está, esas claves no se pueden
abrir: se borran y `VaultResult.lostKeys` lo cuenta para avisar. Hasta que se cree la contraseña, usar una de esas claves
responde `vault-uninitialized` (la UI abre «Crear»).

## macOS: qué se averiguó y qué se aplicó

- **mini-tools no lo resuelve ni lo documenta**: usa el mismo `go-keyring` (servicio `mini-tools-vault`), un
  `TryAutoUnlock` al arrancar que cae en silencio a la contraseña ante cualquier fallo, y solo deja escrito en
  `scripts/package-macos.sh` que el «Self-signing application» de `wails build` es una firma **ad-hoc**, no una de
  Developer ID. No encontré en su CHANGELOG, README, `claude/` ni ayuda ninguna mención a avisos repetidos del Keychain.
- **`go-keyring` v0.2.8 en macOS no enlaza Security.framework: ejecuta `/usr/bin/security`**
  (`find-generic-password`, `add-generic-password -U`, `delete-generic-password`). La entrada la crea `security`, así
  que **`security` —no ScadLab— es la aplicación de confianza de su ACL**. La firma ad-hoc nueva que cambia en cada build
  no cambia quién tiene acceso: en principio **no** debería reaparecer «ScadLab quiere acceder…» tras actualizar. *(Leído
  del código de `go-keyring` y de cómo trata el Keychain las entradas creadas por `security`; no se probó contra un
  Keychain real: la regla del repo es no tocar el del usuario en las pruebas.)*
- Aplicado: (1) **seguir con `go-keyring` y sin CGO** —llamar a `SecItem*` desde el propio binario ataría la ACL a la
  firma de cada build, que es justo lo que se quiere evitar—; (2) **una sola lectura por sesión**, perezosa, así que el
  peor caso de un aviso es uno, en el primer uso; (3) la negativa del usuario o un llavero bloqueado **no rompen ni
  borran nada**: se pide la contraseña una vez por sesión; (4) tope de 60 s por llamada, por si el diálogo del sistema
  (desbloquear el llavero de inicio de sesión) se pierde; (5) `apiKeyEnv` sigue como respaldo sin llavero; (6) la
  ayuda dice qué elegir (*Permitir siempre*) si el sistema llega a preguntar.
- Si en una Mac real aparece un aviso por build, el siguiente paso es firmar con un certificado estable (Developer ID):
  no es un cambio de código de esta fase.
- Windows (Credential Manager) y Linux (Secret Service) no preguntan; en un Linux sin Secret Service el sondeo marca el
  llavero como no disponible y la app pide la contraseña.

## Capas

```
backend/crypto/argon2.go      DeriveKey (argon2id) + KeySize; aesgcm.go: Encrypt/Decrypt/Zero
backend/vaultgate/gate.go     Gate en memoria (copias), códigos y errores tipados
backend/vaultgate/salt.go     vault.salt (0600, escritura atómica)
backend/vaultgate/remember.go Backend (Get/Set/Delete) inyectable + OSBackend (go-keyring, con plazo)
backend/vaultgate/vault.go    Vault: Create/Unlock/Change/Forget/SetRemember/Reset/Key/TryAutoUnlock/Encrypt/Decrypt
backend/store/vault.go        GetVaultMeta, WriteVault (re-cifrado atómico), ResetVault; migración 7
app_vault.go                  bindings (firmas en 33-contrato-servidores-chat.md)
app_servers.go                Encrypt al guardar, Decrypt en serverToken; lo demás sin cambios
```

Frontend: `lib/vault.ts` (`withVault`, `requestVault`, `notifyVaultChanged`), `lib/passwordStrength.ts`,
`components/vault/{MasterPasswordDialog,VaultHost,PasswordStrength,useVaultStatus}`,
`components/settings/providers/VaultPanel.tsx`. `VaultHost` va en `App.tsx` junto a `ConfirmHost`.
`withVault` envuelve `SaveModelServer`, `TestModelServer`, `ListServerModels` y `SendChatMessage`;
reintenta **una vez** tras completar el diálogo y, si se cancela, deja el error original.

## Pruebas

`backend/vaultgate/vault_test.go` y `app_vault_test.go` con `keyring.MockInit()` /
`MockInitWithError` y un `Backend` que cuenta lecturas: primera vez con «Recordar», reinicio sin
preguntar y **una lectura**, uso concurrente, sin recordar, contraseña incorrecta, llavero denegado al crear y al
arrancar (sin bucle), olvidar, cambiar (atómico), restablecer, migración de T1, `vault.salt` ausente y la búsqueda de
secretos. `backend/store/vault_test.go`: migración 7 y `WriteVault`. `pnpm test`: fuerza de la contraseña.

Prueba de humo en la app real: `SCADLAB_FAKE_KEYRING=1` (leída en `main.go`)
cambia el llavero del SO por uno en memoria (`keyring.MockInit`), junto con un
`SCADLAB_DATA_DIR` temporal. Así se prueban crear, recordar y desbloquear sin
tocar el Keychain real. Ojo: ese llavero se vacía con cada proceso, así que tras
un reinicio la llave «recordada» ya no está y la app pide la contraseña (es el
camino sin llavero, no un fallo). En el paquete `main` todo test corre con
`keyring.MockInit()` desde `TestMain` (`app_testmain_test.go`).

## Regla de sincronización

Cualquier camino nuevo que descifre una clave pasa por `Vault.Decrypt` y su binding se envuelve con `withVault`:
un binding que devuelva `vault-locked` sin que la UI lo reintente deja al usuario con un error crudo. El piso de la
contraseña está en `vaultgate.MinPasswordLen` y `MIN_PASSWORD_LEN`: se cambian juntos.
