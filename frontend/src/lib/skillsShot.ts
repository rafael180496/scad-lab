// Solo el banco de capturas (src/uishot.tsx): deja Configuración → Skills en el
// estado a fotografiar —el editor abierto, la confirmación de borrado, el
// cambio de carpeta— sin simular clics. En la app real nunca se escribe.
export const skillsShot: {mode?: 'editor' | 'editor-errors' | 'editor-builtin' | 'delete' | 'folder' | 'rename'} = {}
