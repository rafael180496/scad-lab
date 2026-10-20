# Formato 3MF mínimo (para generarlo en Go)

Referencia para que la app ensamble un 3MF multicolor a partir de mallas (STL)
exportadas por color. Basado en la especificación 3MF Core 1.x
(3mf.io). Lo específico de cada slicer está en las secciones 5 a 7 (qué lee cada
uno: el de Bambu/Orca, el de PrusaSlicer y el de Cura); lo marcado «verificar» no
se probó con la aplicación real (regla 9 de ScadLab: ninguna prueba abre un slicer).

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

## 5. Familia Bambu / Orca: `model_settings.config` (formato `3mf`)

- Un `<item>` cuyo objeto tiene `<components>` → **un objeto con varias partes**
  (comportamiento observado en PrusaSlicer/Bambu Studio; confirmar con la
  versión actual).
- Varios `<item>` → varios objetos independientes en la placa.
- Lo leen **Bambu Studio**, **OrcaSlicer**, **Creality Print** y **Anycubic Slicer
  Next** (todos comparten el lector `bbs_3mf.cpp`); ElegooSlicer y Snapmaker Orca son
  forks de Orca. El `part id` del config tiene que ser el `id` del objeto-malla que
  referencia el `<component>`. Un XML inválido en este archivo es error fatal en
  Creality Print y Anycubic. `Application` no debe empezar por `BambuStudio-` ni
  `OrcaSlicer-`: con ese prefijo se trata como proyecto propio y exige un
  `project_settings.config` completo.
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

## 6. PrusaSlicer: `Slic3r_PE_model.config` (formato `3mf-prusa`)

PrusaSlicer **no lee** `model_settings.config`. Lee `Metadata/Slic3r_PE_model.config`
(siempre, sin importar quién generó el 3MF) y trata un objeto sin geometría con
`<components>` como un alias: cada componente se vuelve un **objeto aparte**. Para
tener **un objeto con N partes** hay que escribir **una sola malla** y describir las
partes como volúmenes con rangos de triángulos:

```xml
<!-- 3D/3dmodel.model: UN objeto, UNA malla con los vértices y triángulos de
     todas las partes uno tras otro; sin <components> ni <basematerials> -->
<object id="1" name="placa" type="model"><mesh> … </mesh></object>
<build><item objectid="1" transform="… tx ty tz" printable="1"/></build>

<!-- Metadata/Slic3r_PE_model.config -->
<config>
 <object id="1" instances_count="1">
  <metadata type="object" key="name" value="placa"/>
  <metadata type="object" key="extruder" value="1"/>
  <volume firstid="0" lastid="11">                   <!-- triángulos 0..11 de la malla -->
   <metadata type="volume" key="name" value="Color 1 (#FFFFFF)"/>
   <metadata type="volume" key="volume_type" value="ModelPart"/>
   <metadata type="volume" key="extruder" value="1"/> <!-- 1-based -->
  </volume>
  <volume firstid="12" lastid="29"> … extruder 2 … </volume>
 </object>
</config>
```

- `firstid`/`lastid` son índices de **triángulo** de la malla del objeto, inclusivos,
  con `firstid <= lastid` y `lastid < nº de triángulos`. En ScadLab los rangos son
  contiguos y cubren la malla entera, en el orden de los extrusores.
- Los índices de vértice de cada parte se desplazan por los vértices anteriores
  (`backend/threemf/write_prusa.go`, `concatParts`).
- No lleva `slic3rpe:Version3mf`: sin él PrusaSlicer trata el archivo como «de otro
  fabricante» (que es lo que es) y no espera los metadatos de uno propio.
- El color del filamento lo da el perfil del usuario, no el archivo.
- Leído del código de PrusaSlicer 2.9 (`src/libslic3r/Format/3mf.cpp`); **no probado
  con la aplicación**.

## 7. Cura: `extruder_nr` por objeto

Cura (libSavitar) importa cada `<component>` como un nodo hijo de un grupo y lee los
ajustes del nodo de `<metadatagroup><metadata name="…">`. `extruder_nr` es **0-based**
y se aplica por objeto. ScadLab sabe escribirlo (dentro de cada objeto-parte y **antes**
del `<mesh>`, orden del esquema 3MF), pero **el `3mf` exportado no lo lleva**: está
apagado hasta probarlo a mano. Hoy, para Cura, exporta `stl-per-color`. Formato de
referencia:

```xml
<object id="2" name="Color 1 (#FFFFFF)" type="model" pid="1" pindex="0">
 <metadatagroup><metadata name="extruder_nr">0</metadata></metadatagroup>
 <mesh> … </mesh>
</object>
```

Bambu Studio, Orca y Creality Print no tienen un manejador para `<metadatagroup>`:
por lo que se vio en su código lo ignoran (el `<metadata>` anidado cae en el manejador
de los metadatos del modelo, que solo guarda un dato más). Leído del código de Cura y
libSavitar; **no probado con Cura ni con los slicers de la familia Orca**. Si
una parte no llega con su extrusor, el camino seguro es el STL por color.

## 8. Notas de implementación en Go

- `archive/zip` + `encoding/xml` (o escritura directa con `bufio.Writer` para
  mallas grandes: millones de vértices no caben cómodos en structs).
- Floats: `strconv.AppendFloat(buf, v, 'f', -1, 32)`; nunca notación
  científica ni coma decimal dependiente de locale.
- STL binario: cabecera 80 bytes + `uint32` de triángulos + 50 bytes por
  triángulo (normal, 3 vértices `float32` little-endian, `uint16` atributo).
  STL ASCII empieza con `solid` (pero hay binarios que también: validar por
  tamaño `84 + 50*n`).
- Un grupo de color = un `<base>`; un STL por color = un objeto-parte.
- Validar el resultado con las pruebas de `backend/threemf` y `backend/exporter`
  (zip y XML válidos, rangos de triángulos contiguos que cubren la malla, volumen
  total igual al del STL con OpenSCAD real). **No se abre ningún slicer** en las
  pruebas: el usuario prueba los archivos a mano.
