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

