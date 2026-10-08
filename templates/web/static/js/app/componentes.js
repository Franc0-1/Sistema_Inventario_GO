// ============================================================
// COMPONENTES
// ============================================================

function celda(clase, etiqueta, ...contenido) {
  return h("div", { class: `celda ${clase}` }, h("span", { class: "celda__etiqueta" }, etiqueta), ...contenido);
}

// Nombre + ícono. El tipo no se muestra como texto: queda en el tooltip del ícono y en el filtro.
function celdaEquipo(it, seleccionable = false) {
  return h("div", { class: "celda celda--equipo" },
    h("div", { class: "equipo__icono", title: it.tipo_dispositivo || "Sin tipo" },
      iconoNodo(ICONO_POR_TIPO[it.tipo_dispositivo] || "caja", 18)),
    h("div", { class: "equipo__texto" },
      h("h3", { class: "equipo__nombre" }, seleccionable
        ? h("button", { type: "button", class: "equipo-seleccionar", dataset: { ficha: it.id }, "aria-label": `Ver ficha de ${nombreItem(it)}`, onclick: () => abrirFichaEquipo(it.id) }, nombreItem(it))
        : nombreItem(it), h("span", { class: "equipo__id" }, `#${it.id}`)),
      h("p", { class: "equipo__tipo" }, it.tipo_dispositivo || "Sin tipo"),
      it.observacion ? h("p", { class: "observacion" }, it.observacion) : null,
      it.equipo_id ? enlaceEquipo(it.equipo_id) : null));
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
    if (it.cantidad <= UMBRAL_BAJO_STOCK) {
      return [h("span", { class: "insignia insignia--aviso", title: `Quedan ${UMBRAL_BAJO_STOCK} unidades o menos` }, "Stock bajo")];
    }
    return [h("span", { class: "insignia insignia--exito" }, "En stock")];
  }
  if (estaPrestado(it)) {
    const desde = formatearFecha(it.fecha_prestamo);
    return [
      h("span", { class: "insignia insignia--aviso" }, iconoNodo("userCheck", 12), "Prestado"),
      it.asignado_a ? h("div", { class: "asignado" }, it.asignado_a) : null,
      desde ? h("div", { class: "texto-suave" }, `Desde ${desde}`) : null,
    ];
  }
  if (!esPrestable(it)) return [h("span", { class: "texto-suave", title: "Este tipo de equipo no se presta" }, "Uso fijo")];
  return [h("span", { class: "insignia insignia--exito" }, "Disponible")];
}

// Préstamos y devoluciones todavía no tienen endpoint: no se muestran botones
// que no hacen nada. La disponibilidad sigue visible en su columna.
function acciones(it) {
  const ocupado = estado.pendientes.has(it.id);
  let principal = null;
  if (!esIndividual(it) && !it.equipo_id) {
    principal = h("button", {
      type: "button", class: "boton boton--chico", disabled: ocupado || it.cantidad <= 0,
      title: it.cantidad <= 0 ? "Sin stock" : "Descontar una unidad del stock", onclick: () => entregarUno(it),
    }, iconoNodo("packageMinus", 14), "Entregar 1");
  }
  const mas = h("button", {
    type: "button", class: "boton-icono", "aria-label": `Más opciones para ${nombreItem(it)}`,
    "aria-haspopup": "menu", "aria-expanded": "false", disabled: ocupado,
    onclick: (e) => { e.stopPropagation(); alternarMenu(it, e.currentTarget); },
  }, iconoNodo("masOpciones", 18));
  return h("div", { class: "celda celda--acciones" }, principal, mas);
}

// Título y explicación de cada sección, con acciones opcionales a la derecha.
function cabeceraVista(titulo, descripcion, ...accionesVista) {
  return h("header", { class: "vista-cabecera" },
    h("div", {}, h("h2", {}, titulo), h("p", { class: "vista-cabecera__descripcion" }, descripcion)),
    accionesVista.length ? h("div", { class: "vista-cabecera__acciones" }, ...accionesVista) : null);
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
  const etiqueta = ({ tipo: "Tipo", marca: "Marca", area: "Ubicación", condicion: "Condición" })[filtro] || textoTodos;
  return h("label", { class: "selector" },
    h("span", { class: "solo-lector" }, textoTodos),
    h("select", { dataset: { filtro, todos: etiqueta } }),
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
  document.querySelectorAll("select[data-filtro]").forEach((select) => {
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

