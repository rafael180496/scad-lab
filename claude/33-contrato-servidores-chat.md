# Contrato Go ↔ React: servidores de modelos, llavero y chat

Parte de [06-go-react-contract.md](06-go-react-contract.md) (índice del contrato). Fuente: `app_servers.go`, `app_vault.go`,
`app_chat.go`, `app_chat_attach.go`, `app_chat_cli.go`.

## Servidores de modelos — `app_servers.go` (S12, 12.1)

Ollama, compatibles con OpenAI (OpenRouter, Gemini, LM Studio, vLLM…) y Anthropic, varios por tipo, en la tabla
`model_servers` ([07-database.md](07-database.md)). La clave de API se cifra con AES-256-GCM con la llave derivada de la contraseña
maestra (`backend/vaultgate`, ver la sección siguiente); **nunca** cruza hacia el frontend (`hasToken`) ni aparece en un log o error.
Detalle: [19-hermes.md](19-hermes.md).

| Binding | Devuelve | Nota |
|---|---|---|
| `ListModelServers()` | `[]store.ModelServer{id, kind, preset, name, baseUrl, apiKeyEnv, hasToken, defaultModel, contextTokens, enabled, sortOrder, updatedAt}` | sin claves |
| `SaveModelServer(s, token, clearToken)` | `store.ModelServer` | id vacío = alta; `token` vacío conserva la guardada. Con `token` y sin contraseña maestra falla con el error de código `vault-uninitialized`; con la app bloqueada, `vault-locked` (alternativa sin contraseña: `apiKeyEnv`) |
| `DeleteModelServer(id)` | — | borra también la clave cifrada; los chats quedan legibles |
| `TestModelServer(s, token)` | `ServerProbe{ok, status, message, models, modelIds[], latencyMs}` | sin guardar; un servidor caído **no** es error del binding (sí lo son `vault-locked` / `vault-uninitialized` si la clave guardada no se puede abrir) |
| `ListServerModels(serverId, refresh)` | `[]ModelInfo{id, label, description, vision, visionKnown, tools, toolsKnown, contextTokens, promptPricePerM, completionPricePerM}` | caché de 5 min por servidor; `…Known=false` = «no se sabe» (no bloquea, solo avisa) |
| `ListProviderModels(providerId)` | `[]string` | alias de S5: los ids de `ListServerModels` (o de los modelos del CLI si es `cli:*`) |
| `SetLastModel(workspaceId, serverId, model)` / `GetLastModel(workspaceId)` | — / `LastModel{serverId, model}` | por proyecto y global; acepta `cli:*`; cae al global y a «ninguno» si el servidor ya no existe o el CLI ya no está instalado |

### Llavero propio — `app_vault.go` (S12, 12.10)

Detalle y flujos: [21-llavero.md](21-llavero.md). La contraseña solo entra por `CreateMasterPassword`, `UnlockVault` y
`ChangeMasterPassword`; ni ella ni la llave salen nunca de Go. Reemplaza a `KeyringStatus()` de T1 (ya no existe).

| Binding | Devuelve | Nota |
|---|---|---|
| `VaultStatus()` | `VaultStatus{initialized, unlocked, remembered, keyringAvailable, keyringDetail}` | **no** lee la llave recordada (con `remembered` puede haber `unlocked=false` hasta el primer uso); sondea el llavero una vez por sesión |
| `CreateMasterPassword(password, remember)` | `VaultResult{rememberFailed, detail, lostKeys}` | mínimo 8 caracteres; solo si no hay una (`ErrAlreadyInitialized`); re-cifra las claves de T1 y borra su entrada; `remember` guarda la **llave derivada**. Que el llavero no deje no es error: `rememberFailed=true` |
| `UnlockVault(password, remember)` | `VaultResult` | error de código `vault-wrong-password`; `remember` enciende o apaga la entrada del llavero |
| `ChangeMasterPassword(oldPassword, newPassword)` | `VaultResult` | re-cifra todas las claves en una transacción; `vault-wrong-password` si la vieja no es |
| `ForgetThisDevice()` | — | «Olvidar este equipo»: borra la entrada del llavero; la sesión actual sigue desbloqueada |
| `SetRememberDevice(remember)` | — | encender necesita la app desbloqueada (`vault-locked`); apagar = `ForgetThisDevice` |
| `ResetVault()` | `number` | cuántas claves se borraron; borra claves, verificador, `vault.salt` y entradas del llavero |

Códigos de error (`errorCode(e)` en el frontend, `lib/vault.ts`): `vault-uninitialized` (abrir «Crear contraseña maestra»),
`vault-locked` (abrir «Desbloquear») y `vault-wrong-password`. Los devuelven `SaveModelServer` (con token),
`TestModelServer`, `ListServerModels` y `SendChatMessage` (esta última **antes** de guardar el mensaje);
`withVault()` abre el diálogo y reintenta la llamada una sola vez.

## Chat con Hermes — `app_chat.go`, `app_chat_attach.go` (S12, 12.4–12.6)

Tabla completa del chat (con `NewChat`, `ListChats`…) en [09-agents-ai.md](09-agents-ai.md#chat-con-modelos--app_chatgo-s5-s12).

| Binding | Devuelve | Nota |
|---|---|---|
| `SendChatMessage(chatId, text, currentFile, attachmentIds, opts)` | `store.ChatMessage` (el del usuario) | `opts` = `SendOptions{agent?, maxSteps}` (`maxSteps` 1–30, 0 = 12). **El modo sale de la conversación** (`chat.mode`, ver `SetChatMode`); `agent` es un override opcional (`*bool`) **solo de ese turno** —`false` = modo texto de S5, `true` = herramientas— y no cambia lo guardado; la interfaz no lo manda salvo cuando el modelo no llama herramientas. Texto vacío solo con adjuntos. Falla **antes** de guardar nada si el servidor no responde o una imagen va a un modelo que informó no ver |
| `AddChatAttachment(workspaceId, path)` | `hermes.Attachment{id, kind, name, mime, size, thumbDataUrl, refPath, summary}` | `kind` `image` (≤ 5 MB) o `model` (STL/3MF/OBJ ≤ 64 MB, copiado a `refs/` y analizado); por contenido |
| `AddChatAttachmentData(workspaceId, name, dataBase64)` | `hermes.Attachment` | pegar / arrastrar; acepta data URL; `name` solo aporta nombre y extensión |
| `ChooseChatAttachment(workspaceId)` | `[]hermes.Attachment` | diálogo nativo; si uno falla no queda ninguno de la tanda y el error nombra el archivo |
| `ChooseChatImages(workspaceId)` | `[]hermes.Attachment` | S15 («Crear desde una imagen»): lo mismo pero el diálogo ofrece solo fotos y un modelo 3D que se cuele se rechaza (no queda ninguno de la tanda) |
| `RemoveChatAttachment(id)` | — | borra lo que se creó (la copia en `refs/` solo si la hizo el adjunto); no es error quitar uno que ya no está |
| `RenameChat(chatId, title)` | — | recorta y colapsa espacios, máx. 120 letras, no vacío |
| `SetChatMode(chatId, mode)` | — | `mode`: `"agent"` (también `""`) o `"ask"`; se guarda en `chats.mode` y vale **solo para esa conversación** (no cambia otra ni deja un predeterminado: toda conversación nueva nace en Agente). Un modo desconocido o un chat inexistente fallan sin tocar nada. Un turno en curso termina con el modo con que empezó |
| `SetChatModel(chatId, serverId, model)` | — | cambia servidor y modelo a mitad de camino (modelo vacío = el del servidor); también es el último elegido del proyecto |
| `GetChat(chatId)` | `ChatView{chat, messages, running, partial, partialSeq, steps[], usage}` | `steps` y `usage` son los del turno en curso |

**S15**: `SendOptions` ganó `imagesAsFiles` (`bool`, opcional): las fotos adjuntas no viajan como imagen ni pasan por el chequeo de visión, solo quedan como archivos en
`refs/images/` (el «Usar trazado de contorno» del aviso sin visión; un CLI lo ignora). Al enviar, cada foto se copia a `<ws>/refs/images/<nombre>-<hash8>.<ext>` y su ficha
guardada (`hermes.AttachmentMeta`, en los `parts` del mensaje) lleva esa ruta en `refPath`; el chip pendiente (`hermes.Attachment`) no la tiene hasta enviar. Ver [28-modelo-desde-imagen.md](28-modelo-desde-imagen.md).

`store.Chat` ganó `mode` (S12.c: `""` = Agente, `"ask"` = Solo consulta; viaja en `ListChats`, `GetChat` y `NewChat`). `store.ChatMessage` ganó `parts` (JSON: adjuntos del usuario o pasos del asistente), `inputTokens`, `outputTokens` y `costUsd`.

## CLIs como proveedores del chat — `app_chat_cli.go`, `app_servers_minitools.go` (S12.b, T5)

Los CLIs (Claude Code, Codex, Antigravity, OpenCode) se **autodetectan** y son un proveedor más del chat; no hay filas en `model_servers`.
Detalle, flags por modo y decisiones: [19-hermes.md](19-hermes.md#clis-como-proveedores-s12b).

```go
type ChatProvider struct { ID, Kind, Name, Vendor string; Installed bool; Path string; Remote, Enabled bool } // json camelCase
// ID "cli:<id>" | id de model_servers; Kind cli|ollama|openai|anthropic; Vendor "" en un servidor
type DetectedCLI struct { ID, Name, Vendor string; Installed bool; Path, Version string }             // ID corto: "claude"
type ProviderHealthInfo struct { OK bool; LatencyMs int64; Error string }                              // ok, latencyMs, error
type ModelCatalog struct { DefaultHint string; Models []ModelInfo }                                    // defaultHint, models
type MiniToolsServer struct { Name, Kind, BaseURL, DefaultModel string; ContextTokens int; Exists bool; Error string }
type MiniToolsImport struct { Found bool; Path string; Servers []MiniToolsServer; Imported int }

ListChatProviders() ([]ChatProvider, error)             // CLIs instalados (primero) + servidores habilitados; sin --version; detección con caché de ~30 s
RefreshChatProviders() ([]ChatProvider, error)          // lo mismo volviendo a detectar (al abrir el selector); extra del contrato
ListDetectedCLIs() ([]DetectedCLI, error)               // los cuatro, instalados o no, con --version en paralelo y cacheado; solo lectura
ProviderHealth(id string) (ProviderHealthInfo, error)   // servidor: sondeo de S12 cacheado 30 s con su latencia; CLI: ok si está instalado, latencia 0
ChatModelCatalog(id string, refresh bool) (ModelCatalog, error) // CLI: modelos del propio CLI (sin «Por defecto»: la pone la UI con defaultHint)
ImportMiniToolsServers(dryRun bool) (MiniToolsImport, error)    // dryRun=true no escribe; false crea los que no existen, sin clave
```

`NewChat`, `SendChatMessage`, `SetChatModel`, `SetLastModel`/`GetLastModel` y `ListProviderModels` aceptan ids `cli:*`. Con un CLI,
el modo es el de la conversación (`chat.mode`; `SendOptions.Agent`, si viene, lo fuerza en ese turno) y `maxSteps` se ignora; los eventos son los de S12 (`chat:delta`,
`chat:step`, `chat:usage`, `chat:done`, `chat:error`). `store.Chat` gana `cliSession` (el id de conversación del CLI).
Un proveedor caído o un CLI ausente **no** son error de `ProviderHealth`: viajan con `ok=false` y el motivo en `error`.
