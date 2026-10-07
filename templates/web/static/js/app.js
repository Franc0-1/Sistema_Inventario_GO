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
  { titulo: "Identificación", orden: "inventoryNumber" },
  { titulo: "Área", orden: "location" },
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
  ++estado.analitica.solicitud;
  estado.analitica.datos = null;
  if (esVistaAnalitica()) cargarAnalitica();
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

// ============================================================
// COMPONENTES
// ============================================================

function celda(clase, etiqueta, ...contenido) {
  return h("div", { class: `celda ${clase}` }, h("span", { class: "celda__etiqueta" }, etiqueta), ...contenido);
}

// Nombre + ícono. El tipo no se muestra como texto: queda en el tooltip del ícono y en el filtro.
function celdaEquipo(it) {
  return h("div", { class: "celda celda--equipo" },
    h("div", { class: "equipo__icono", title: it.tipo_dispositivo || "Sin tipo" },
      iconoNodo(ICONO_POR_TIPO[it.tipo_dispositivo] || "caja", 18)),
    h("div", { class: "equipo__texto" },
      h("h3", { class: "equipo__nombre" }, nombreItem(it), h("span", { class: "equipo__id" }, `#${it.id}`)),
      it.observacion ? h("p", { class: "observacion" }, it.observacion) : null));
}

function identificacion(it) {
  if (!esIndividual(it)) {
    return [h("div", { class: "cantidad" },
      h("span", { class: "cantidad__numero" }, String(it.cantidad)), ` ${it.cantidad === 1 ? "unidad" : "unidades"}`)];
  }
  return [
    h("div", { class: "etiqueta-mono etiqueta-mono--caja" }, iconoNodo("qr", 12), it.numero_inventario || "Sin N°"),
    h("div", { class: "etiqueta-mono" }, iconoNodo("hash", 12), `S/N: ${it.numero_serie || "—"}`),
  ];
}

function area(it) {
  return h("span", { class: "area" }, iconoNodo("pin", 14), areaDe(it));
}

function condicion(it) {
  const c = CONDICIONES[it.estado] || { etiqueta: it.estado || "Sin condición", clase: "neutro" };
  return h("span", { class: `condicion condicion--${c.clase}` }, h("span", { class: "condicion__punto" }), c.etiqueta);
}

function disponibilidad(it) {
  if (!esIndividual(it)) {
    if (it.cantidad <= 0) return [h("span", { class: "insignia insignia--peligro" }, "Sin stock")];
    if (it.cantidad <= UMBRAL_BAJO_STOCK) return [h("span", { class: "insignia insignia--aviso" }, "Bajo stock")];
    return [h("span", { class: "insignia insignia--neutra" }, "Stock óptimo")];
  }
  if (estaPrestado(it)) {
    const desde = formatearFecha(it.fecha_prestamo);
    return [
      h("span", { class: "insignia insignia--aviso" }, iconoNodo("userCheck", 12), "Prestado"),
      it.asignado_a ? h("div", { class: "asignado" }, it.asignado_a) : null,
      desde ? h("div", { class: "texto-suave" }, `Desde ${desde}`) : null,
    ];
  }
  if (!esPrestable(it)) return [h("span", { class: "texto-suave" }, "Uso fijo")];
  return [h("span", { class: "insignia insignia--exito" }, "Disponible")];
}

// Préstamos y devoluciones todavía no tienen endpoint: el botón se conserva
// (es parte del diseño) pero solo informa.
function botonPrestamoInactivo(nombreIcono, texto, azul = false) {
  return h("button", {
    type: "button", class: `boton boton--chico boton--inactivo${azul ? " boton--azul" : ""}`, "aria-disabled": "true",
    title: "Los préstamos todavía no están disponibles",
    onclick: () => avisar("Los préstamos todavía no están disponibles en esta versión.", "info"),
  }, iconoNodo(nombreIcono, 14), texto);
}

function acciones(it) {
  const ocupado = estado.pendientes.has(it.id);
  let principal = null;
  if (!esIndividual(it)) {
    principal = h("button", {
      type: "button", class: "boton boton--chico", disabled: ocupado || it.cantidad <= 0,
      title: it.cantidad <= 0 ? "Sin stock" : null, onclick: () => entregarUno(it),
    }, iconoNodo("packageMinus", 14), "Entregar 1");
  } else if (estaPrestado(it)) {
    principal = botonPrestamoInactivo("devolver", "Devolver");
  } else if (esPrestable(it)) {
    principal = botonPrestamoInactivo("userCheck", "Prestar", true);
  }
  const mas = h("button", {
    type: "button", class: "boton-icono", "aria-label": `Más opciones para ${nombreItem(it)}`,
    "aria-haspopup": "menu", "aria-expanded": "false", disabled: ocupado,
    onclick: (e) => { e.stopPropagation(); alternarMenu(it, e.currentTarget); },
  }, iconoNodo("masOpciones", 18));
  return h("div", { class: "celda celda--acciones" }, principal, mas);
}

function vacio(mensaje) {
  return h("div", { class: "vacio" }, iconoNodo("caja", 40), h("p", {}, mensaje));
}

function estadoCargando(mensaje) {
  return h("div", { class: "estado-carga", role: "status" }, h("span", { class: "girando", "aria-hidden": "true" }), mensaje);
}

function estadoError(mensaje) {
  return h("div", { class: "vacio vacio--error", role: "alert" },
    iconoNodo("alerta", 40), h("p", {}, mensaje),
    h("button", { type: "button", class: "boton boton--secundario", onclick: () => (estado.catalogoCargado ? cargarItems() : cargarTodo()) }, "Reintentar"));
}

function selector(filtro, textoTodos) {
  return h("label", { class: "selector" },
    h("span", { class: "solo-lector" }, textoTodos),
    h("select", { dataset: { filtro, todos: textoTodos } }),
    iconoNodo("chevron", 16));
}

function filtroTexto(filtro, etiqueta) {
  return h("label", { class: "filtro-texto" },
    h("span", { class: "solo-lector" }, etiqueta),
    h("input", { type: "search", class: "filtro-texto__input", dataset: { filtro }, value: filtrosActuales()[filtro],
      placeholder: etiqueta, autocomplete: "off", maxlength: "80" }));
}

function barraFiltros(seccion) {
  const placeholder = seccion === "aires"
    ? "Buscar por marca, modelo, N° de inventario o serie…"
    : "Buscar por N°, marca, modelo, tipo o serie…";
  const equipos = seccion === "equipos";
  return h("div", { class: "barra-filtros" },
    h("label", { class: "buscador" },
      iconoNodo("search", 16),
      h("span", { class: "solo-lector" }, "Buscar"),
      h("input", { type: "search", class: "buscador__input", dataset: { filtro: "busqueda" }, value: estado.consulta,
        placeholder, autocomplete: "off" })),
    h("div", { class: "barra-filtros__selectores" },
      equipos ? selector("tipo", "Todos los tipos") : null,
      equipos ? selector("marca", "Todas las marcas") : null,
      selector("area", "Todas las áreas"),
      selector("condicion", "Cualquier condición"),
      equipos ? h("button", {
        type: "button", class: "boton-limpiar", dataset: { accion: "mas-filtros" },
        "aria-expanded": String(estado.masFiltros), "aria-controls": "filtros-avanzados",
      }, iconoNodo("filtros", 14), h("span", { id: "mas-filtros-texto" }, "Más filtros")) : null,
      h("button", { type: "button", class: "boton-limpiar", dataset: { accion: "limpiar" }, hidden: true },
        iconoNodo("x", 14), "Limpiar")),
    equipos ? h("div", { class: "filtros-avanzados", id: "filtros-avanzados", hidden: !estado.masFiltros },
      filtroTexto("modelo", "Modelo"),
      filtroTexto("numero", "N° de inventario"),
      filtroTexto("serie", "N° de serie"),
      selector("inventario", "Con y sin N° de inventario")) : null);
}

// Las opciones de tipo, marca y área salen del catálogo: se recalculan en cada
// render sin recrear los campos de texto (así no pierden el foco al escribir).
function actualizarOpcionesFiltros() {
  const items = estado.alcanceResumen && estado.seccion === "equipos" ? estado.catalogo : catalogoDeSeccion(estado.seccion);
  const f = filtrosActuales();
  const fijos = estado.seccion === "equipos" ? pestanaActual().fijos : {};
  const opciones = {
    tipo: unicos(items.map((i) => i.tipo_dispositivo)).map((v) => [v, v]),
    marca: unicos(items.map((i) => i.marca)).map((v) => [v, v]),
    area: unicos(items.map((i) => i.ubicacion)).map((v) => [v, v]),
    condicion: Object.entries(CONDICIONES).map(([valor, c]) => [valor, c.etiqueta]),
    inventario: [["true", "Con N° de inventario"], ["false", "Sin N° (consumibles)"]],
  };
  document.querySelectorAll("#contenido select[data-filtro]").forEach((select) => {
    const campo = select.dataset.filtro;
    const fijo = fijos[campo];
    const valor = fijo ?? f[campo];
    const lista = opciones[campo];
    if (valor && !lista.some(([v]) => v === valor)) lista.push([valor, valor]); // conservar la selección
    select.replaceChildren(
      h("option", { value: "" }, select.dataset.todos),
      ...lista.map(([v, texto]) => h("option", { value: v }, texto)));
    select.value = valor;
    select.disabled = fijo !== undefined;
    select.title = fijo !== undefined ? `Lo define la pestaña «${pestanaActual().etiqueta}»` : "";
    select.closest(".selector").classList.toggle("selector--activo", Boolean(valor));
  });
  const limpiar = document.querySelector('[data-accion="limpiar"]');
  if (limpiar) limpiar.hidden = !hayFiltros(f);
  const avanzados = FILTROS_AVANZADOS.filter((c) => f[c]).length;
  const textoMas = document.getElementById("mas-filtros-texto");
  if (textoMas) textoMas.textContent = avanzados ? `Más filtros (${avanzados})` : "Más filtros";
}

// ---- Orden y paginación: la estructura es fija y aquí solo se actualiza
// (los botones no se recrean, así conservan el foco al usarlos con teclado). ----

function controlesOrden() {
  return h("div", { class: "orden" },
    h("label", { class: "selector selector--chico" },
      h("span", { class: "solo-lector" }, "Ordenar por"),
      h("select", { dataset: { control: "orden" } },
        CAMPOS_ORDEN.map(([valor, texto]) => h("option", { value: valor }, texto))),
      iconoNodo("chevron", 16)),
    h("button", { type: "button", class: "boton-icono boton-sentido", dataset: { control: "sentido" } }));
}

function cabeceraEquipos() {
  return h("div", { class: "tabla-cabecera tabla--equipos" },
    COLUMNAS_EQUIPOS.map((c) => h("div", {}, c.orden
      ? h("button", { type: "button", class: "cabecera-orden", dataset: { orden: c.orden } },
        c.titulo, h("span", { class: "cabecera-orden__icono" }))
      : c.titulo)));
}

function pieDePaginacion() {
  return h("nav", { class: "paginacion", id: "paginacion", "aria-label": "Paginación", hidden: true },
    h("p", { class: "paginacion__rango", id: "paginacion-rango", "aria-live": "polite" }),
    h("div", { class: "paginacion__controles" },
      h("label", { class: "selector selector--chico" },
        h("span", { class: "solo-lector" }, "Resultados por página"),
        h("select", { dataset: { control: "tamPagina" } },
          TAMANOS_PAGINA.map((n) => h("option", { value: String(n) }, `${n} por página`))),
        iconoNodo("chevron", 16)),
      h("button", { type: "button", class: "boton-icono", dataset: { ir: "anterior" }, "aria-label": "Página anterior" },
        iconoNodo("anterior", 18)),
      h("span", { class: "paginacion__pagina", id: "paginacion-pagina" }),
      h("button", { type: "button", class: "boton-icono", dataset: { ir: "siguiente" }, "aria-label": "Página siguiente" },
        iconoNodo("siguiente", 18))));
}

function actualizarOrden() {
  const { campo, sentido } = estado.orden;
  const asc = sentido === "asc";
  const select = document.querySelector('[data-control="orden"]');
  if (!select) return;
  select.value = campo;
  const boton = document.querySelector('[data-control="sentido"]');
  const texto = asc ? "Orden ascendente (cambiar a descendente)" : "Orden descendente (cambiar a ascendente)";
  boton.setAttribute("aria-label", texto);
  boton.title = texto;
  boton.replaceChildren(iconoNodo(asc ? "flechaArriba" : "flechaAbajo", 16));

  document.querySelectorAll("[data-orden]").forEach((b) => {
    const activa = b.dataset.orden === campo;
    const titulo = b.firstChild.textContent;
    b.classList.toggle("cabecera-orden--activa", activa);
    b.setAttribute("aria-label", activa
      ? `${titulo}: orden ${asc ? "ascendente" : "descendente"}. Invertir el orden`
      : `Ordenar por ${titulo}`);
    b.querySelector(".cabecera-orden__icono").replaceChildren(iconoNodo(activa ? (asc ? "flechaArriba" : "flechaAbajo") : "flechas", 12));
  });
}

function actualizarPaginacion() {
  const nav = document.getElementById("paginacion");
  if (!nav) return;
  const p = estado.paginacion;
  const visible = Boolean(p && p.totalItems) && !estado.errorCarga && estado.seccionCargada === estado.seccion;
  nav.hidden = !visible;
  if (!visible) return;
  const desde = (p.page - 1) * p.pageSize + 1;
  const hasta = Math.min(p.page * p.pageSize, p.totalItems);
  document.getElementById("paginacion-rango").textContent =
    desde <= p.totalItems ? `Mostrando ${desde}–${hasta} de ${p.totalItems}` : `${p.totalItems} resultados`;
  document.getElementById("paginacion-pagina").textContent = `Página ${p.page} de ${Math.max(1, p.totalPages)}`;
  nav.querySelector('[data-ir="anterior"]').disabled = p.page <= 1;
  nav.querySelector('[data-ir="siguiente"]').disabled = p.page >= p.totalPages;
  nav.querySelector('[data-control="tamPagina"]').value = String(estado.tamPagina);
}

// ============================================================
// SECCIÓN: EQUIPOS Y STOCK
// ============================================================

function plantillaEquipos() {
  return [
    h("header", { class: "vista-cabecera" }, h("h2", {}, "Equipos y stock"), h("p", { class: "texto-suave" }, "Equipos individuales y materiales")),
    barraFiltros("equipos"),
    h("section", { class: "tarjeta-lista" },
      h("div", { class: "tarjeta-lista__barra" },
        h("div", { class: "pestanas", id: "pestanas", role: "tablist" }),
        h("div", { class: "tarjeta-lista__extremo" },
          controlesOrden(),
          h("span", { class: "contador", id: "contador", "aria-live": "polite" }))),
      cabeceraEquipos(),
      h("div", { id: "lista" }),
      pieDePaginacion()),
  ];
}

function filaEquipo(it) {
  const ocupado = estado.pendientes.has(it.id);
  return h("article", { class: `fila tabla--equipos${ocupado ? " fila--ocupada" : ""}`, "aria-busy": ocupado ? "true" : null },
    celdaEquipo(it),
    celda("celda--ident", esIndividual(it) ? "Identificación" : "Cantidad", ...identificacion(it)),
    celda("celda--area", "Área", area(it)),
    celda("celda--condicion", "Condición", condicion(it)),
    celda("celda--disp", esIndividual(it) ? "Disponibilidad" : "Stock", ...disponibilidad(it)),
    acciones(it));
}

// Las pestañas son filtros del backend: el total de la pestaña activa lo da
// el contador de resultados.
function renderEquipos() {
  const f = estado.filtros.equipos;
  const pestanas = document.getElementById("pestanas");
  if (!pestanas.childElementCount) {
    pestanas.replaceChildren(...PESTANAS.map((p) => h("button", {
      type: "button", role: "tab", class: "pestana", dataset: { pestana: p.id },
    }, p.etiqueta)));
  }
  pestanas.querySelectorAll("[data-pestana]").forEach((b) => {
    const activa = b.dataset.pestana === f.pestana;
    b.classList.toggle("pestana--activa", activa);
    b.setAttribute("aria-selected", String(activa));
  });

  actualizarOrden();
  pintarLista(() => estado.items.map(filaEquipo), "No se encontraron equipos o materiales con esos filtros.");
  actualizarPaginacion();
}

// ============================================================
// SECCIÓN: AIRES ACONDICIONADOS (agrupados por área)
// ============================================================

function plantillaAires() {
  return [
    h("header", { class: "vista-cabecera" }, h("h2", {}, "Aires acondicionados")),
    h("section", { class: "resumen" },
      h("div", { class: "resumen__titulo" },
        h("h2", {}, "Distribución por área"),
        h("p", { class: "texto-suave", id: "resumen-texto" })),
      h("div", { class: "resumen__grilla", id: "resumen-aires" })),
    barraFiltros("aires"),
    h("section", { class: "tarjeta-lista" },
      h("div", { class: "tarjeta-lista__barra" },
        h("span", { class: "tarjeta-lista__titulo" }, "Equipos instalados"),
        h("span", { class: "contador", id: "contador" })),
      h("div", { class: "tabla-cabecera tabla--aires", "aria-hidden": "true" },
        ["Equipo", "Identificación", "Condición", ""].map((t) => h("div", {}, t))),
      h("div", { id: "lista" })),
  ];
}

function filaAire(it) {
  const ocupado = estado.pendientes.has(it.id);
  return h("article", { class: `fila tabla--aires${ocupado ? " fila--ocupada" : ""}`, "aria-busy": ocupado ? "true" : null },
    celdaEquipo(it),
    celda("celda--ident", "Identificación", ...identificacion(it)),
    celda("celda--condicion", "Condición", condicion(it)),
    acciones(it));
}

// Tarjetas por área: muestran el total del catálogo, sin depender de los filtros.
function renderResumenAires() {
  const aires = catalogoDeSeccion("aires").filter((it) => it.estado !== "BAJA");
  const areas = unicos(aires.map(areaDe));
  const utilizables = aires.filter((a) => a.estado === "OPERATIVO").length;
  const seleccionada = estado.filtros.aires.area;

  document.getElementById("resumen-texto").textContent = estado.catalogoCargado
    ? `${plural(aires.length, "equipo", "equipos")} en ${plural(areas.length, "área", "áreas")} · ${utilizables} utilizables`
    : "";

  document.getElementById("resumen-aires").replaceChildren(...areas.map((nombreArea) => {
    const deArea = aires.filter((a) => areaDe(a) === nombreArea);
    const conFalla = deArea.filter((a) => a.estado !== "OPERATIVO").length;
    const activa = nombreArea === seleccionada;
    const sinArea = !deArea[0].ubicacion; // no se puede filtrar por "Sin área"
    return h("button", {
      type: "button", class: `resumen-area${activa ? " resumen-area--activa" : ""}`,
      dataset: sinArea ? {} : { area: nombreArea }, "aria-pressed": String(activa), disabled: sinArea,
    },
    h("span", { class: "resumen-area__nombre" }, iconoNodo("pin", 14), nombreArea),
    h("span", { class: "resumen-area__cantidad" }, String(deArea.length)),
    h("span", { class: `resumen-area__detalle${conFalla ? " resumen-area__detalle--alerta" : ""}` },
      conFalla ? plural(conFalla, "con falla", "con falla") : "Todos utilizables"));
  }));
}

function renderAires() {
  renderResumenAires();
  const items = estado.items;
  pintarLista(() => unicos(items.map(areaDe)).map((nombreArea) => {
    const deArea = items.filter((it) => areaDe(it) === nombreArea);
    return h("div", { class: "grupo" },
      h("div", { class: "grupo__cabecera" },
        iconoNodo("pin", 14), h("span", {}, nombreArea),
        h("span", { class: "grupo__cantidad" }, plural(deArea.length, "equipo", "equipos"))),
      ...deArea.map(filaAire));
  }), "No se encontraron aires acondicionados con esos filtros.");
}

// ============================================================
// RENDER GENERAL
// ============================================================

// pintarLista resuelve los estados comunes: primera carga, error, vacío y lista.
// Mientras llega una nueva página se conserva la anterior atenuada.
function pintarLista(construirFilas, mensajeVacio) {
  const lista = document.getElementById("lista");
  const contador = document.getElementById("contador");
  const cargada = estado.seccionCargada === estado.seccion;
  lista.setAttribute("aria-busy", String(estado.cargando));
  lista.classList.toggle("lista--actualizando", estado.cargando && cargada);

  if (estado.errorCarga) {
    contador.textContent = "";
    lista.replaceChildren(estadoError(estado.errorCarga));
  } else if (!cargada) {
    contador.textContent = "";
    lista.replaceChildren(estadoCargando("Cargando inventario…"));
  } else {
    const total = estado.paginacion ? estado.paginacion.totalItems : estado.items.length;
    contador.textContent = estado.cargando ? "Buscando…" : plural(total, "resultado", "resultados");
    lista.replaceChildren(...(estado.items.length ? construirFilas() : [vacio(hayFiltros(filtrosActuales()) || estado.alcanceResumen
      ? "No hay resultados para estos filtros." : mensajeVacio)]));
  }
}

// Reconstruye toda la sección (al cambiar de sección o limpiar filtros).
function renderSeccion() {
  cerrarMenu();
  document.querySelectorAll("[data-seccion]").forEach((b) => {
    const activa = b.dataset.seccion === estado.seccion;
    b.classList.toggle("seccion--activa", activa);
    b.setAttribute("aria-current", activa ? "page" : "false");
  });
  if (esVistaAnalitica()) { renderAnalitica(); return; }
  document.getElementById("contenido").replaceChildren(
    ...(estado.seccion === "aires" ? plantillaAires() : plantillaEquipos()));
  if (estado.seccion === "equipos" && estado.alcanceResumen) {
    document.getElementById("contenido").prepend(h("div", { class: "alcance-resumen" },
      h("p", {}, `Consulta del resumen · Todas las categorías · ${estado.alcanceResumen.includeRetired ? "Con bajas" : "Sin bajas"}${estado.alcanceResumen.availability ? ` · ${estado.alcanceResumen.availability === "PRESTADO" ? "Prestados" : "Disponibles"}` : ""}`),
      h("button", { type: "button", class: "boton boton--secundario", onclick: () => {
        estado.alcanceResumen = null; limpiarFiltros();
      } }, "Quitar alcance")));
  }
  renderResultados();
}

// Actualiza resultados, filtros y contadores sin recrear el buscador.
function renderResultados() {
  cerrarMenu();
  if (esVistaAnalitica()) { renderAnalitica(); actualizarContadoresSecciones(); return; }
  if (estado.seccion === "aires") renderAires(); else renderEquipos();
  actualizarOpcionesFiltros();
  actualizarContadoresSecciones();
}

function actualizarContadoresSecciones() {
  const activos = (seccion) => catalogoDeSeccion(seccion).filter((it) => it.estado !== "BAJA").length;
  document.getElementById("cantidad-equipos").textContent = estado.catalogoCargado ? activos("equipos") : "–";
  document.getElementById("cantidad-aires").textContent = estado.catalogoCargado ? activos("aires") : "–";
}

// ============================================================
// CARGA Y BÚSQUEDA
// ============================================================

let ultimaSolicitud = 0;
let ultimaSolicitudCatalogo = 0;
let temporizadorBusqueda = null;

// cargarItems pide al backend la página que corresponde a los filtros, el
// orden y la página actuales. Si llega una respuesta vieja después de una
// nueva (búsquedas o clics rápidos), se descarta.
async function cargarItems() {
  if (esVistaAnalitica()) return true;
  clearTimeout(temporizadorBusqueda);
  const solicitud = ++ultimaSolicitud;
  const seccion = estado.seccion;
  estado.cargando = true;
  estado.errorCarga = "";
  renderResultados();
  try {
    const { items, pagination } = await api.getInventory(parametrosConsulta());
    if (solicitud !== ultimaSolicitud) return;
    // La página quedó fuera de rango (p. ej. se eliminó el último ítem de la
    // última página): se pide la última que existe.
    if (!items.length && pagination.totalPages > 0 && estado.pagina > pagination.totalPages) {
      estado.pagina = pagination.totalPages;
      queueMicrotask(cargarItems);
      return;
    }
    estado.items = items;
    estado.paginacion = seccion === "equipos" ? pagination : null;
    estado.seccionCargada = seccion;
    return true;
  } catch (err) {
    if (solicitud !== ultimaSolicitud) return;
    estado.errorCarga = mensajeDeError(err);
    return false;
  } finally {
    if (solicitud === ultimaSolicitud) {
      estado.cargando = false;
      renderResultados();
    }
  }
}

// El catálogo (todo el inventario, con bajas) solo alimenta opciones de
// filtros, sugerencias y contadores: si falla, la lista sigue funcionando.
async function cargarCatalogo() {
  const solicitud = ++ultimaSolicitudCatalogo;
  try {
    const { items } = await api.getInventory({ includeRetired: true });
    if (solicitud !== ultimaSolicitudCatalogo) return false;
    estado.catalogo = items;
    estado.catalogoCargado = true;
    renderResultados();
    return true;
  } catch (err) {
    console.error("No se pudo cargar el catálogo para los filtros:", err);
    return false;
  }
}

function cargarTodo() {
  cargarCatalogo();
  cargarItems();
}

// Cualquier cambio de filtros vuelve a la primera página.
function recargarDesdeElInicio() {
  estado.pagina = 1;
  cargarItems();
}

function programarBusqueda() {
  clearTimeout(temporizadorBusqueda);
  temporizadorBusqueda = setTimeout(recargarDesdeElInicio, ESPERA_BUSQUEDA_MS);
}

// ---- Cambios con la respuesta del servidor ----

function reemplazarEn(lista, item, agregar) {
  const i = lista.findIndex((it) => it.id === item.id);
  if (i >= 0) lista[i] = item; else if (agregar) lista.push(item);
}

// actualizarItem refleja un ítem leído del servidor, sin volver a pedir la lista.
function actualizarItem(item) {
  reemplazarEn(estado.items, item, false);
  reemplazarEn(estado.catalogo, item, true);
  renderResultados();
}

// Después de crear, editar o cambiar stock, el ítem puede haber cambiado de
// página o dejado de cumplir los filtros: se muestra el cambio al instante y
// se vuelve a pedir la página actual.
function aplicarCambio(item) {
  invalidarAnalitica();
  actualizarItem(item);
  cargarItems();
}

function quitarItem(id) {
  invalidarAnalitica();
  estado.items = estado.items.filter((it) => it.id !== id);
  estado.catalogo = estado.catalogo.filter((it) => it.id !== id);
  renderResultados();
  cargarItems();
}

// Bloquea la fila mientras dura la operación: evita el doble envío.
async function conFilaOcupada(id, operacion) {
  if (estado.pendientes.has(id)) return;
  estado.pendientes.add(id);
  renderResultados();
  try {
    await operacion();
  } finally {
    estado.pendientes.delete(id);
    renderResultados();
  }
}

// ============================================================
// ERRORES Y AVISOS
// ============================================================

function mensajeDeError(err) {
  if (!(err instanceof ApiError)) {
    console.error(err);
    return MENSAJE_ERROR_GENERICO;
  }
  if (err.code === "INVALID_ITEM") {
    // El backend explica qué falta ("ítem inválido: falta la marca"); se muestra solo el detalle.
    const detalle = err.message.split(": ").slice(1).join(": ");
    return detalle ? `Revise los datos: ${detalle}.` : MENSAJES_ERROR.INVALID_REQUEST;
  }
  return MENSAJES_ERROR[err.code] || MENSAJE_ERROR_GENERICO;
}

// Si el servidor dice que el ítem ya no existe, se lo quita de la lista.
function sincronizarSiNoExiste(err, id) {
  if (err instanceof ApiError && err.code === "ITEM_NOT_FOUND") quitarItem(id);
}

const ICONO_AVISO = { exito: "check", error: "alerta", info: "info" };

function avisar(mensaje, tipo = "exito") {
  const aviso = h("div", { class: `aviso aviso--${tipo}`, role: tipo === "error" ? "alert" : "status" },
    iconoNodo(ICONO_AVISO[tipo], 18),
    h("p", { class: "aviso__texto" }, mensaje),
    h("button", { type: "button", class: "aviso__cerrar", "aria-label": "Cerrar aviso", onclick: () => aviso.remove() },
      iconoNodo("x", 14)));
  document.getElementById("avisos").append(aviso);
  setTimeout(() => aviso.remove(), tipo === "error" ? 7000 : 4000);
}

function marcarOcupado(boton, ocupado, textoOcupado) {
  const texto = boton.querySelector(".boton__texto");
  if (ocupado) {
    texto.dataset.original = texto.textContent;
    texto.textContent = textoOcupado;
  } else if (texto.dataset.original) {
    texto.textContent = texto.dataset.original;
  }
  boton.disabled = ocupado;
  boton.classList.toggle("boton--cargando", ocupado);
  boton.setAttribute("aria-busy", String(ocupado));
}

function mostrarAlerta(id, mensaje) {
  const alerta = document.getElementById(id);
  alerta.textContent = mensaje;
  alerta.hidden = !mensaje;
}

// El resultado se conserva hasta terminar la recarga: reintentar nunca vuelve a importar.
let importacionOcupada = false;
let importacionRealizada = null;
let exportacionOcupada = false;

async function exportarExcel() {
  if (exportacionOcupada) return;
  exportacionOcupada = true;
  const boton = document.getElementById("boton-exportar");
  document.getElementById("herramientas-excel").setAttribute("aria-busy", "true");
  marcarOcupado(boton, true, "Exportando…");
  let url;
  let enlace;
  try {
    const { archivo, nombre } = await api.exportInventory();
    url = URL.createObjectURL(archivo);
    enlace = h("a", { href: url, download: nombre, hidden: true });
    document.body.append(enlace);
    enlace.click();
  } catch (err) {
    avisar(mensajeDeError(err), "error");
  } finally {
    enlace?.remove();
    if (url) setTimeout(() => URL.revokeObjectURL(url), 1000);
    exportacionOcupada = false;
    document.getElementById("herramientas-excel").setAttribute("aria-busy", "false");
    marcarOcupado(boton, false);
  }
}

function abrirImportacion() {
  if (importacionOcupada) return;
  const form = document.getElementById("form-importar");
  if (importacionRealizada === null) {
    form.reset();
    document.getElementById("importar-nombre").textContent = "";
    document.getElementById("importar-resultado").hidden = true;
    mostrarAlerta("form-importar-error", "");
  }
  actualizarImportacion();
  document.getElementById("dialogo-importar").showModal();
}

function actualizarImportacion() {
  const realizada = importacionRealizada !== null;
  const dialogo = document.getElementById("dialogo-importar");
  dialogo.setAttribute("aria-busy", String(importacionOcupada));
  document.getElementById("importar-archivo").disabled = importacionOcupada || realizada;
  document.getElementById("importar-confirmacion").disabled = importacionOcupada || realizada;
  dialogo.querySelectorAll("[data-cerrar]").forEach((b) => {
    b.disabled = importacionOcupada;
    if (b.classList.contains("boton")) b.textContent = realizada ? "Cerrar" : "Cancelar";
  });
  const boton = document.getElementById("importar-enviar");
  boton.disabled = importacionOcupada;
  boton.classList.toggle("boton--cargando", importacionOcupada);
  boton.querySelector(".boton__texto").textContent = importacionOcupada
    ? (realizada ? "Actualizando…" : "Importando…")
    : (realizada ? "Reintentar actualización" : "Reemplazar inventario");
}

async function recargarTrasImportacion() {
  clearTimeout(temporizadorBusqueda);
  ++ultimaSolicitud;
  ++ultimaSolicitudCatalogo;
  estado.cargando = false;
  estado.pagina = 1;
  if (!await cargarCatalogo()) return false;
  for (const seccion of ["equipos", "aires"]) {
    const items = catalogoDeSeccion(seccion);
    for (const [filtro, campo] of [["tipo", "tipo_dispositivo"], ["marca", "marca"], ["area", "ubicacion"]]) {
      if (!items.some((it) => it[campo] === estado.filtros[seccion][filtro])) estado.filtros[seccion][filtro] = "";
    }
  }
  return await cargarItems();
}

async function importarExcel(e) {
  e.preventDefault();
  if (importacionOcupada) return;
  mostrarAlerta("form-importar-error", "");
  const archivo = document.getElementById("importar-archivo").files[0];
  if (importacionRealizada === null) {
    if (!archivo || !/\.xlsx$/i.test(archivo.name)) {
      mostrarAlerta("form-importar-error", "Seleccione un archivo .xlsx.");
      return;
    }
    if (!document.getElementById("importar-confirmacion").checked) {
      mostrarAlerta("form-importar-error", "Confirme el reemplazo del inventario antes de continuar.");
      return;
    }
  }
  importacionOcupada = true;
  actualizarImportacion();
  try {
    if (importacionRealizada === null) {
      const { imported } = await api.importInventory(archivo);
      importacionRealizada = imported;
      invalidarAnalitica();
      const resultado = document.getElementById("importar-resultado");
      resultado.textContent = `Importación exitosa: ${plural(imported, "registro importado", "registros importados")}.`;
      resultado.hidden = false;
      actualizarImportacion();
    }
    if (!await recargarTrasImportacion()) {
      mostrarAlerta("form-importar-error", "La importación fue exitosa, pero no se pudo actualizar la pantalla. Reintente la actualización.");
      return;
    }
    avisar(`Importación exitosa: ${plural(importacionRealizada, "registro importado", "registros importados")}.`);
    importacionRealizada = null;
    document.getElementById("dialogo-importar").close();
  } catch (err) {
    const validacion = err instanceof ApiError && err.status === 400 && err.code === "INVALID_REQUEST";
    mostrarAlerta("form-importar-error", importacionRealizada !== null
      ? "La importación fue exitosa, pero no se pudo actualizar la pantalla. Reintente la actualización."
      : (validacion ? err.message : mensajeDeError(err)));
  } finally {
    importacionOcupada = false;
    actualizarImportacion();
  }
}

// ============================================================
// MENÚ DE ACCIONES (⋮)
// ============================================================

let menuAbierto = null;

function alternarMenu(it, boton) {
  if (menuAbierto && menuAbierto.boton === boton) {
    cerrarMenu({ devolverFoco: true });
    return;
  }
  cerrarMenu();
  const opciones = [
    { texto: "Editar", icono: "lapiz", accion: () => editarItem(it) },
    esIndividual(it) ? null : { texto: "Ajustar stock", icono: "caja", accion: () => abrirStock(it) },
    { texto: "Historial", icono: "historial", accion: () => abrirHistorial(it) },
    { texto: "Eliminar", icono: "papelera", accion: () => abrirEliminar(it), peligro: true },
  ].filter(Boolean);

  const menu = h("div", { class: "menu-acciones", role: "menu", "aria-label": `Acciones para ${nombreItem(it)}` },
    opciones.map((o) => h("button", {
      type: "button", role: "menuitem",
      class: `menu-acciones__item${o.peligro ? " menu-acciones__item--peligro" : ""}`,
      onclick: () => { cerrarMenu(); o.accion(); },
    }, iconoNodo(o.icono, 16), o.texto)));
  document.body.append(menu);

  // Debajo del botón, o arriba si no entra; siempre dentro de la ventana.
  const r = boton.getBoundingClientRect();
  const top = r.bottom + 4 + menu.offsetHeight > innerHeight - 8 ? r.top - menu.offsetHeight - 4 : r.bottom + 4;
  const left = Math.max(8, Math.min(r.right - menu.offsetWidth, innerWidth - menu.offsetWidth - 8));
  menu.style.top = `${Math.max(8, top)}px`;
  menu.style.left = `${left}px`;

  boton.setAttribute("aria-expanded", "true");
  menuAbierto = { menu, boton };
  menu.querySelector("button").focus();
}

function cerrarMenu({ devolverFoco = false } = {}) {
  if (!menuAbierto) return;
  const { menu, boton } = menuAbierto;
  menuAbierto = null;
  menu.remove();
  boton.setAttribute("aria-expanded", "false");
  if (devolverFoco && boton.isConnected) boton.focus();
}

function navegarMenu(e) {
  if (!menuAbierto) return;
  const items = [...menuAbierto.menu.querySelectorAll("button")];
  const actual = items.indexOf(document.activeElement);
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    e.preventDefault();
    const paso = e.key === "ArrowDown" ? 1 : -1;
    items[(actual + paso + items.length) % items.length].focus();
  } else if (e.key === "Escape") {
    cerrarMenu({ devolverFoco: true });
  } else if (e.key === "Tab") {
    cerrarMenu();
  }
}

// ============================================================
// ALTA Y EDICIÓN
// ============================================================

const formItem = { idEditando: null, enviando: false };
const campoForm = (nombre) => document.getElementById("form-item").elements[nombre];

function llenarSugerencias() {
  const opciones = (valores) => valores.map((v) => h("option", { value: v }));
  document.getElementById("lista-tipos").replaceChildren(
    ...opciones(unicos([...TIPOS_SUGERIDOS, ...estado.catalogo.map((i) => i.tipo_dispositivo)])));
  document.getElementById("lista-areas").replaceChildren(...opciones(unicos(estado.catalogo.map((i) => i.ubicacion))));
}

// Coherencia HasInventory / InventoryNumber: sin inventario no hay número, y
// un equipo individual es unitario (así lo muestra la lista).
function sincronizarInventario() {
  const tiene = campoForm("tiene_inventario").checked;
  const numero = campoForm("numero_inventario");
  const cantidad = campoForm("cantidad");
  numero.disabled = !tiene;
  document.getElementById("f-numero-obligatorio").hidden = !tiene;
  if (!tiene) {
    numero.value = "";
    limpiarErrorCampo("numero_inventario");
  }
  cantidad.disabled = tiene;
  if (tiene) cantidad.value = "1";
  document.getElementById("f-cantidad-ayuda").hidden = !tiene;
}

function abrirFormulario(item = null) {
  const form = document.getElementById("form-item");
  form.reset();
  limpiarErroresFormulario();
  llenarSugerencias();
  formItem.idEditando = item ? item.id : null;

  document.getElementById("dialogo-item-titulo").textContent = item ? `Editar elemento #${item.id}` : "Ingresar nuevo elemento";
  document.querySelector("#form-item-guardar .boton__texto").textContent = item ? "Guardar cambios" : "Crear elemento";

  const valores = item || { tiene_inventario: true, cantidad: 1, estado: "OPERATIVO" };
  campoForm("tiene_inventario").checked = Boolean(valores.tiene_inventario);
  for (const nombre of ["numero_inventario", "numero_serie", "tipo_dispositivo", "ubicacion", "marca", "modelo", "observacion"]) {
    campoForm(nombre).value = valores[nombre] || "";
  }
  campoForm("cantidad").value = String(valores.cantidad ?? 0);
  campoForm("estado").value = valores.estado || "OPERATIVO";
  sincronizarInventario();

  const fechas = document.getElementById("form-item-fechas");
  const alta = item && formatearFechaHora(item.created_at);
  const cambio = item && formatearFechaHora(item.updated_at);
  fechas.textContent = [alta && `Alta: ${alta}`, cambio && `Última modificación: ${cambio}`].filter(Boolean).join(" · ");
  fechas.hidden = !fechas.textContent;

  document.getElementById("dialogo-item").showModal();
  campoForm(item ? "marca" : "tipo_dispositivo").focus();
}

function leerFormulario() {
  const valor = (nombre) => campoForm(nombre).value.trim();
  const tiene = campoForm("tiene_inventario").checked;
  const cantidadTexto = valor("cantidad");
  return {
    numero_inventario: tiene ? valor("numero_inventario") : "",
    tiene_inventario: tiene,
    tipo_dispositivo: valor("tipo_dispositivo"),
    marca: valor("marca"),
    modelo: valor("modelo"),
    numero_serie: valor("numero_serie"),
    cantidad: cantidadTexto === "" ? NaN : Number(cantidadTexto),
    ubicacion: valor("ubicacion"),
    estado: campoForm("estado").value,
    observacion: valor("observacion"),
  };
}

// Validación básica para ayudar al usuario. La autoridad sigue siendo el backend.
function validarFormulario(datos) {
  const errores = {};
  const obligatorios = { tipo_dispositivo: "el tipo", marca: "la marca", modelo: "el modelo", ubicacion: "el área", estado: "la condición" };
  for (const [campo, nombre] of Object.entries(obligatorios)) {
    if (!datos[campo]) errores[campo] = `Ingrese ${nombre}.`;
  }
  if (datos.tiene_inventario && !datos.numero_inventario) errores.numero_inventario = "Ingrese el número de inventario.";
  if (!Number.isInteger(datos.cantidad) || datos.cantidad < 0) errores.cantidad = "Ingrese un número entero mayor o igual a 0.";
  return errores;
}

function mostrarErrorCampo(campo, mensaje) {
  const error = document.getElementById(`error-${campo}`);
  error.textContent = mensaje;
  error.hidden = false;
  const input = campoForm(campo);
  input.setAttribute("aria-invalid", "true");
  input.setAttribute("aria-describedby", error.id);
  input.closest(".campo").classList.add("campo--invalido");
}

function limpiarErrorCampo(campo) {
  const error = document.getElementById(`error-${campo}`);
  if (!error) return;
  error.hidden = true;
  const input = campoForm(campo);
  input.removeAttribute("aria-invalid");
  input.removeAttribute("aria-describedby");
  input.closest(".campo").classList.remove("campo--invalido");
}

function limpiarErroresFormulario() {
  document.querySelectorAll("#form-item [id^='error-']").forEach((e) => limpiarErrorCampo(e.id.slice("error-".length)));
  mostrarAlerta("form-item-error", "");
}

async function guardarItem(e) {
  e.preventDefault();
  if (formItem.enviando) return;

  limpiarErroresFormulario();
  const datos = leerFormulario();
  const errores = validarFormulario(datos);
  const campos = Object.keys(errores);
  if (campos.length) {
    campos.forEach((c) => mostrarErrorCampo(c, errores[c]));
    campoForm(campos[0]).focus();
    return;
  }

  const boton = document.getElementById("form-item-guardar");
  const editando = formItem.idEditando;
  formItem.enviando = true;
  marcarOcupado(boton, true, editando ? "Guardando…" : "Creando…");
  try {
    const item = editando
      ? await api.updateInventoryItem(editando, datos)
      : await api.createInventoryItem(datos);
    aplicarCambio(item);
    document.getElementById("dialogo-item").close();
    avisar(editando ? `Se guardaron los cambios de ${nombreItem(item)}.` : `Se creó ${nombreItem(item)}.`);
  } catch (err) {
    const campo = err instanceof ApiError ? CAMPO_DEL_ERROR[err.code] : null;
    if (campo) {
      mostrarErrorCampo(campo, mensajeDeError(err));
      campoForm(campo).focus();
    } else {
      mostrarAlerta("form-item-error", mensajeDeError(err));
    }
    if (editando) sincronizarSiNoExiste(err, editando);
  } finally {
    formItem.enviando = false;
    marcarOcupado(boton, false);
  }
}

// La edición parte de los datos actuales del servidor, no de la lista en pantalla.
function editarItem(it) {
  return conFilaOcupada(it.id, async () => {
    try {
      const actual = await api.getInventoryById(it.id);
      actualizarItem(actual);
      abrirFormulario(actual);
    } catch (err) {
      avisar(mensajeDeError(err), "error");
      sincronizarSiNoExiste(err, it.id);
    }
  });
}

// ============================================================
// STOCK
// ============================================================

const formStock = { item: null, enviando: false };

async function enviarStock(id, cantidad) {
  const item = await api.updateInventoryStock(id, cantidad);
  aplicarCambio(item);
  return item;
}

function entregarUno(it) {
  return conFilaOcupada(it.id, async () => {
    try {
      const item = await enviarStock(it.id, it.cantidad - 1);
      avisar(`Se entregó 1 unidad de ${nombreItem(item)}. Quedan ${item.cantidad}.`);
    } catch (err) {
      avisar(mensajeDeError(err), "error");
      sincronizarSiNoExiste(err, it.id);
    }
  });
}

function abrirStock(it) {
  formStock.item = it;
  document.getElementById("stock-nombre").textContent = nombreItem(it);
  document.getElementById("stock-actual").textContent = `Cantidad actual: ${plural(it.cantidad, "unidad", "unidades")}`;
  const input = document.getElementById("s-cantidad");
  input.value = String(it.cantidad);
  document.getElementById("error-stock-cantidad").hidden = true;
  input.removeAttribute("aria-invalid");
  mostrarAlerta("form-stock-error", "");
  document.getElementById("dialogo-stock").showModal();
  input.select();
}

async function guardarStock(e) {
  e.preventDefault();
  if (formStock.enviando) return;
  const input = document.getElementById("s-cantidad");
  const error = document.getElementById("error-stock-cantidad");
  const texto = input.value.trim();
  const cantidad = texto === "" ? NaN : Number(texto);
  if (!Number.isInteger(cantidad) || cantidad < 0) {
    error.textContent = "Ingrese un número entero mayor o igual a 0.";
    error.hidden = false;
    input.setAttribute("aria-invalid", "true");
    input.focus();
    return;
  }
  error.hidden = true;
  input.removeAttribute("aria-invalid");

  const { id } = formStock.item;
  const boton = document.getElementById("form-stock-guardar");
  formStock.enviando = true;
  marcarOcupado(boton, true, "Guardando…");
  estado.pendientes.add(id);
  try {
    const item = await enviarStock(id, cantidad);
    document.getElementById("dialogo-stock").close();
    avisar(`Stock de ${nombreItem(item)} actualizado: ${plural(item.cantidad, "unidad", "unidades")}.`);
  } catch (err) {
    mostrarAlerta("form-stock-error", mensajeDeError(err));
    sincronizarSiNoExiste(err, id);
  } finally {
    formStock.enviando = false;
    estado.pendientes.delete(id);
    marcarOcupado(boton, false);
    renderResultados();
  }
}

// ============================================================
// ELIMINAR
// ============================================================

const formEliminar = { item: null, enviando: false };

function abrirEliminar(it) {
  formEliminar.item = it;
  document.getElementById("eliminar-nombre").textContent =
    `${nombreItem(it)}${it.numero_inventario ? ` · N° ${it.numero_inventario}` : ""}`;
  mostrarAlerta("form-eliminar-error", "");
  document.getElementById("dialogo-eliminar").showModal();
  document.querySelector("#dialogo-eliminar [data-cerrar].boton").focus(); // foco en "Cancelar": acción segura
}

// La fila se quita solo después de que el servidor confirma (204).
async function confirmarEliminar(e) {
  e.preventDefault();
  if (formEliminar.enviando) return;
  const it = formEliminar.item;
  const boton = document.getElementById("form-eliminar-confirmar");
  formEliminar.enviando = true;
  marcarOcupado(boton, true, "Eliminando…");
  estado.pendientes.add(it.id);
  try {
    await api.deleteInventoryItem(it.id);
    estado.pendientes.delete(it.id);
    quitarItem(it.id);
    document.getElementById("dialogo-eliminar").close();
    avisar(`Se eliminó ${nombreItem(it)}.`);
  } catch (err) {
    estado.pendientes.delete(it.id);
    if (err instanceof ApiError && err.code === "ITEM_NOT_FOUND") {
      document.getElementById("dialogo-eliminar").close();
      avisar(mensajeDeError(err), "error");
      quitarItem(it.id);
    } else {
      mostrarAlerta("form-eliminar-error", mensajeDeError(err));
      renderResultados();
    }
  } finally {
    formEliminar.enviando = false;
    marcarOcupado(boton, false);
  }
}

// ============================================================
// HISTORIAL (solo lectura)
// ============================================================

// models.MovementType -> etiqueta, estilo e indicador de dirección.
const TIPOS_MOVIMIENTO = {
  stock_in: { etiqueta: "Ingreso", clase: "insignia--exito", signo: "+" },
  stock_out: { etiqueta: "Egreso", clase: "insignia--aviso", signo: "−" },
  stock_update: { etiqueta: "Sin cambio", clase: "insignia--neutra", signo: "" },
  transfer: { etiqueta: "Traslado", clase: "insignia--neutra", signo: "" },
  adjustment: { etiqueta: "Ajuste", clase: "insignia--neutra", signo: "" },
};

let ultimaConsultaHistorial = 0;

async function abrirHistorial(it) {
  const consulta = ++ultimaConsultaHistorial;
  const contenido = document.getElementById("historial-contenido");
  document.getElementById("historial-item").textContent =
    `${nombreItem(it)} · #${it.id}${it.numero_inventario ? ` · N° ${it.numero_inventario}` : ""}`;
  contenido.replaceChildren(estadoCargando("Cargando historial…"));
  document.getElementById("dialogo-historial").showModal();

  try {
    const movimientos = await api.getItemMovements(it.id);
    if (consulta !== ultimaConsultaHistorial) return; // se abrió otro historial mientras tanto
    contenido.replaceChildren(movimientos.length
      ? tablaHistorial(movimientos)
      : vacio("Todavía no hay movimientos registrados para este elemento."));
  } catch (err) {
    if (consulta !== ultimaConsultaHistorial) return;
    contenido.replaceChildren(h("p", { class: "alerta-form", role: "alert" }, mensajeDeError(err)));
  }
}

function tablaHistorial(movimientos) {
  const columnas = ["Fecha", "Tipo", "Cantidad", "Stock anterior", "Stock nuevo", "Origen", "Destino", "Observación"];
  const numericas = new Set(["Cantidad", "Stock anterior", "Stock nuevo"]);
  const texto = (valor) => valor || "—";

  // La API los devuelve del más antiguo al más nuevo; se muestran al revés.
  const filas = [...movimientos].reverse().map((m) => {
    const tipo = TIPOS_MOVIMIENTO[m.tipo] || { etiqueta: m.tipo || "—", clase: "insignia--neutra", signo: "" };
    return h("tr", {},
      h("td", { class: "tabla-historial__fecha" }, formatearFechaHora(m.created_at) || "—"),
      h("td", {}, h("span", { class: `insignia ${tipo.clase}` }, tipo.etiqueta)),
      h("td", { class: "num" }, `${m.cantidad ? tipo.signo : ""}${m.cantidad}`),
      h("td", { class: "num" }, String(m.cantidad_anterior)),
      h("td", { class: "num" }, String(m.cantidad_nueva)),
      h("td", {}, texto(m.ubicacion_origen)),
      h("td", {}, texto(m.ubicacion_destino)),
      h("td", { class: "tabla-historial__nota" }, texto(m.observacion)));
  });

  return h("div", { class: "tabla-historial" },
    h("table", {},
      h("caption", { class: "solo-lector" }, "Movimientos, del más reciente al más antiguo"),
      h("thead", {}, h("tr", {}, columnas.map((c) => h("th", { scope: "col", class: numericas.has(c) ? "num" : null }, c)))),
      h("tbody", {}, filas)));
}

// ============================================================
// TEMA CLARO/OSCURO (el valor inicial lo fija el script del <head>)
// ============================================================

function actualizarBotonTema() {
  const oscuro = document.documentElement.dataset.theme === "dark";
  const texto = oscuro ? "Cambiar a modo claro" : "Cambiar a modo oscuro";
  const boton = document.getElementById("boton-tema");
  boton.setAttribute("aria-label", texto);
  boton.title = texto;
}

function alternarTema() {
  const nuevo = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
  document.documentElement.dataset.theme = nuevo;
  try { localStorage.setItem("tema", nuevo); } catch (e) { /* sin almacenamiento: vale solo para esta visita */ }
  actualizarBotonTema();
}

// ============================================================
// INICIO Y EVENTOS
// ============================================================

function cambiarSeccion(seccion) {
  if (seccion === estado.seccion) return;
  ++ultimaSolicitud;
  ++estado.analitica.solicitud;
  estado.cargando = false;
  estado.seccion = seccion;
  estado.analitica.datos = null;
  estado.analitica.error = "";
  estado.analitica.cargando = false;
  history.replaceState(null, "", seccion === "equipos" ? location.pathname : `#${seccion}`);
  renderSeccion();
  if (esVistaAnalitica()) cargarAnalitica(); else cargarItems();
}

function limpiarFiltros() {
  estado.alcanceResumen = null;
  const f = filtrosActuales();
  estado.filtros[estado.seccion] = { ...filtrosVacios(), pestana: f.pestana };
  estado.consulta = "";
  renderSeccion();
  recargarDesdeElInicio();
}

// Clic en una columna: la misma invierte el sentido; otra ordena ascendente.
function ordenarPor(campo) {
  const { orden } = estado;
  estado.orden = orden.campo === campo
    ? { campo, sentido: orden.sentido === "asc" ? "desc" : "asc" }
    : { campo, sentido: "asc" };
  recargarDesdeElInicio();
}

function irAPagina(pagina) {
  estado.pagina = pagina;
  cargarItems();
  // Si la lista quedó arriba de la pantalla (se estaba al pie), volver a su inicio.
  const tarjeta = document.querySelector(".tarjeta-lista");
  if (tarjeta && tarjeta.getBoundingClientRect().top < 0) tarjeta.scrollIntoView({ block: "start" });
}

function alternarMasFiltros(boton) {
  estado.masFiltros = !estado.masFiltros;
  boton.setAttribute("aria-expanded", String(estado.masFiltros));
  const panel = document.getElementById("filtros-avanzados");
  panel.hidden = !estado.masFiltros;
  if (estado.masFiltros) panel.querySelector("input").focus();
}

document.addEventListener("DOMContentLoaded", () => {
  pintarIconosEstaticos();
  actualizarBotonTema();
  document.getElementById("boton-tema").addEventListener("click", alternarTema);
  document.getElementById("boton-nuevo").addEventListener("click", () => abrirFormulario());
  document.getElementById("boton-exportar").addEventListener("click", exportarExcel);
  document.getElementById("boton-importar").addEventListener("click", abrirImportacion);
  const herramientasExcel = document.getElementById("herramientas-excel");
  herramientasExcel.addEventListener("click", (e) => {
    if (e.target.closest("button")) {
      herramientasExcel.open = false;
      if (!document.querySelector("dialog[open]")) herramientasExcel.querySelector("summary").focus();
    }
  });
  document.getElementById("dialogo-importar").addEventListener("close", () => herramientasExcel.querySelector("summary").focus());
  document.addEventListener("click", (e) => {
    if (!herramientasExcel.contains(e.target)) herramientasExcel.open = false;
  });
  herramientasExcel.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      herramientasExcel.open = false;
      herramientasExcel.querySelector("summary").focus();
    }
  });
  document.getElementById("form-importar").addEventListener("submit", importarExcel);
  document.getElementById("importar-archivo").addEventListener("change", (e) => {
    document.getElementById("importar-nombre").textContent = e.target.files[0]?.name || "";
    document.getElementById("importar-confirmacion").checked = false;
    mostrarAlerta("form-importar-error", "");
  });
  document.getElementById("dialogo-importar").addEventListener("cancel", (e) => {
    if (importacionOcupada) e.preventDefault();
  });

  if (["#aires", "#resumen", "#reportes"].includes(location.hash)) estado.seccion = location.hash.slice(1);
  renderSeccion();
  cargarTodo();
  if (esVistaAnalitica()) cargarAnalitica();

  document.getElementById("secciones").addEventListener("click", (e) => {
    const boton = e.target.closest("[data-seccion]");
    if (boton) cambiarSeccion(boton.dataset.seccion);
  });

  const contenido = document.getElementById("contenido");

  // Todos los filtros los resuelve el backend: los de texto con espera, los
  // selectores al instante. Cualquier cambio vuelve a la página 1.
  contenido.addEventListener("input", (e) => {
    const { filtro, control } = e.target.dataset;
    if (filtro) {
      if (filtro === "busqueda") estado.consulta = e.target.value;
      else filtrosActuales()[filtro] = e.target.value;
      actualizarOpcionesFiltros();
      if (FILTROS_DE_TEXTO.has(filtro)) programarBusqueda(); else recargarDesdeElInicio();
    } else if (control === "orden") {
      estado.orden = { campo: e.target.value, sentido: "asc" };
      recargarDesdeElInicio();
    } else if (control === "tamPagina") {
      estado.tamPagina = Number(e.target.value);
      recargarDesdeElInicio();
    }
  });
  contenido.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && FILTROS_DE_TEXTO.has(e.target.dataset.filtro)) recargarDesdeElInicio(); // sin esperar
  });

  contenido.addEventListener("click", (e) => {
    const f = filtrosActuales();
    const objetivo = (selector) => e.target.closest(selector);
    let el;
    if ((el = objetivo("[data-pestana]"))) {
      if (f.pestana === el.dataset.pestana) return;
      estado.alcanceResumen = null;
      f.pestana = el.dataset.pestana;
      recargarDesdeElInicio();
    } else if ((el = objetivo("[data-area]"))) {
      f.area = f.area === el.dataset.area ? "" : el.dataset.area;
      recargarDesdeElInicio();
    } else if ((el = objetivo("[data-orden]"))) {
      ordenarPor(el.dataset.orden);
    } else if (objetivo('[data-control="sentido"]')) {
      ordenarPor(estado.orden.campo);
    } else if ((el = objetivo("[data-ir]"))) {
      // Desde la última página pedida (no la respondida): dos clics rápidos avanzan dos páginas.
      // Si se pasa de la última, cargarItems pide la última que existe.
      irAPagina(el.dataset.ir === "anterior" ? Math.max(1, estado.pagina - 1) : estado.pagina + 1);
    } else if ((el = objetivo('[data-accion="mas-filtros"]'))) {
      alternarMasFiltros(el);
    } else if (objetivo('[data-accion="limpiar"]')) {
      limpiarFiltros();
    }
  });

  // Formularios y diálogos.
  document.getElementById("form-item").addEventListener("submit", guardarItem);
  document.getElementById("f-tiene").addEventListener("change", sincronizarInventario);
  document.getElementById("form-item").addEventListener("input", (e) => {
    if (e.target.name) limpiarErrorCampo(e.target.name);
  });
  document.getElementById("form-stock").addEventListener("submit", guardarStock);
  document.getElementById("form-eliminar").addEventListener("submit", confirmarEliminar);
  document.querySelectorAll("[data-cerrar]").forEach((b) =>
    b.addEventListener("click", () => b.closest("dialog").close()));

  // Menú de acciones: se cierra al hacer clic afuera, con Escape, al desplazar o redimensionar.
  document.addEventListener("click", (e) => {
    if (menuAbierto && !menuAbierto.menu.contains(e.target)) cerrarMenu();
  });
  document.addEventListener("keydown", navegarMenu);
  window.addEventListener("resize", () => cerrarMenu());
  window.addEventListener("scroll", () => cerrarMenu(), { passive: true });
});
