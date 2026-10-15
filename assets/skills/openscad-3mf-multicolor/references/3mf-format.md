# Formato 3MF mínimo (para generarlo en Go)

Referencia para que la app ensamble un 3MF multicolor a partir de mallas (STL)
exportadas por color. Basado en la especificación 3MF Core 1.x
(3mf.io). Lo específico de Bambu Studio está marcado «verificar».

## 1. Estructura del paquete

Un 3MF es un **ZIP** (OPC, Open Packaging Conventions):

```
modelo.3mf
├── [Content_Types].xml        tipos MIME por extensión
├── _rels/.rels                relación raíz → modelo 3D
└── 3D/3dmodel.model           XML con mallas, materiales y build
   (opcional)
├── Metadata/thumbnail.png     miniatura (requiere relación propia)
└── Metadata/model_settings.config   ajustes de Bambu Studio «verificar»
```

- Nombres de entrada en el ZIP **sin** barra inicial (`3D/3dmodel.model`).
- Compresión Deflate o Store; ambos válidos.
- Recomendado escribir `[Content_Types].xml` primero.

## 2. `[Content_Types].xml`

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/>
  <Default Extension="png" ContentType="image/png"/>
</Types>
```

## 3. `_rels/.rels`

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rel0" Target="/3D/3dmodel.model"
    Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>
  <!-- opcional -->
  <Relationship Id="rel1" Target="/Metadata/thumbnail.png"
    Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/thumbnail"/>
</Relationships>
```

Aquí el `Target` **sí** lleva barra inicial (es una URI dentro del paquete).

## 4. `3D/3dmodel.model`

Un objeto padre con una parte por color (componentes), cada parte con su
material base:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xml:lang="en-US"
       xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">
  <metadata name="Application">ScadLab</metadata>
  <metadata name="Title">nameplate</metadata>
  <resources>
    <basematerials id="1">
      <base name="base_color" displaycolor="#FFFFFFFF"/>
      <base name="text_color" displaycolor="#D32F2FFF"/>
    </basematerials>

    <object id="2" type="model" name="base" pid="1" pindex="0">
      <mesh>
        <vertices>
          <vertex x="0" y="0" z="0"/>
          <vertex x="90" y="0" z="0"/>
          <!-- ... -->
        </vertices>
        <triangles>
          <triangle v1="0" v2="2" v3="1"/>
          <!-- ... -->
        </triangles>
      </mesh>
    </object>

    <object id="3" type="model" name="text" pid="1" pindex="1">
      <mesh> ... </mesh>
    </object>

    <object id="4" type="model" name="nameplate">
      <components>
        <component objectid="2"/>
        <component objectid="3"/>
      </components>
    </object>
  </resources>
  <build>
    <item objectid="4"/>
  </build>
</model>
```

### Reglas de la malla

- `vertex` con coordenadas en la unidad declarada (`millimeter`). Decimales con
  punto, sin separador de miles.
- `triangle v1 v2 v3`: índices **base 0** dentro del `<vertices>` del mismo
  objeto. Orden **antihorario visto desde fuera** (normal saliente, regla de la
  mano derecha). Los STL de OpenSCAD ya vienen orientados así: conservar el
  orden de los vértices de cada faceta.
- La malla debe ser cerrada (manifold) y sin triángulos degenerados.
- Deduplicar vértices al leer el STL (STL repite cada vértice por faceta):
  mapa `[3]float32 → índice`. Reduce el tamaño ~6× y Bambu Studio lo agradece.
- Un objeto con `<mesh>` o con `<components>`, nunca ambos.
- Los `id` son únicos en todo `<resources>` (materiales y objetos comparten
  espacio de ids) y un recurso se define **antes** de ser referenciado.

### Color / material

- `<basematerials>` pertenece al core. Cada `<base>` tiene `name` y
  `displaycolor` sRGB `#RRGGBB` o `#RRGGBBAA`.
- Un objeto toma su material con `pid` (id del grupo) + `pindex` (índice base 0
  dentro del grupo). También se puede asignar por triángulo
  (`<triangle ... pid="1" p1="1"/>`).
- Alternativa del Materials Extension: `<m:colorgroup>` con
  `xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`.
  Para ScadLab basta `basematerials`.

### Transformaciones

`<component objectid="2" transform="m00 m01 m02 m10 m11 m12 m20 m21 m22 m30 m31 m32"/>`
(matriz 3×4, la última fila es la traslación). Para piezas exportadas desde el
mismo `.scad` no hace falta: comparten origen. En `<item>` se puede usar
`transform` para centrar el objeto en la placa (Bambu Studio igual lo
recoloca).

## 5. Cómo lo abre Bambu Studio «verificar»

- Un `<item>` cuyo objeto tiene `<components>` → **un objeto con varias partes**
  (comportamiento observado en PrusaSlicer/Bambu Studio; confirmar con la
  versión actual).
- Varios `<item>` → varios objetos independientes en la placa.
- Bambu Studio no siempre asigna filamentos a partir de `displaycolor`; el
  mapeo fiable es su archivo propio `Metadata/model_settings.config`:

  ```xml
  <?xml version="1.0" encoding="UTF-8"?>
  <config>
    <object id="4">
      <metadata key="name" value="nameplate"/>
      <metadata key="extruder" value="1"/>
      <part id="2" subtype="normal_part">
        <metadata key="name" value="base"/>
        <metadata key="extruder" value="1"/>
      </part>
      <part id="3" subtype="normal_part">
        <metadata key="name" value="text"/>
        <metadata key="extruder" value="2"/>
      </part>
    </object>
  </config>
  ```

  `extruder` = número de filamento (1 = primer slot). Formato de referencia:
  abrir un 3MF guardado por Bambu Studio y copiar su estructura «verificar»
  (los proyectos de Bambu Studio usan además Production Extension con mallas en
  `3D/Objects/*.model`; no es obligatorio para importar).

## 6. Notas de implementación en Go

- `archive/zip` + `encoding/xml` (o escritura directa con `bufio.Writer` para
  mallas grandes: millones de vértices no caben cómodos en structs).
- Floats: `strconv.AppendFloat(buf, v, 'f', -1, 32)`; nunca notación
  científica ni coma decimal dependiente de locale.
- STL binario: cabecera 80 bytes + `uint32` de triángulos + 50 bytes por
  triángulo (normal, 3 vértices `float32` little-endian, `uint16` atributo).
  STL ASCII empieza con `solid` (pero hay binarios que también: validar por
  tamaño `84 + 50*n`).
- Un grupo de color = un `<base>`; un STL por color = un objeto-parte.
- Validar el resultado abriéndolo en Bambu Studio y, si se puede, con una
  librería de referencia (lib3mf) en los tests.
