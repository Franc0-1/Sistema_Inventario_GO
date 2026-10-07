# Arquitectura del backend – Sistema de Inventario

API REST en Go (`net/http`) que usa `data/inventario.xlsx` como almacenamiento mediante `excelize`.
Los usuarios **nunca** tocan el Excel: todo pasa por web → handlers → services → repository → Excel.

| Etapa | Estado |
|---|---|
| 1 – Arquitectura | ✅ Rutas, capas, validación de entrada, mapeo de errores |
| 2 – Repository de Excel (lectura) | ✅ `GetAll`, `GetByID`, `Search`, apertura segura, esquema, tests |
| 3 – Repository de Excel (escritura) | ✅ `Create`, `Update`, `Delete`, `UpdateStock`, IDs sin reutilización, concurrencia |
| 4 – Capa de servicio | ✅ `InventoryService`: validación, normalización, unicidad, inyección del repositorio |
| 5 – Capa HTTP | ✅ 7 endpoints de `/api/inventory`, errores JSON, CORS configurable |
| 6 – Frontend conectado | ✅ Listado, búsqueda, alta, edición, stock y baja contra la API |
| 7 – Movimientos e historial | ✅ Hoja `Movimientos`, `UpdateStock` atómico con su movimiento, `/api/movements`, historial en la interfaz |
| 8 – Persistencia segura | ✅ Respaldo previo, temporal validado, reemplazo atómico, validación de integridad, `/api/health` |
| 9 – Consulta avanzada | ✅ Filtros combinables, orden con lista blanca, paginación, respuesta `{items, pagination}`, frontend paginado |
| 10 – Movimientos de inventario | ✅ Entrada, salida (sin superar el stock), ajuste y transferencia por `PATCH …/stock` con `tipo`; el repositorio rechaza movimientos que no coinciden con el cambio guardado |

### Resumen y reportes (Etapa 13)

- `GET /api/inventory/summary`, sin parámetros, devuelve `{data: {registros_activos,
  registros_baja, unidades_activas, equipos_disponibles, equipos_prestados,
  registros, unidades, ubicaciones, tipos, condiciones, movimientos}}`.
  Activo significa `Status != BAJA`. Las unidades activas suman `Quantity`.
  Disponible/prestado cuenta registros activos con `HasInventory=true` según
  `Availability`, independientemente de la condición operativa.
- Las distribuciones del resumen incluyen bajas. Cada grupo contiene
  `{valor, registros, unidades}`. Las etiquetas equivalentes según `FoldText`
  se agrupan; se elige la etiqueta lexicográficamente menor y se ordena por
  etiqueta normalizada. Los valores vacíos se muestran como «Sin especificar».
- `movimientos` contiene los últimos 10, por `CreatedAt` descendente y luego
  ID descendente; puede incluir historial de ítems eliminados.
- `GET /api/inventory/reports?location=...&deviceType=...&includeRetired=true`
  devuelve `{data: {registros, unidades, ubicaciones, tipos, condiciones}}`.
  Los filtros se combinan en el repositorio antes de agrupar. Por defecto se
  excluyen bajas. No acepta otros parámetros, valores repetidos ni booleanos
  inválidos (400 `INVALID_QUERY`). Los conjuntos vacíos tienen totales cero y
  listas `[]`. Los errores internos conservan el mapeo HTTP existente.
- Los cálculos residen en services. El resumen lee inventario e historial una
  vez bajo `writeMu` para evitar escrituras de la aplicación entre ambas
  lecturas; cada reporte hace una sola búsqueda. No se escribe almacenamiento.
- Las vistas web Resumen/Reportes descartan respuestas antiguas y se invalidan
  tras mutaciones o importaciones exitosas. Al regresar al inventario normal
  se conserva su consulta y página. Los enlaces del resumen abren consultas
  de todas las categorías; su alcance adicional se muestra y puede quitarse.

### Escritura segura (Etapa 8)

Toda escritura (`Create`, `Update`, `Delete`, `UpdateStock`, `CreateMovement`) pasa por `write()`,
bajo un único `Lock`:

```
abrir el original → validateWorkbook (estructura + datos)
→ modificar en memoria
→ respaldo: data/backups/inventario_AAAA-MM-DD_HH-MM-SS.xlsx (con _2, _3… si coincide el segundo; nunca sobrescribe)
→ guardar en un temporal único junto al original (data/.inventario.xlsx.<aleatorio>.tmp)
→ reabrir el temporal y validateWorkbook
→ reemplazar el original (rename) → borrar el temporal
```

- Si falla el respaldo (`ErrBackupFailed`), el guardado o la validación del temporal (`ErrSaveFailed`),
  o el reemplazo (`ErrReplaceFailed`), el original queda intacto y la operación devuelve error. El
  temporal se borra siempre.
- El respaldo se hace después de modificar en memoria y antes de tocar el disco. Así, las operaciones
  rechazadas (ítem inexistente, conflicto) no generan copias.
- Los respaldos no se eliminan automáticamente (la retención queda para una etapa futura).

**Validación** (`repository/validation.go`, solo detecta problemas, nunca repara):

| Nivel | Qué comprueba | Cuándo |
|---|---|---|
| `validateStructure` | Hojas `Inventario` y `Movimientos`; encabezados exactos (Inventario: A–M, más la extensión N–P de préstamos) | Cada lectura, `/api/health` |
| `validateWorkbook` | Lo anterior, más cada fila. **Inventario:** ID > 0 y único, Quantity ≥ 0, coherencia HasInventory/InventoryNumber, CreatedAt y UpdatedAt presentes y válidos. **Movimientos:** ID > 0 y único, ItemID válido, tipo conocido, cantidades ≥ 0, CreatedAt válido | Antes de cada escritura y sobre el temporal |

Las lecturas no hacen la validación completa: un dato inválido no debe impedir consultar el
inventario para corregirlo. Lo informan las escrituras (que se niegan) y el log.

**Referencias de movimientos:** un `ItemID` es válido si el ítem existe **o existió**, es decir, si es
≤ al mayor ID de ítem asignado alguna vez (nombre definido `UltimoIDInventario`). Así se conserva el
historial de ítems eliminados (Etapa 7). Un ID que nunca se asignó es `ErrInvalidMovementReference`.

### Consulta avanzada (Etapa 9)

`GET /api/inventory` y `GET /api/inventory/search` son la misma consulta (`handler.list`). Cada capa
hace una sola cosa:

```
handler   parseItemQuery: solo formato (parámetro conocido y no repetido, enteros ≥ 1, true/false) → models.ItemQuery
service   Query: listas blancas y rangos (ErrInvalidQuery) → repo.Search(filtro) → ordenar → paginar, en memoria
repo      Search: una lectura del Excel bajo RLock, filtra fila por fila (itemMatcher)
```

`models.ItemQuery = {Filter ItemFilter, Sort, Order, Page, PageSize}`. No se pagina leyendo el
archivo por partes: se lee una vez, se filtra, se ordena y se recorta.

**Filtros** (se combinan con Y; vacío = no filtra). Todos ignoran mayúsculas, tildes y espacios de más
(`utils.FoldText`):

| Parámetro | Campo | Comparación |
|---|---|---|
| `q` | InventoryNumber, Brand, Model, DeviceType, SerialNumber | parcial (búsqueda general) |
| `deviceType` · `excludeDeviceType` · `location` | DeviceType · DeviceType · Location | exacta |
| `brand` · `model` · `inventoryNumber` · `serialNumber` | ídem | parcial |
| `status` | Status (`OPERATIVO`, `EN_REPARACION`, `FUERA_DE_SERVICIO`, `BAJA`) | exacta, lista blanca |
| `availability` | Availability (`DISPONIBLE`, `PRESTADO`) | exacta, lista blanca |
| `hasInventory` | HasInventory | `true` / `false` |
| `includeRetired` | — | `true` incluye las bajas |

**Bajas:** se ocultan por defecto. Se muestran con `includeRetired=true` o con `status=BAJA`.
"Disponible" no es un estado sino una disponibilidad: `availability=DISPONIBLE` (`status=Disponible` → 400).
`excludeDeviceType` permite que "Equipos y stock" no incluya los aires acondicionados.

**Orden:** `sort` solo acepta la lista blanca `models.SortFields`: `id`, `inventoryNumber`, `deviceType`,
`brand`, `model`, `quantity`, `location`, `status`, `createdAt`, `updatedAt` (sin distinguir
mayúsculas). Cualquier otro valor → 400 `INVALID_QUERY`, nunca se usa como nombre de campo.
`order` = `asc` (defecto) | `desc`. Detalles: los textos se comparan sin tildes ni mayúsculas y los
vacíos van al final en ambos sentidos; `status` se ordena por gravedad (OPERATIVO → BAJA); a igual
valor desempata el ID ascendente, así las páginas no repiten ni pierden ítems.

**Paginación:** `page` (≥ 1) y `pageSize` (1–100, defecto 20). Si se envía uno solo, el otro toma su
valor por defecto; sin ninguno se devuelve todo en una página. Una página posterior a la última
responde 200 con `items: []`.

```json
{ "data": { "items": [Item],
            "pagination": { "page": 1, "pageSize": 20, "totalItems": 143, "totalPages": 8 } } }
```

`totalPages` es 0 solo si no hay resultados. Parámetro desconocido o repetido, entero inválido o
booleano inválido → 400 `INVALID_QUERY` (así un error de tipeo no devuelve resultados sin filtrar).

**Frontend:** no filtra, no ordena ni pagina localmente. `api.js` arma la query string (omite los
vacíos) y `app.js` traduce el estado de la pantalla a parámetros (`parametrosConsulta`):
- Barra de filtros (tipo, marca, área, condición) y panel "Más filtros" (modelo, N° de inventario,
  N° de serie, con/sin N°). Los textos se envían con espera de 300 ms; los selectores, al instante.
  Todo cambio vuelve a la página 1.
- Pestañas = parámetros: Disponibles (`availability=DISPONIBLE` + condición Utilizable + con N°),
  Prestados (`availability=PRESTADO`), Consumibles (`hasInventory=false`). El filtro que fija la pestaña
  se muestra deshabilitado. Las pestañas no tienen contador propio: el total lo da el contador de resultados.
- Orden por clic en las columnas Equipo (marca), Identificación, Área y Condición, o con el selector
  (todos los campos de la lista blanca) y el botón de sentido.
- Pie de paginación: rango, anterior/siguiente y 10/20/50/100 por página.
- Aires: la misma consulta con `deviceType=Aire acondicionado`, sin paginar (se agrupa por área).
- Un "catálogo" (`includeRetired=true`, sin paginar) se pide al inicio solo para las opciones de los
  selectores, las sugerencias del formulario y los contadores de sección.
- Tras crear, editar, cambiar stock o eliminar se vuelve a pedir la página actual. Si quedó fuera de
  rango (se eliminó el único ítem de la última página), se pide la última que existe.
- Mientras llega una página, la anterior se muestra atenuada y el contador dice "Buscando…". Las
  respuestas viejas se descartan.

Préstamos y categorías todavía no tienen endpoints.

---

## 1. Árbol del backend

```
inventario/
├── cmd/server/main.go                 # Composición de dependencias, timeouts, apagado ordenado
├── internal/
│   ├── handlers/                      # Capa HTTP
│   │   ├── inventory_handler.go       # InventoryHandler: rutas /api/inventory, un método por endpoint
│   │   ├── request.go                 # DTOs, Content-Type, lectura de JSON y de {id}
│   │   ├── response.go                # writeJSON/writeError, códigos y mapeo error → HTTP
│   │   ├── middleware.go              # Recuperación de panics, log de /api, CORS configurable
│   │   ├── web_handler.go             # index.html, /static, /healthz
│   │   ├── fake_service_test.go       # InventoryService falso, solo para tests
│   │   └── inventory_handler_test.go  # Tests HTTP con httptest
│   ├── services/                      # Reglas de negocio
│   │   ├── interface.go               # InventoryService
│   │   ├── inventory_service.go       # Casos de uso (reciben InventoryRepository por inyección)
│   │   ├── validation.go              # Normalización, validación y unicidad
│   │   ├── query.go                   # Query: listas blancas, orden y paginación en memoria (Etapa 9)
│   │   ├── errors.go                  # Errores de negocio
│   │   ├── fake_repository_test.go    # Repositorio en memoria, solo para tests
│   │   └── inventory_service_test.go
│   ├── repository/                    # Persistencia (solo Excel)
│   │   ├── interface.go               # InventoryRepository y demás contratos
│   │   ├── errors.go                  # ErrItemNotFound, ErrInvalidWorkbook, CellError…
│   │   ├── excel_repository.go        # Apertura segura, bloqueo, esquema, lectura, escritura atómica
│   │   ├── excel_schema.go            # Hojas, columnas, conversión fila ⇄ struct
│   │   ├── filter.go                  # Criterios de Search: exactos, parciales, sin tildes (funciones puras)
│   │   ├── excel_repository_test.go        # Lectura (Etapa 2)
│   │   ├── excel_repository_write_test.go  # Escritura y concurrencia (Etapa 3)
│   │   ├── fixture_test.go            # Genera el Excel de prueba y helpers de test
│   │   └── testdata/inventario.xlsx   # Excel de prueba (versionado)
│   ├── models/
│   │   ├── item.go                    # Item, ItemStatus, Availability, ItemFilter
│   │   ├── movement.go                # Movement, MovementType
│   │   ├── query.go                   # ItemQuery, SortFields, Pagination, ItemPage (Etapa 9)
│   │   └── category.go                # Category
│   └── utils/
│       ├── excel.go                   # Conversiones seguras de celdas (int, bool, fecha)
│       ├── text.go                    # FoldText: comparar sin mayúsculas, tildes ni espacios de más
│       └── config.go                  # Variables de entorno (puerto, Excel, CORS)
├── templates/web/                     # Frontend ya construido
├── data/inventario.xlsx               # Se crea si no existe; no se versiona
├── Dockerfile · docker-compose.yml · go.mod
```

## 2. Interfaces

```go
type InventoryRepository interface {
    GetAll(ctx) ([]models.Item, error)
    GetByID(ctx, id int) (models.Item, error)
    Search(ctx, filter models.ItemFilter) ([]models.Item, error)
    Create(ctx, item models.Item) (models.Item, error)                              // asigna ID y fechas
    Update(ctx, item models.Item, expectedVersion time.Time, mov *models.Movement) (models.Item, error) // + movimiento opcional
    Delete(ctx, id int, expectedVersion time.Time) error                                                // borra la fila; conserva el historial
    UpdateStock(ctx, id, quantity int, expectedVersion time.Time, mov models.Movement) (models.Item, error) // stock + movimiento
}

type MovementRepository interface {
    GetMovements(ctx) ([]models.Movement, error)                      // ordenados por ID
    GetMovementsByItemID(ctx, itemID int) ([]models.Movement, error)  // incluye ítems ya eliminados
    CreateMovement(ctx, mov models.Movement) (models.Movement, error) // asigna ID y fecha
}
type CategoryRepository interface { ListCategories(ctx) ([]models.Category, error) }

type Repository interface {
    InventoryRepository; MovementRepository; CategoryRepository
    Close() error
}
```

Reglas del contrato:

1. **Sin lógica de negocio.** El repositorio guarda y lee. Solo garantiza lo que garantizaría una base
   de datos: IDs únicos, escrituras atómicas, cantidad ≥ 0 y control de versión.
2. **ID, CreatedAt y UpdatedAt los asigna el repositorio.** Los valores que traiga el ítem en esos campos
   se ignoran. `UpdatedAt` siempre avanza: dos escrituras seguidas nunca comparten el mismo valor.
3. **IDs estables y nunca reutilizados.** Próximo ID = `max(mayor ID existente, último ID asignado) + 1`.
   El último asignado se guarda en el libro como el nombre definido `UltimoIDInventario` (Fórmulas →
   Administrador de nombres), sin tocar ninguna hoja. Sin él, borrar el mayor ID haría que se reutilizara.
4. **Concurrencia optimista.** `Update`, `Delete` y `UpdateStock` reciben `expectedVersion`, el `UpdatedAt`
   que el llamador leyó. Si no coincide con lo guardado, devuelven `ErrConflict` sin guardar; el llamador
   relee y reintenta.
5. **`Delete` es físico** (`RemoveRow` en `Inventario`). Para conservar un ítem como "Dado de baja", el
   servicio usa `Update` con `Status = BAJA`.
6. **Stock + movimiento en una sola escritura.** `UpdateStock` (y `Update` cuando recibe un movimiento)
   modifica `Inventario` y agrega la fila en `Movimientos` en memoria, y `write()` guarda el libro una
   única vez bajo el mismo `Lock`. Si cualquier paso falla no se guarda nada, así que nunca queda stock sin
   movimiento ni movimiento sin stock. El repositorio completa `ItemID`, `PreviousQuantity`,
   `NewQuantity` y `CreatedAt` con lo que realmente escribió. Los helpers internos (`appendMovement`,
   `idSequence`) trabajan sobre el libro ya abierto y **no** toman el mutex, lo que evita deadlocks.
7. **Los IDs de movimientos** siguen la misma regla que los de ítems (nombre definido `UltimoIDMovimiento`).

`var _ Repository = (*ExcelRepository)(nil)` hace que el compilador verifique el contrato.

### Errores del repositorio (`errors.Is`)

| Error | Cuándo |
|---|---|
| `ErrItemNotFound` | `GetByID` con un ID inexistente (o ≤ 0) |
| `ErrInvalidWorkbook` | El archivo no existe, no es `.xlsx` o tiene encabezados incompatibles |
| `ErrSheetMissing` | Falta la hoja `Inventario` o `Movimientos` |
| `ErrInvalidCell` | Una celda no se puede convertir. Es un `*CellError` con hoja, celda y campo, p. ej. `hoja "Inventario", celda H4 (Quantity): "muchos" no es un número entero` |
| `ErrConflict` | `expectedVersion` no coincide con el `UpdatedAt` guardado |
| `ErrInvalidItem` | Invariante de almacenamiento violado (cantidad negativa) |
| `ErrWriteFailed` | No se pudo guardar el libro |
| `ErrNotImplemented` | Operación de una etapa pendiente |

El servicio los devuelve sin reemplazarlos. Los handlers los identifican con
`errors.Is(err, repository.ErrItemNotFound)`: usan solo los valores de error del paquete, nunca su implementación.

### Capa de servicio (`services.InventoryService`)

```go
type InventoryService interface {
    GetAll(ctx) ([]models.Item, error)
    GetByID(ctx, id int) (models.Item, error)
    Search(ctx, filter models.ItemFilter) ([]models.Item, error)
    Query(ctx, q models.ItemQuery) (models.ItemPage, error)   // filtros + orden + página (Etapa 9)
    Create(ctx, item models.Item) (models.Item, error)
    Update(ctx, id int, item models.Item) (models.Item, error)
    Delete(ctx, id int) error
    UpdateStock(ctx, id, quantity int) (models.Item, error)
}
func NewInventoryService(repo repository.InventoryRepository) InventoryService
```

Reglas de negocio:

| Regla | Error |
|---|---|
| ID > 0 en `GetByID`, `Update`, `Delete`, `UpdateStock`; `Update` no puede cambiar el ID | `ErrInvalidID` |
| Obligatorios: `DeviceType`, `Brand`, `Model`, `Location`, `Status` (texto con solo espacios = vacío) | `ErrInvalidItem` |
| `Status` debe ser uno de `models.ValidStatuses` | `ErrInvalidItem` |
| `Quantity ≥ 0` en `Create`, `Update` y `UpdateStock` | `ErrInvalidQuantity` (envuelve `ErrInvalidItem`) |
| `HasInventory` ⇒ `InventoryNumber` obligatorio | `ErrInventoryNumberRequired` (envuelve `ErrInvalidItem`) |
| Sin `HasInventory` ⇒ `InventoryNumber` vacío | `ErrInvalidItem` |
| `InventoryNumber` único, sin distinguir mayúsculas (excluye el propio ítem; incluye las bajas) | `ErrInventoryNumberExists` |
| `SerialNumber` único si no está vacío, mismas condiciones | `ErrSerialNumberExists` |

- **Normalización:** se quitan los espacios del principio y del final de los textos. No se cambian las
  mayúsculas ni los espacios internos.
- **`Create`:** ignora `ID` y las fechas (las asigna el repositorio). Un ítem nuevo nace `DISPONIBLE` y sin préstamo.
- **`Update`:** conserva `ID`, `CreatedAt` y los datos de préstamo. El formulario de edición no los envía
  y, si no se conservaran, se borraría un préstamo activo.
- **Concurrencia:** "verificar unicidad + guardar" se ejecuta bajo un `sync.Mutex` del servicio. Así, dos
  altas simultáneas con el mismo número no pasan ambas la verificación.

## 3. Modelos

Los nombres de campo están en inglés, como pide la especificación de la Etapa 2. **Los tags JSON no
cambiaron**: son el contrato con el frontend.

```go
type Item struct {
    ID              int        `json:"id"`
    InventoryNumber string     `json:"numero_inventario"`
    HasInventory    bool       `json:"tiene_inventario"`   // true = individual, false = consumible
    DeviceType      string     `json:"tipo_dispositivo"`
    Brand           string     `json:"marca"`
    Model           string     `json:"modelo"`
    SerialNumber    string     `json:"numero_serie"`
    Quantity        int        `json:"cantidad"`
    Location        string     `json:"ubicacion"`          // "Área" en la interfaz
    Status          ItemStatus `json:"estado"`             // OPERATIVO | EN_REPARACION | FUERA_DE_SERVICIO | BAJA
    Notes           string     `json:"observacion"`
    CreatedAt       time.Time  `json:"created_at"`
    UpdatedAt       time.Time  `json:"updated_at"`         // también es la versión (concurrencia)
    // Extensión de préstamos (columnas N–P)
    Availability    Availability `json:"disponibilidad"`   // DISPONIBLE | PRESTADO
    AssignedTo      string       `json:"asignado_a"`
    LoanedAt        *time.Time   `json:"fecha_prestamo,omitempty"`
}

type Movement struct {
    ID                  int          `json:"id"`
    ItemID              int          `json:"item_id"`
    MovementType        MovementType `json:"tipo"`
    Quantity            int          `json:"cantidad"`          // magnitud del cambio (≥ 0)
    PreviousQuantity    int          `json:"cantidad_anterior"`
    NewQuantity         int          `json:"cantidad_nueva"`
    OriginLocation      string       `json:"ubicacion_origen"`
    DestinationLocation string       `json:"ubicacion_destino"`
    Notes               string       `json:"observacion"`
    CreatedAt           time.Time    `json:"created_at"`
}
// MovementType: stock_in | stock_out | stock_update | transfer | adjustment

type Category struct { ID int; Name string; Active, Loanable bool }

type ItemFilter struct {
    Text string // sin distinguir mayúsculas en InventoryNumber, Brand, Model, DeviceType, SerialNumber
    DeviceType, Location string
    Status ItemStatus; Availability Availability
    OnlyConsumables, IncludeRetired bool
}
```

**Estados.** Los cuatro estados de negocio se representan con dos campos independientes:

| Estado de negocio | `Status` | `Availability` |
|---|---|---|
| Disponible | `OPERATIVO` | `DISPONIBLE` |
| Prestado | (≠ `BAJA`) | `PRESTADO` |
| Reparación | `EN_REPARACION` / `FUERA_DE_SERVICIO` | `DISPONIBLE` |
| Baja | `BAJA` | `DISPONIBLE` |

## 4. Diseño del Excel

| Hoja | Columnas |
|---|---|
| `Inventario` | **A** ID · **B** InventoryNumber · **C** HasInventory · **D** DeviceType · **E** Brand · **F** Model · **G** SerialNumber · **H** Quantity · **I** Location · **J** Status · **K** Notes · **L** CreatedAt · **M** UpdatedAt — *extensión:* **N** Availability · **O** AssignedTo · **P** LoanedAt |
| `Movimientos` | A ID · B ItemID · C MovementType · D Quantity · E PreviousQuantity · F NewQuantity · G OriginLocation · H DestinationLocation · I Notes · J CreatedAt |

**Cuándo se registra un movimiento** (lo decide el servicio):

| Operación | Movimiento |
|---|---|
| `UpdateStock` con cantidad mayor / menor / igual | `stock_in` / `stock_out` / `stock_update`, con `Quantity` = magnitud del cambio (10 → 15 = `stock_in` de 5) |
| `ApplyMovement`: `stock_in` / `stock_out` / `adjustment` | El tipo pedido, con cantidades anterior y nueva leídas del stock real. Salida > stock → `ErrInsufficientStock` |
| `ApplyMovement`: `transfer` | Cambia solo `Location` (vía `Update`); `Quantity` = unidades trasladadas, anterior = nueva, origen y destino distintos |
| `Update` (edición) que cambia la cantidad | `adjustment` |
| Alta (`Create`) | Ninguno |
| Baja (`Delete`) | Ninguno. Los movimientos del ítem se conservan |

El servicio valida cada movimiento antes de guardarlo: tipo conocido, cantidades ≥ 0 y coherentes con el
tipo (`ErrInvalidMovement`, `ErrInvalidQuantity`).
| `Categorias` | A ID · B Name · C Active · D Loanable (hoja auxiliar para filtros y la regla de préstamo) |

**Lectura** (`utils/excel.go`: nunca hace panic; celda vacía = valor cero):

| Tipo | Acepta |
|---|---|
| int | `12`, `12.0` (Excel guarda números como decimales). Rechaza `2.5` y texto |
| bool | `SI`/`NO` (con o sin tilde), `TRUE`/`FALSE`, `VERDADERO`/`FALSO`, `1`/`0` (celdas booleanas), `X`. Sin distinguir mayúsculas |
| fecha | RFC 3339 (con o sin nanosegundos), `AAAA-MM-DD[ hh:mm:ss]`, `DD/MM/AAAA[ hh:mm]` y fechas numéricas de Excel |

- Las celdas se leen como **valor crudo** (`RawCellValue`). Así una fecha escrita desde Excel llega como
  número de serie y no como texto con el formato regional.
- Las filas vacías se ignoran.
- Una fila con datos debe tener un ID entero positivo.
- `N`–`P` son opcionales al leer: si faltan, `Availability` vale `DISPONIBLE`.
- **El encabezado se valida en cada lectura.** Si una columna está corrida, se devuelve `ErrInvalidWorkbook`
  en lugar de devolver datos equivocados sin avisar.

**Escritura**: fechas RFC 3339 con nanosegundos, booleanos `SI`/`NO`, enums en `MAYÚSCULAS`.

**Actualización de esquema al arrancar** (`NewExcelRepository`):
- Crea las hojas que falten.
- Reescribe un encabezado solo si es seguro:
  - la hoja no tiene datos;
  - al encabezado actual le faltan columnas al final;
  - el encabezado es una versión anterior con los mismos significados en el mismo orden (p. ej. `Nombre` → `Name`).
- En cualquier otro caso devuelve `ErrInvalidWorkbook` y **no modifica el archivo**.

**Concurrencia:**
- `sync.RWMutex`: las lecturas comparten el bloqueo; `write()` lo toma en exclusiva.
- Guardado atómico: se escribe `inventario.xlsx.tmp`, se fuerza a disco y se renombra sobre el original.
- Un solo proceso debe escribir el archivo, y no hay que abrirlo con Excel mientras el servicio corre.

## 5. API HTTP

Solo `net/http` y `encoding/json`, con los patrones de `http.ServeMux` (Go 1.22+) para `{id}`.

| Método y ruta | Cuerpo | Éxito | Servicio |
|---|---|---|---|
| `GET /api/inventory?…` | — | 200 `{"data": {"items": [Item], "pagination": {…}}}` (ver Etapa 9) | `Query` |
| `GET /api/inventory/{id}` | — | 200 `{"data": Item}` | `GetByID` |
| `GET /api/inventory/search?q=texto&…` | — | igual que `GET /api/inventory` (mismos parámetros) | `Query` |
| `POST /api/inventory` | `ItemRequest` | 201 `{"data": Item}` | `Create` |
| `PUT /api/inventory/{id}` | `ItemRequest` | 200 `{"data": Item}` | `Update` |
| `DELETE /api/inventory/{id}` | — | 204 sin cuerpo | `Delete` (borra la fila) |
| `PATCH /api/inventory/{id}/stock` | `{"cantidad": 10}` (fija la cantidad) o con `tipo`: `stock_in`/`stock_out` + `cantidad` (unidades), `adjustment`/`stock_update` + `cantidad` (final), `transfer` + `ubicacion_destino` (sin cantidad); `observacion` opcional | 200 `{"data": Item}` · 409 `INSUFFICIENT_STOCK` si la salida supera el stock | `UpdateStock` / `ApplyMovement` (+ movimiento) |
| `GET /api/inventory/{id}/movements` | — | 200 `{"data": [Movement]}` (vacío = `[]`; 404 si el ID no existe ni tiene historial) | `GetMovementsByItemID` |
| `GET /api/movements` | — | 200 `{"data": [Movement]}` | `GetMovements` |
| `GET /api/health` | — | 200 `{"status": "ok"}` · 503 `SERVICE_UNAVAILABLE` si el Excel no es accesible o su estructura no es válida | `CheckHealth` |

**Cuerpo de alta y edición** (`ItemRequest`). Usa los mismos nombres JSON que las respuestas:

```json
{ "numero_inventario": "1001", "tiene_inventario": true, "tipo_dispositivo": "Notebook",
  "marca": "Lenovo", "modelo": "ThinkPad T14", "numero_serie": "ABC123", "cantidad": 1,
  "ubicacion": "Sistemas", "estado": "OPERATIVO", "observacion": "Equipo nuevo" }
```

`id`, `created_at`, `updated_at` y los datos de préstamo no forman parte del DTO. Como el decodificador
rechaza campos desconocidos, enviarlos da 400. En `PUT`, el ID sale solo de la URL.

**Reglas de la capa HTTP** (sin reglas de negocio):
- `POST`, `PUT` y `PATCH` exigen `Content-Type: application/json` (se acepta `; charset=…`). Si no → 415.
- El cuerpo tiene un máximo de 64 KiB (→ 413), un único objeto JSON, y no admite campos desconocidos (→ 400).
- `{id}` debe ser un entero (→ 400 `INVALID_ID`). Si además es positivo, lo decide el servicio.
- Un método no soportado → 405 con `Allow` y el formato de error de la API. Una ruta `/api/...`
  inexistente → 404 JSON.
- CORS está desactivado por defecto: el frontend lo sirve este mismo servidor. Para otro origen:
  `INVENTARIO_CORS_ORIGINS=http://host:puerto[,…]` (nunca `*`).

**Errores:** siempre `{"error": {"code": "...", "message": "..."}}`. La traducción está solo en
`handlers/response.go`:

| Código | HTTP | Origen |
|---|---|---|
| `INVALID_JSON`, `INVALID_REQUEST`, `INVALID_ID` | 400 | formato de la petición |
| `INVALID_QUERY` | 400 | parámetros del listado (handler: formato; `services`: listas blancas y rangos) |
| `UNSUPPORTED_MEDIA_TYPE` · `REQUEST_TOO_LARGE` · `METHOD_NOT_ALLOWED` · `NOT_FOUND` | 415 · 413 · 405 · 404 | HTTP |
| `INVALID_ITEM`, `INVENTORY_NUMBER_REQUIRED`, `INVALID_QUANTITY` | 400 | `services` |
| `INVENTORY_NUMBER_EXISTS`, `SERIAL_NUMBER_EXISTS`, `INSUFFICIENT_STOCK` | 409 | `services` |
| `ITEM_NOT_FOUND` · `CONFLICT` | 404 · 409 | `repository` |
| `INTERNAL_ERROR` | 500 | cualquier otro. El detalle va solo al log; el cliente recibe un mensaje genérico |

Lectura de `GET /api/inventory/4`:

```
handler   pathID → 4 ──► service.GetByID (valida el ID) ──► repo.GetByID(4)
repo      RLock → openWorkbook (abre + verifica hojas) → f.Rows("Inventario") en streaming
          fila 1: checkHeader · por cada fila: ¿columna A == 4? → no: siguiente, sin convertir
                                                              → sí: rowToItem y deja de leer
          Close → RUnlock
handler   200 {"data": Item} | 404 ITEM_NOT_FOUND | 500 INTERNAL_ERROR (detalle en el log)
```

### Frontend (`templates/web`)

HTML, CSS y JavaScript sin frameworks, servidos por el mismo servidor Go (mismo origen, sin CORS).

| Archivo | Responsabilidad |
|---|---|
| `static/js/api.js` | **Única** capa HTTP: URL base relativa `/api/inventory`, `fetch`, lectura de `{data}` / `{error}`, `ApiError` con el `code` del backend. Solo envía los campos editables |
| `static/js/app.js` | Estado (`estado.items` es la única fuente de verdad), renderizado, filtros locales, formularios y acciones |
| `static/js/iconos.js` | Íconos SVG constantes |
| `index.html` | Estructura, diálogos de alta/edición, stock y confirmación de borrado |

- **Búsqueda:** se resuelve en el backend (`/search?q=`) con 300 ms de espera entre teclas. Si llega una
  respuesta vieja después de una nueva, se descarta. Con texto vacío se usa `GET /api/inventory`. Los
  filtros de tipo, área, condición y pestañas se aplican en el cliente sobre la lista recibida.
- **Operaciones:** la lista se actualiza con la respuesta del servidor, sin volver a pedirla. Las filas
  solo se quitan después del `204`.
- **Doble envío:** cada fila o botón queda bloqueado mientras su petición está en curso.
- **Errores:** `error.code` se traduce a un mensaje en español. Los errores de un campo (número de
  inventario o de serie duplicado, cantidad) se muestran junto al campo. Si el servidor responde
  `ITEM_NOT_FOUND`, el ítem se quita de la lista.
- **Seguridad:** los datos del servidor se insertan como nodos de texto (`textContent`), nunca como HTML.
- **Historial:** la opción «Historial» del menú ⋮ abre un diálogo de solo lectura con los movimientos del
  ítem, del más reciente al más antiguo (`api.getItemMovements`).
- **Préstamos:** los botones Prestar y Devolver se ven inactivos y avisan que no están disponibles
  (la API todavía no tiene esos endpoints). `TIPOS_PRESTABLES` en `app.js` reemplaza provisoriamente a
  las categorías.

## 6. Responsabilidad de cada paquete

| Paquete | Hace | Puede importar | Nunca |
|---|---|---|---|
| `cmd/server` | Config; repositorio → servicio → router; timeouts; apagado | todo `internal/*` | Lógica |
| `handlers` | Rutas; formato de entrada; JSON; mapeo de errores | `services`, `models`, `utils`; de `repository`, solo sus errores | Usar el repositorio; reglas de negocio |
| `services` | Reglas de negocio, validaciones, movimientos, fechas de auditoría | `models`, `repository` (interfaces) | HTTP, JSON, excelize |
| `repository` | Excel: esquema, lectura, escritura atómica, versión | `models`, `utils`, `excelize` | Reglas de negocio |
| `models` | Entidades y enumeraciones | stdlib | Paquetes internos |
| `utils` | Config y conversión de celdas | stdlib, `excelize` | `services`/`repository` |

## 7. Convenciones

| Elemento | Convención | Ejemplo |
|---|---|---|
| `models` y `repository` | Inglés (especificación Etapa 2) | `Item.Brand`, `GetByID`, `ErrItemNotFound` |
| `services` | Inglés en la interfaz pública (especificación Etapa 4); mensajes en español | `InventoryService.UpdateStock` |
| `handlers` | Inglés en identificadores (especificación Etapa 5); mensajes en español | `NewInventoryHandler`, `writeError` |
| JSON | `snake_case` en español (contrato con el frontend) | `numero_inventario` |
| Encabezados de hojas | Nombre del campo en inglés | `InventoryNumber` |
| Nombres de hojas | Español, sin tildes | `Inventario`, `Movimientos` |
| Enums | Valor en MAYÚSCULAS, en español | `StatusInRepair = "EN_REPARACION"` |
| Archivos | `snake_case.go`, uno por responsabilidad | `excel_schema.go` |
| Tests | Nombre del método + caso en español | `TestGetByID_NoLeeFilasPosteriores` |
| Entorno | `INVENTARIO_` + MAYÚSCULAS | `INVENTARIO_EXCEL` |

## 8. Pruebas

```
go test ./...
```

- **Lectura** (Etapa 2): lectura del Excel, conversión de filas, búsqueda por ID y por texto.
- **Escritura** (Etapa 3): `Create`, `Update`, `Delete`, `UpdateStock`, la no reutilización de IDs
  (incluso reabriendo el libro), el conflicto de versión, y una prueba concurrente con altas, lecturas y
  ajustes de stock en paralelo.

Usan `internal/repository/testdata/inventario.xlsx`, copiado a un directorio temporal en cada test
para no modificarlo nunca. El Excel se genera desde código, así su contenido queda documentado en
`buildFixture`:

```
go test ./internal/repository -run TestGenerateFixture -update
```

---

### Fuera de alcance hasta ahora
Escrituras, historial y categorías vía API, autenticación, QR, SQLite y exportaciones.

## Equipos y componentes

- Una PC es un registro individual de tipo `Gabinete`, `CPU` o `PC`, con numero
  de inventario y cantidad 1. Su ID es la referencia del conjunto; no se duplica
  en otra hoja ni se suma nuevamente en los indicadores.
- `Item.EquipmentID` / JSON `equipo_id` referencia al gabinete. Cero significa
  sin asociacion. Cada componente conserva su ID, numero de inventario, serie,
  condicion y movimientos. Vincular/desvincular no crea movimientos de stock.
- Solo se vinculan unidades individuales (cantidad 1). No se admiten ciclos,
  gabinetes anidados ni referencias inexistentes. Un gabinete con componentes
  no puede eliminarse ni convertirse en otro tipo o stock; primero se desvincula.
- Numeros de inventario repetidos se admiten exclusivamente dentro de la misma
  PC validada, incluido el gabinete. Los seriales siguen siendo unicos. Si al
  desvincular un componente su numero queda duplicado fuera del conjunto, se
  rechaza la operacion; no se cambia su numero automaticamente.
- `GET /api/equipment?includeRetired=true|false` devuelve
  `{data: [{gabinete: Item, componentes: Item[]}]}` ordenado por ID. Excluye
  gabinetes dados de baja por defecto; el detalle conserva todos sus componentes.
- `PUT /api/inventory/{id}/equipment`, cuerpo `{equipo_id: ID}` (0 para
  desvincular), devuelve `{data: Item}`. Usa validacion, mutex del servicio,
  version del registro, backup y escritura atomica existentes. `POST /api/inventory`
  acepta `equipo_id` para crear un componente directamente asociado. La edicion
  normal conserva la relacion y no admite cambiarla por el endpoint general.
- Excel interno: columna opcional Q `EquipmentID`. Los libros de 16 columnas
  se leen sin modificarlos al abrir; el encabezado se extiende al guardar una fila
  con el nuevo formato. La exportacion agrega M `ID del equipo` (ID interno,
  no numero patrimonial). La importacion valida el conjunto completo antes de
  persistir. Los archivos exportados anteriores, de 12 columnas, mantienen las
  asociaciones existentes por ID. El formato tecnico anterior sigue admitido.
- Frontend: `Inventario general` conserva filtros/paginacion; `Equipos` muestra
  PCs y componentes, busqueda, bajas, edicion/historial y vinculacion explicita.
  Los cambios e importaciones invalidan la vista; respuestas anteriores se descartan.
- No se infieren asociaciones por ubicacion ni se importa automaticamente el
  relevamiento externo. Sus encabezados, datos faltantes y seriales duplicados
  requieren una adaptacion separada y confirmacion de los conjuntos.
