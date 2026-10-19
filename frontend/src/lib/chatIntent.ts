// ¿El mensaje pide CREAR o CAMBIAR un modelo? En «Solo consulta» el chat no crea
// nada, así que quien escribe «hazme una caja» merece saber —antes de enviar— que
// ahí el modelo solo propone código, y poder pasar a «Agente» con un clic (S12.c).
//
// Es una heurística barata de palabras, no un clasificador: un falso positivo solo
// muestra un aviso que se descarta; un falso negativo deja el aviso fuera, que es
// lo que había. Se inclina a no molestar: una pregunta («¿cómo hago…?») no cuenta.

// Palabra completa, también con tildes y ñ (el \b de JavaScript no las ve como letras).
const word = (alts: string, after = '') => new RegExp(`(?<!\\p{L})(?:${alts})(?!\\p{L})${after}`, 'iu')

// Verbos en imperativo o infinitivo que piden construir o modificar una pieza.
const CREATE_ES = word(
    'crea|crear|crees|diseña|diseñar|diseñes|haz|hazme|hazlo|hacer|hagas|genera|generar|modela|modelar|dibuja|dibujar|construye|construir|arma|armar|fabrica|agrega|agregar|añade|añadir|modifica|modificar|cambia|cambiar|ajusta|ajustar|corrige|corregir|copia|copiar|pon|poner|quita|quitar|elimina|eliminar|reemplaza|reemplazar|mueve|mover|escala|agranda|reduce|aumenta',
)
const CREATE_EN = word('create|design|make|build|generate|model|draw|add|change|modify|adjust|fix|copy|remove|replace|move|scale|resize|enlarge|shrink|increase|reduce|put')
// Un pedido «nominal»: «Una caja con tapa para un Raspberry Pi 5» (lo que escribe quien no sabe qué verbo usar).
const NOUN_PHRASE = /^\s*(?:un|una|unos|unas|a|an)\s+\p{L}+/iu
// «quiero/necesito un …» sin verbo de acción.
const WANT_ES = word('quiero|necesito|quisiera|me hace falta|me gustaría', '\\s+(?:un|una|unos|unas|el|la|los|las)(?!\\p{L})') // i18n-ignore: palabras clave para detectar el pedido, no texto de interfaz
const WANT_EN = word("i want|i need|i'd like|we need", '\\s+(?:a|an|the|some)(?!\\p{L})')
// Una pregunta de curiosidad o de explicación: no pide construir nada.
const QUESTION = new RegExp('^\\s*[¿?]?\\s*(?:cómo|qué|por qué|cuál|cuáles|cuánto|dónde|how|what|why|which|where|can you explain|explain|explica)(?!\\p{L})', 'iu')

/** Verdadero si el texto parece pedir que se cree o se cambie un modelo. */
export function asksToCreate(text: string): boolean {
    const t = text.trim()
    if (t.length < 4 || QUESTION.test(t)) return false
    return CREATE_ES.test(t) || CREATE_EN.test(t) || WANT_ES.test(t) || WANT_EN.test(t) || NOUN_PHRASE.test(t)
}
