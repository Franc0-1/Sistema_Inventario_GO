// Lógica de la interfaz: estado, renderizado y acciones. Toda la comunicación
// con el servidor pasa por api.js. Los datos del servidor se insertan siempre
// con nodos de texto (nunca con innerHTML).

// ============================================================
// CONFIGURACIÓN DE PRESENTACIÓN
// ============================================================

const TIPO_AIRE = "Aire acondicionado"; // = models.DeviceTypeAirConditioner
const UMBRAL_BAJO_STOCK = 5;
const ESPERA_BUSQUEDA_MS = 300;

// Tipos que se prestan: equivale a la columna Loanable de la hoja Categorias.
// Provisorio hasta que la API exponga las categorías.
const TIPOS_PRESTABLES = new Set(["Notebook", "Celular", "Monitor", "Teclado", "Mouse", "Accesorio", "Otro"]);

// Sugerencias del campo "Tipo" (se suman los tipos que ya existen en el inventario).
const TIPOS_SUGERIDOS = ["PC", "Notebook", "Celular", "Monitor", "Impresora", "Teclado", "Mouse", "Accesorio",
  "Switch", "Router", "UPS", TIPO_AIRE, "Otro"];

// Cada pestaña es un filtro del backend. `fijos` son filtros de la barra que la
// pestaña determina: el selector correspondiente se muestra con ese valor y deshabilitado.
const PESTANAS = [
  { id: "todos", etiqueta: "Todos", parametros: {}, fijos: {} },
  { id: "disponibles", etiqueta: "Disponibles", parametros: { availability: "DISPONIBLE" },
    fijos: { condicion: "OPERATIVO", inventario: "true" } },
  { id: "prestados", etiqueta: "Prestados", parametros: { availability: "PRESTADO" }, fijos: {} },
  { id: "stock", etiqueta: "Consumibles", parametros: {}, fijos: { inventario: "false" } },
];
const pestanaActual = () => PESTANAS.find((p) => p.id === filtrosActuales().pestana) || PESTANAS[0];

// Lista blanca de ordenamiento del backend (models.SortFields) -> texto.
const CAMPOS_ORDEN = [
  ["id", "Orden de alta (ID)"], ["inventoryNumber", "N° de inventario"], ["deviceType", "Tipo"],
  ["brand", "Marca"], ["model", "Modelo"], ["quantity", "Cantidad"], ["location", "Área"],
  ["status", "Condición"], ["createdAt", "Fecha de alta"], ["updatedAt", "Última modificación"],
];
const ORDEN_INICIAL = { campo: "id", sentido: "asc" };

// Columnas de la tabla de equipos; las que tienen `orden` se ordenan con un clic.
const COLUMNAS_EQUIPOS = [
  { titulo: "Equipo", orden: "brand" },
  { titulo: "Inventario / Serie", orden: "inventoryNumber" },
  { titulo: "Ubicación", orden: "location" },
  { titulo: "Condición", orden: "status" },
  { titulo: "Disponibilidad" },
  { titulo: "" },
];

const TAMANOS_PAGINA = [10, 20, 50, 100]; // el backend acepta hasta 100
const TAMANO_PAGINA_INICIAL = 20;

// Filtros de texto: se envían con la misma espera que la búsqueda.
const FILTROS_DE_TEXTO = new Set(["busqueda", "modelo", "numero", "serie"]);
const FILTROS_AVANZADOS = ["modelo", "numero", "serie", "inventario"];

// models.ItemStatus -> cómo se muestra la condición de uso.
const CONDICIONES = {
  OPERATIVO: { etiqueta: "Utilizable", clase: "ok" },
  EN_REPARACION: { etiqueta: "En reparación", clase: "aviso" },
  FUERA_DE_SERVICIO: { etiqueta: "No utilizable", clase: "error" },
  BAJA: { etiqueta: "Dado de baja", clase: "neutro" },
};

const ICONO_POR_TIPO = {
  Notebook: "laptop", Celular: "celular", PC: "monitor", Monitor: "monitor",
  Impresora: "impresora", Mouse: "mouse", Switch: "red", Router: "red",
  "Access Point": "red", [TIPO_AIRE]: "aire",
};

// Códigos de error de la API -> mensaje para el usuario.
const MENSAJES_ERROR = {
  ITEM_NOT_FOUND: "El elemento no existe. Puede que otra persona lo haya eliminado.",
  INVENTORY_NUMBER_EXISTS: "El número de inventario ya está registrado.",
  INVENTORY_NUMBER_REQUIRED: "Ingrese el número de inventario.",
  SERIAL_NUMBER_EXISTS: "El número de serie ya está registrado.",
  INVALID_QUANTITY: "La cantidad no puede ser negativa.",
  INVALID_REQUEST: "Los datos ingresados no son válidos.",
  INVALID_JSON: "Los datos ingresados no son válidos.",
  INVALID_ID: "El elemento seleccionado no es válido.",
  CONFLICT: "Otra persona modificó este elemento al mismo tiempo. Intente nuevamente.",
  REQUEST_TOO_LARGE: "Los datos enviados son demasiado extensos.",
  INVALID_QUERY: "Los filtros de búsqueda no son válidos. Limpie los filtros e intente nuevamente.",
  INTERNAL_ERROR: "Ocurrió un error interno. Intente nuevamente.",
  NETWORK: "No se pudo conectar con el servidor. Verifique la conexión e intente nuevamente.",
};
const MENSAJE_ERROR_GENERICO = "Ocurrió un error inesperado. Intente nuevamente.";

// Errores de la API que corresponden a un campo del formulario.
const CAMPO_DEL_ERROR = {
  INVENTORY_NUMBER_EXISTS: "numero_inventario",
  INVENTORY_NUMBER_REQUIRED: "numero_inventario",
  SERIAL_NUMBER_EXISTS: "numero_serie",
  INVALID_QUANTITY: "cantidad",
};

const esIndividual = (it) => it.tiene_inventario;
const esAire = (it) => it.tipo_dispositivo === TIPO_AIRE;
const esPrestable = (it) => esIndividual(it) && TIPOS_PRESTABLES.has(it.tipo_dispositivo);
const estaPrestado = (it) => it.disponibilidad === "PRESTADO";
const areaDe = (it) => it.ubicacion || "Sin área";
const nombreItem = (it) => [it.marca, it.modelo].filter(Boolean).join(" ") || it.tipo_dispositivo || `Elemento #${it.id}`;

// ============================================================
// ESTADO
// ============================================================

const filtrosVacios = () => ({
  tipo: "", marca: "", area: "", condicion: "", modelo: "", numero: "", serie: "", inventario: "", pestana: "todos",
});

// Filtrar, ordenar y paginar lo resuelve el backend: el frontend solo guarda
// qué se pidió y lo que respondió.
const estado = {
  items: [],              // la página actual (en aires: todos los que cumplen los filtros)
  paginacion: null,       // { page, pageSize, totalItems, totalPages } de la última respuesta
  seccionCargada: null,   // sección a la que pertenecen items (al cambiar, se muestra "cargando")
  catalogo: [],           // inventario completo con bajas: opciones de los filtros, contadores y resumen de aires
  catalogoCargado: false,
  cargando: false,
  errorCarga: "",         // mensaje si falló la última carga/búsqueda
  consulta: "",           // texto de búsqueda general
  seccion: "equipos",
  filtros: { equipos: filtrosVacios(), aires: filtrosVacios() }, // por sección
  orden: { ...ORDEN_INICIAL },
  pagina: 1,
  tamPagina: TAMANO_PAGINA_INICIAL,
  masFiltros: false,      // panel de filtros avanzados abierto
  pendientes: new Set(),  // IDs con una operación en curso (evita doble envío)
  alcanceResumen: null,
  reporteFiltros: { location: "", deviceType: "", includeRetired: false },
  analitica: { datos: null, error: "", cargando: false, solicitud: 0 },
};
const esVistaAnalitica = () => estado.seccion === "resumen" || estado.seccion === "reportes";

function invalidarAnalitica() {
  if (typeof invalidarEquipos === "function") invalidarEquipos();
  ++estado.analitica.solicitud;
  estado.analitica.datos = null;
  if (esVistaAnalitica()) cargarAnalitica();
  if (typeof cargarPanelInventario === "function") cargarPanelInventario();
}
const filtrosActuales = () => estado.filtros[estado.seccion];

// Filtros efectivos: los de la barra más los que fija la pestaña.
const filtrosEfectivos = () => ({ ...filtrosActuales(), ...(estado.seccion === "equipos" ? pestanaActual().fijos : {}) });

// ============================================================
// UTILIDADES
// ============================================================

// h crea un elemento. Los hijos de texto se agregan como nodos de texto, así
// ningún dato del servidor se interpreta como HTML.
function h(etiqueta, props = {}, ...hijos) {
  const el = document.createElement(etiqueta);
  for (const [clave, valor] of Object.entries(props)) {
    if (valor === null || valor === undefined || valor === false) continue;
    if (clave === "class") el.className = valor;
    else if (clave === "dataset") Object.assign(el.dataset, valor);
    else if (clave.startsWith("on")) el.addEventListener(clave.slice(2), valor);
    else el.setAttribute(clave, valor === true ? "" : valor);
  }
  for (const hijo of hijos.flat()) {
    if (hijo !== null && hijo !== undefined && hijo !== false) el.append(hijo);
  }
  return el;
}

// Fecha "cero" de Go (sin valor) = "0001-01-01T00:00:00Z".
const fechaValida = (iso) => Boolean(iso) && !iso.startsWith("0001-");

// "2026-09-15T09:30:00-03:00" -> "15/09/2026"
function formatearFecha(iso) {
  if (!fechaValida(iso)) return "";
  const [a, m, d] = iso.slice(0, 10).split("-");
  return `${d}/${m}/${a}`;
}

function formatearFechaHora(iso) {
  if (!fechaValida(iso)) return "";
  // El backend guarda nanosegundos; Date solo garantiza milisegundos.
  const fecha = new Date(iso.replace(/(\.\d{3})\d+/, "$1"));
  return Number.isNaN(fecha.getTime()) ? "" : fecha.toLocaleString("es-AR", { dateStyle: "short", timeStyle: "short" });
}

const unicos = (valores) => [...new Set(valores.filter(Boolean))].sort((a, b) => a.localeCompare(b, "es"));
const plural = (n, singular, pluralTxt) => `${n} ${n === 1 ? singular : pluralTxt}`;

// ============================================================
// CONSULTA AL BACKEND
// ============================================================

// Ítems del catálogo (inventario completo) de una sección: alimenta las
// opciones de los filtros, los contadores y el resumen de aires.
function catalogoDeSeccion(seccion) {
  return estado.catalogo.filter((it) => esAire(it) === (seccion === "aires"));
}

// parametrosConsulta traduce el estado de la pantalla a los parámetros de
// GET /api/inventory. Sin filtro de condición el backend oculta las bajas.
function parametrosConsulta() {
  const f = filtrosEfectivos();
  const comunes = { q: estado.consulta.trim(), location: f.area, status: f.condicion };
  if (estado.seccion === "aires") {
    // Todos los aires de una vez: se agrupan por área, no se paginan.
    return { ...comunes, deviceType: TIPO_AIRE, sort: "location" };
  }
  return {
    ...comunes,
    excludeDeviceType: TIPO_AIRE,
    deviceType: f.tipo,
    brand: f.marca,
    model: f.modelo.trim(),
    inventoryNumber: f.numero.trim(),
    serialNumber: f.serie.trim(),
    hasInventory: f.inventario,
    ...pestanaActual().parametros,
    ...(estado.alcanceResumen || {}),
    sort: estado.orden.campo,
    order: estado.orden.sentido,
    page: estado.pagina,
    pageSize: estado.tamPagina,
  };
}

const hayFiltros = (f) => Boolean(estado.consulta || f.tipo || f.marca || f.area || f.condicion ||
  FILTROS_AVANZADOS.some((c) => f[c]));

