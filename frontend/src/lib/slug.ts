// slugify imita a workspace.Slug (Go) para mostrar, antes de guardar, cómo va
// a quedar el nombre del archivo. El número lo asigna Go al crear el archivo.
export function slugify(label: string): string {
    const s = label
        .toLowerCase()
        .normalize('NFD')
        .replace(/[̀-ͯ]/g, '')
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '')
    return (s.length > 60 ? s.slice(0, 60).replace(/-+$/, '') : s) || 'model'
}
