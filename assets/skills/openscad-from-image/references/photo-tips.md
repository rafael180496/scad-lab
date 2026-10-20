# Cómo pedir fotos que se puedan usar

Cuando el usuario va a sacar la foto (o puede sacar otra), pídele esto. Una foto
buena vale más que diez correcciones del modelo.

1. **Fondo liso** y de un color distinto al de la pieza (una hoja blanca, una
   mesa lisa). Es lo que permite separar la pieza y calcular el `silhouetteIoU`.
2. **De frente, no en diagonal.** La cámara perpendicular a la cara que se
   quiere copiar, con la pieza centrada. Si se puede, un poco alejada y con el
   zoom (así se reduce la perspectiva) en vez de pegada a la pieza.
3. **Un objeto de escala en el mismo plano**: una moneda o una tarjeta puesta
   junto a la pieza, a la misma distancia de la cámara. No encima ni delante.
4. **Una foto por vista** cuando la pieza no es plana: frente, costado y arriba,
   con la misma pieza y la misma escala de referencia.
5. **Luz pareja**, sin sombras duras ni reflejos sobre la pieza; las sombras se
   cuentan como parte del objeto.
6. **Un boceto a mano**: en papel blanco, trazo oscuro y con las cotas escritas
   grandes y con la unidad.
7. Si el usuario conoce **una medida** (el ancho, el alto, el diámetro de un
   agujero), que la diga: vale más que cualquier cálculo con la foto.

Qué hacer si la foto que llegó no cumple: úsala igual, di qué límites tiene
(«la foto está en perspectiva: las medidas tienen ~ 10 % de error») y pide una
mejor solo si el resultado no converge.
