const panelInventario = { datos: null, cargando: false, error: "", solicitud: 0 };

async function cargarPanelInventario() {
  const solicitud = ++panelInventario.solicitud;
  panelInventario.cargando = true;
  panelInventario.error = "";
  renderPanelInventario();
  try {
    const datos = await api.getSummary();
    if (solicitud !== panelInventario.solicitud) return;
    panelInventario.datos = datos;
  } catch (err) {
    if (solicitud !== panelInventario.solicitud) return;
    panelInventario.datos = null;
    panelInventario.error = mensajeDeError(err);
  } finally {
    if (solicitud === panelInventario.solicitud) {
      panelInventario.cargando = false;
      renderPanelInventario();
    }
  }
}

function renderPanelInventario() {
  const panel = document.getElementById("panel-inventario");
  const actividad = document.getElementById("actividad-inventario");
  if (!panel || !actividad) return;
  const { datos, cargando, error } = panelInventario;
  panel.setAttribute("aria-busy", String(cargando));
  const indicadores = [
    ["registros activos", datos?.registros_activos],
    ["unidades activas", datos?.unidades_activas],
  ];
  panel.replaceChildren(...indicadores.map(([etiqueta, valor]) => h("span", {},
    h("strong", {}, valor === undefined ? "—" : String(valor)), ` ${etiqueta}`)));
  actividad.replaceChildren(h("h3", {}, "Últimos movimientos"));
  if (error) {
    actividad.append(h("div", { class: "panel-error", role: "alert" }, h("p", {}, "No se pudo actualizar el resumen del inventario."),
      h("button", { type: "button", class: "boton boton--secundario", onclick: cargarPanelInventario }, "Reintentar")));
  } else if (!datos) actividad.append(estadoCargando("Cargando movimientos…"));
  else if (!datos.movimientos.length) actividad.append(h("p", { class: "texto-suave" }, "Sin movimientos registrados."));
  else actividad.append(h("div", { class: "actividad-tabla-scroll", tabindex: "0", role: "region", "aria-label": "Movimientos recientes" }, h("table", { class: "actividad-tabla" },
    h("thead", {}, h("tr", {}, ["Fecha y hora", "Tipo", "Equipo / Artículo", "Cantidad", "Ubicación"].map((texto) => h("th", { scope: "col" }, texto)))),
    h("tbody", {}, datos.movimientos.slice(0, 5).map((mov) => {
      const item = estado.catalogo.find((it) => it.id === mov.item_id);
      return h("tr", {}, h("td", {}, formatearFechaHora(mov.created_at)),
        h("td", { class: `actividad-tipo actividad-tipo--${mov.tipo}` }, TIPOS_MOVIMIENTO[mov.tipo]?.etiqueta || mov.tipo),
        h("td", {}, item ? nombreItem(item) : `Elemento #${mov.item_id}`), h("td", {}, `${mov.cantidad_anterior} → ${mov.cantidad_nueva}`),
        h("td", {}, mov.ubicacion_destino || mov.ubicacion_origen || item?.ubicacion || "—"));
    })))));
}

document.addEventListener("DOMContentLoaded", () => {
  const actualizarEtiqueta = () => {
    document.getElementById("tema-etiqueta").textContent = document.documentElement.dataset.theme === "dark" ? "Modo oscuro" : "Modo claro";
  };
  actualizarEtiqueta();
  document.getElementById("boton-tema").addEventListener("click", actualizarEtiqueta);
  cargarPanelInventario();
});
