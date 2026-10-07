const fichaEquipo = { id: null, datos: null, error: "", cargando: false, solicitud: 0 };
const fichaLateral = () => window.matchMedia("(min-width: 1300px)").matches;

function prepararDistribucionInventario(montar) {
  const filtros = document.getElementById("filtros-laterales");
  const destino = document.getElementById("filtros-laterales-contenido");
  if (!montar) {
    document.body.classList.remove("inventario-maestro");
    destino.replaceChildren();
    filtros.hidden = true;
    if (estado.seccion !== "equipos") cerrarFichaEquipo(false);
    return;
  }
  if (estado.seccion !== "equipos") return;
  const barra = document.querySelector("#contenido .barra-filtros");
  destino.append(barra.querySelector(".barra-filtros__selectores"), barra.querySelector(".filtros-avanzados"));
  const etiquetas = { tipo: "Tipo", marca: "Marca", area: "Ubicación", condicion: "Condición", inventario: "Identificación" };
  destino.querySelectorAll("label > .solo-lector").forEach((label) => {
    label.className = "filtro-lateral__etiqueta";
    const control = label.parentElement.querySelector("select, input");
    if (control.tagName === "SELECT") control.dataset.todos = label.textContent;
    label.textContent = etiquetas[control.dataset.filtro] || label.textContent;
  });
  barra.append(document.querySelector("#contenido .orden"));
  document.querySelector("#contenido .vista-cabecera").append(document.getElementById("panel-inventario"));
  filtros.hidden = false;
  document.body.classList.add("inventario-maestro");
}

function mostrarFichaEquipo() {
  const dialogo = document.getElementById("ficha-equipo");
  if (!dialogo.open) {
    if (fichaLateral()) dialogo.show(); else dialogo.showModal();
  }
  document.body.classList.toggle("con-ficha", fichaLateral());
}

async function abrirFichaEquipo(id) {
  if (fichaEquipo.id === id && fichaEquipo.cargando) return;
  fichaEquipo.id = id;
  fichaEquipo.datos = null;
  fichaEquipo.error = "";
  fichaEquipo.cargando = true;
  const solicitud = ++fichaEquipo.solicitud;
  renderFichaEquipo();
  mostrarFichaEquipo();
  marcarSeleccionEquipo();
  try {
    const item = await api.getInventoryById(id);
    if (solicitud !== fichaEquipo.solicitud) return;
    fichaEquipo.datos = item;
  } catch (err) {
    if (solicitud !== fichaEquipo.solicitud) return;
    fichaEquipo.error = mensajeDeError(err);
  } finally {
    if (solicitud === fichaEquipo.solicitud) {
      fichaEquipo.cargando = false;
      renderFichaEquipo();
    }
  }
}

function cerrarFichaEquipo(devolverFoco = true) {
  const id = fichaEquipo.id;
  ++fichaEquipo.solicitud;
  fichaEquipo.id = null;
  fichaEquipo.datos = null;
  fichaEquipo.cargando = false;
  document.getElementById("ficha-equipo").close();
  document.body.classList.remove("con-ficha");
  marcarSeleccionEquipo();
  if (devolverFoco && id !== null) document.querySelector(`[data-ficha="${id}"]`)?.focus();
}

function marcarSeleccionEquipo() {
  document.querySelectorAll("[data-equipo]").forEach((fila) => {
    const seleccionada = Number(fila.dataset.equipo) === fichaEquipo.id;
    fila.classList.toggle("fila--seleccionada", seleccionada);
    fila.querySelector("[data-ficha]")?.setAttribute("aria-pressed", String(seleccionada));
  });
}

function sincronizarFichaEquipo() {
  if (fichaEquipo.id === null) return;
  const item = estado.items.find((it) => it.id === fichaEquipo.id);
  if (!estado.cargando && !estado.errorCarga && !item) { cerrarFichaEquipo(false); return; }
  const anterior = item && fichaEquipo.datos && Date.parse(item.updated_at) < Date.parse(fichaEquipo.datos.updated_at);
  if (item && fichaEquipo.datos && !anterior && !estado.cargando && !fichaEquipo.cargando && JSON.stringify(item) !== JSON.stringify(fichaEquipo.datos)) {
    fichaEquipo.datos = item;
    renderFichaEquipo();
  }
  marcarSeleccionEquipo();
}

function renderFichaEquipo() {
  const contenido = document.getElementById("ficha-contenido");
  const { datos: item, error, cargando } = fichaEquipo;
  contenido.setAttribute("aria-busy", String(cargando));
  if (cargando) {
    contenido.replaceChildren(h("h2", { id: "ficha-titulo", class: "solo-lector" }, "Ficha del equipo"), estadoCargando("Cargando equipo…"));
    return;
  }
  if (error) {
    contenido.replaceChildren(h("h2", { id: "ficha-titulo" }, "No se pudo cargar el equipo"), h("p", { role: "alert", class: "texto-suave" }, error),
      h("button", { type: "button", class: "boton boton--secundario", onclick: () => abrirFichaEquipo(fichaEquipo.id) }, "Reintentar"));
    return;
  }
  if (!item) return;
  const campos = [["Inventario", item.numero_inventario || "Sin N°"], ["N° de serie", item.numero_serie || "Sin serie"],
    ["Ubicación", item.ubicacion || "Sin especificar"], ["Condición", CONDICIONES[item.estado]?.etiqueta || item.estado], ["Cantidad", String(item.cantidad)]];
  contenido.replaceChildren(
    h("div", { class: "ficha-equipo__identidad" }, h("span", { class: "ficha-equipo__simbolo", "aria-hidden": "true" }, iconoNodo(ICONO_POR_TIPO[item.tipo_dispositivo] || "caja", 24)),
      h("h2", { id: "ficha-titulo" }, nombreItem(item)), h("p", { class: "texto-suave" }, item.tipo_dispositivo),
      h("div", { class: "ficha-equipo__disponibilidad" }, ...disponibilidad(item))),
    h("dl", { class: "ficha-equipo__campos" }, campos.map(([label, valor]) => h("div", {}, h("dt", {}, label), h("dd", {}, valor)))),
    h("section", { class: "ficha-equipo__observacion" }, h("h3", {}, "Observación"), h("p", {}, item.observacion || "Sin observaciones")),
    item.equipo_id ? enlaceEquipo(item.equipo_id) : null,
    h("div", { class: "ficha-equipo__acciones" },
      h("button", { type: "button", class: "boton boton--secundario", onclick: () => editarItem(item) }, iconoNodo("lapiz", 16), "Editar equipo"),
      h("button", { type: "button", class: "boton boton--secundario", onclick: () => abrirHistorial(item) }, iconoNodo("historial", 16), "Ver historial")));
}

document.addEventListener("DOMContentLoaded", () => {
  const filtros = document.getElementById("filtros-laterales");
  const ancho = window.matchMedia("(min-width: 768px)");
  const ajustarFiltros = () => { filtros.open = ancho.matches; };
  ajustarFiltros();
  ancho.addEventListener("change", ajustarFiltros);
  document.getElementById("ficha-cerrar").addEventListener("click", () => cerrarFichaEquipo());
  const dialogo = document.getElementById("ficha-equipo");
  dialogo.addEventListener("cancel", (e) => { e.preventDefault(); cerrarFichaEquipo(); });
  dialogo.addEventListener("close", () => { if (!dialogo.open && fichaEquipo.id !== null) cerrarFichaEquipo(false); });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && dialogo.open && fichaLateral() && !document.querySelector("dialog:modal")) cerrarFichaEquipo();
  });
  window.matchMedia("(min-width: 1300px)").addEventListener("change", () => {
    if (fichaEquipo.id === null) return;
    dialogo.close();
    mostrarFichaEquipo();
  });
});
