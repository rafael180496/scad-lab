// Messages<T> convierte cada string literal del diccionario español en
// `string`, conservando la forma. en.ts se declara `Messages<typeof es>`: si le
// falta una clave o sobra una, tsc falla. Es el chequeo de completitud del
// frontend; el del backend es TestMessagesAreComplete.
export type Messages<T> = {
    [K in keyof T]: T[K] extends string ? string : Messages<T[K]>
}
