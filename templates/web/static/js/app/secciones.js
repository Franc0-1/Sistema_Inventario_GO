// ============================================================
// SECCIÓN: EQUIPOS Y STOCK
// ============================================================

function plantillaEquipos() {
  return [
    cabeceraVista("Inventario general",
      "Equipos con número de inventario y materiales que se controlan por cantidad."),
    h("div", { id: "panel-inventario", class: "panel-inventario", "aria-label": "Resumen del inventario completo" }),
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
    h("section", { id: "actividad-inventario", class: "actividad-inventario", "aria-label": "Últimos movimientos" }),
  ];
}

function filaEquipo(it) {
  const ocupado = estado.pendientes.has(it.id);
  return h("article", { class: `fila tabla--equipos${ocupado ? " fila--ocupada" : ""}`, dataset: { equipo: it.id }, "aria-busy": ocupado ? "true" : null,
    onclick: (e) => { if (!e.target.closest("button, a, input, select")) abrirFichaEquipo(it.id); } },
    celdaEquipo(it, true),
    celda("celda--ident", esIndividual(it) ? "Identificación" : "Cantidad", ...identificacion(it)),
    celda("celda--area", "Área", area(it)),
    celda("celda--condicion", "Condición", condicion(it)),
    celda("celda--disp", esIndividual(it) ? "Disponibilidad" : "Stock", ...disponibilidad(it)),
    acciones(it));
}

// Las pestañas son filtros del backend: el total de la pestaña activa lo da
// el contador de resultados.
function renderEquipos() {
  if (typeof renderPanelInventario === "function") renderPanelInventario();
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
    cabeceraVista("Aires acondicionados",
      "Equipos instalados, agrupados por área. Haga clic en un área para ver solo sus equipos."),
    h("section", { class: "resumen" },
      h("div", { class: "resumen__titulo" },
        h("h3", {}, "Distribución por área"),
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
  if (typeof prepararDistribucionInventario === "function") prepararDistribucionInventario(false);
  const ruta = document.getElementById("ruta-seccion");
  if (ruta) ruta.textContent = ({ equipos: "Inventario general", pcs: "Equipos", aires: "Aires acondicionados", resumen: "Resumen", reportes: "Reportes" })[estado.seccion];
  document.querySelectorAll("[data-seccion]").forEach((b) => {
    const activa = b.dataset.seccion === estado.seccion;
    b.classList.toggle("seccion--activa", activa);
    b.setAttribute("aria-current", activa ? "page" : "false");
  });
  if (estado.seccion === "pcs") { renderEquiposPC(); return; }
  if (esVistaAnalitica()) { renderAnalitica(); return; }
  document.getElementById("contenido").replaceChildren(
    ...(estado.seccion === "aires" ? plantillaAires() : plantillaEquipos()));
  if (typeof prepararDistribucionInventario === "function") prepararDistribucionInventario(true);
  if (estado.seccion === "equipos" && estado.alcanceResumen) {
    const { includeRetired, availability } = estado.alcanceResumen;
    const detalle = [
      "incluye aires acondicionados",
      includeRetired ? "incluye dados de baja" : null,
      availability ? `solo ${availability === "PRESTADO" ? "prestados" : "disponibles"}` : null,
    ].filter(Boolean).join(", ");
    document.getElementById("contenido").prepend(h("div", { class: "alcance-resumen", role: "status" },
      iconoNodo("info", 16),
      h("p", {}, h("strong", {}, "Listado abierto desde el Resumen"), ` (${detalle}).`),
      h("button", { type: "button", class: "boton boton--secundario", onclick: () => {
        estado.alcanceResumen = null; limpiarFiltros();
      } }, "Volver al listado normal")));
  }
  renderResultados();
}

// Actualiza resultados, filtros y contadores sin recrear el buscador.
function renderResultados() {
  cerrarMenu();
  if (estado.seccion === "pcs") { renderEquiposPC(); actualizarContadoresSecciones(); return; }
  document.getElementById("contenido").removeAttribute("aria-busy");
  if (esVistaAnalitica()) { renderAnalitica(); actualizarContadoresSecciones(); return; }
  if (estado.seccion === "aires") renderAires(); else renderEquipos();
  actualizarOpcionesFiltros();
  actualizarContadoresSecciones();
  if (typeof sincronizarFichaEquipo === "function") sincronizarFichaEquipo();
}

function actualizarContadoresSecciones() {
  const activos = (seccion) => catalogoDeSeccion(seccion).filter((it) => it.estado !== "BAJA").length;
  document.getElementById("cantidad-equipos").textContent = estado.catalogoCargado ? activos("equipos") : "–";
  document.getElementById("cantidad-aires").textContent = estado.catalogoCargado ? activos("aires") : "–";
}

