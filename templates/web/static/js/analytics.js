async function cargarAnalitica() {
  const seccion = estado.seccion;
  if (!esVistaAnalitica()) return;
  const solicitud = ++estado.analitica.solicitud;
  estado.analitica.cargando = true;
  estado.analitica.error = "";
  renderAnalitica();
  try {
    const datos = await (seccion === "resumen" ? api.getSummary() : api.getReports(estado.reporteFiltros));
    if (solicitud !== estado.analitica.solicitud || seccion !== estado.seccion) return;
    estado.analitica.datos = datos;
  } catch (err) {
    if (solicitud !== estado.analitica.solicitud || seccion !== estado.seccion) return;
    estado.analitica.error = mensajeDeError(err);
  } finally {
    if (solicitud === estado.analitica.solicitud && seccion === estado.seccion) {
      estado.analitica.cargando = false;
      renderAnalitica();
    }
  }
}

function abrirConsultaResumen(filtros) {
  estado.consulta = "";
  estado.filtros.equipos = { ...filtrosVacios(), tipo: filtros.deviceType || "", area: filtros.location || "", condicion: filtros.status || "", inventario: filtros.hasInventory === undefined ? "" : String(filtros.hasInventory) };
  estado.alcanceResumen = { excludeDeviceType: "", includeRetired: Boolean(filtros.includeRetired),
    availability: filtros.availability || "" };
  estado.pagina = 1;
  cambiarSeccion("equipos");
}

function indicador(etiqueta, cantidad, filtros, clase = "", simbolo = "caja") {
  const props = { class: `indicador ${clase}` };
  if (filtros) { props.type = "button"; props.onclick = () => abrirConsultaResumen(filtros); }
  return h(filtros ? "button" : "div", props,
    h("span", { class: "indicador__encabezado" }, iconoNodo(simbolo, 16), h("span", {}, etiqueta),
      filtros ? iconoNodo("siguiente", 14) : null),
    h("span", { class: "indicador__valor" }, String(cantidad)));
}

function tablaReporte(titulo, filas, campo, incluirBajas, navegable) {
  const maximo = filas.reduce((max, fila) => Math.max(max, fila.registros), 1);
  return h("section", { class: `reporte-bloque reporte-bloque--${campo}` }, h("h3", {},
    iconoNodo(campo === "location" ? "pin" : campo === "status" ? "check" : "caja", 16), titulo),
    filas.length ? h("div", { class: "reporte-tabla-scroll" }, h("table", { class: "reporte-tabla" },
      h("thead", {}, h("tr", {}, [titulo, "Registros", "Unidades"].map((texto) => h("th", { scope: "col" }, texto)))),
      h("tbody", {}, filas.map((fila) => {
        const texto = campo === "status" ? (CONDICIONES[fila.valor]?.etiqueta || fila.valor) : fila.valor;
        return h("tr", {}, h("th", { scope: "row" }, h("div", { class: "reporte-etiqueta" }, navegable && fila.valor
          ? h("button", { type: "button", class: "reporte-enlace", onclick: () => abrirConsultaResumen({ [campo]: fila.valor, includeRetired: incluirBajas }) }, texto)
          : (texto || "Sin especificar")),
          h("span", { class: "reporte-barra", "aria-hidden": "true" }, h("span", { style: `width:${(fila.registros / maximo) * 100}%` }))),
          h("td", { class: "reporte-numero" }, String(fila.registros)), h("td", {}, String(fila.unidades)));
      })))) : h("p", { class: "texto-suave" }, "Sin registros."));
}

function filtrosReporte() {
  const selectorReporte = (campo, etiqueta, opciones) => h("label", { class: "campo" },
    h("span", {}, etiqueta), h("select", { id: `reporte-${campo}`, dataset: { reporte: campo } },
      h("option", { value: "", selected: !estado.reporteFiltros[campo] }, "Todos"),
      opciones.map((valor) => h("option", { value: valor, selected: estado.reporteFiltros[campo] === valor }, valor))));
  const opciones = (campo, filtro) => unicos([...estado.catalogo.map((it) => it[campo]), estado.reporteFiltros[filtro]]);
  return h("div", { class: "reporte-filtros" },
    selectorReporte("location", "Ubicación", opciones("ubicacion", "location")),
    selectorReporte("deviceType", "Tipo de dispositivo", opciones("tipo_dispositivo", "deviceType")),
    h("label", { class: "casilla" }, h("input", { id: "reporte-bajas", type: "checkbox", checked: estado.reporteFiltros.includeRetired, dataset: { reporte: "includeRetired" } }), "Incluir bajas"));
}

function renderAnalitica() {
  const foco = document.activeElement?.id;
  const resumen = estado.seccion === "resumen";
  const { datos, cargando, error } = estado.analitica;
  const contenido = document.getElementById("contenido");
  contenido.replaceChildren(h("header", { class: "analitica-cabecera" },
    h("div", {}, h("h2", {}, resumen ? "Resumen operativo" : "Reportes de inventario"),
      h("p", { class: "texto-suave" }, resumen ? "Inventario completo · Distribuciones con bajas incluidas" : "Registros y unidades del inventario seleccionado")),
    h("button", { id: "analitica-actualizar", type: "button", class: "boton-icono", title: "Actualizar", "aria-label": "Actualizar", disabled: cargando, onclick: cargarAnalitica }, iconoNodo("historial", 18))));
  if (!resumen) contenido.append(filtrosReporte());
  if (cargando) contenido.append(estadoCargando("Actualizando datos…"));
  else if (error) contenido.append(h("div", { class: "vacio vacio--error", role: "alert" },
    h("p", {}, error), h("button", { id: "analitica-reintentar", type: "button", class: "boton boton--secundario", onclick: cargarAnalitica }, "Reintentar")));
  else if (datos) {
    if (resumen) {
      contenido.append(h("div", { class: "indicadores" },
        indicador("Registros activos", datos.registros_activos, {}),
        indicador("Registros dados de baja", datos.registros_baja, { status: "BAJA" }, "indicador--baja", "packageMinus"),
        indicador("Unidades activas", datos.unidades_activas, null, "", "grafico"),
        indicador("Equipos disponibles · Activos con N°", datos.equipos_disponibles, { availability: "DISPONIBLE", hasInventory: true }, "indicador--disponible", "check"),
        indicador("Equipos prestados · Activos con N°", datos.equipos_prestados, { availability: "PRESTADO", hasInventory: true }, "indicador--prestado", "userCheck")));
    } else contenido.append(h("div", { class: "reporte-totales", role: "status" },
      h("p", {}, h("strong", {}, String(datos.registros)), " registros"),
      h("p", {}, h("strong", {}, String(datos.unidades)), " unidades"),
      h("span", { class: "texto-suave" }, estado.reporteFiltros.includeRetired ? "Bajas incluidas" : "Sin bajas")));
    if (!datos.registros) contenido.append(vacio(!resumen && (estado.reporteFiltros.location || estado.reporteFiltros.deviceType || !estado.reporteFiltros.includeRetired) && estado.catalogo.length
      ? "No hay resultados para estos filtros." : "No hay datos de inventario."));
    else contenido.append(h("div", { class: "reporte-distribuciones" },
      tablaReporte("Ubicación", datos.ubicaciones, "location", true, resumen),
      tablaReporte("Tipo de dispositivo", datos.tipos, "deviceType", true, resumen),
      tablaReporte("Condición", datos.condiciones, "status", true, resumen)));
    if (resumen) contenido.append(h("section", { class: "reporte-bloque" }, h("h3", {}, "Últimos movimientos"),
      datos.movimientos.length ? h("ol", { class: "movimientos-recientes" }, datos.movimientos.map((mov) => {
        const item = estado.catalogo.find((it) => it.id === mov.item_id);
        return h("li", {},
          h("span", { class: `movimiento-simbolo movimiento-simbolo--${mov.tipo}`, "aria-hidden": "true" },
            iconoNodo(mov.tipo === "stock_in" ? "flechaAbajo" : mov.tipo === "stock_out" ? "flechaArriba" : "flechas", 16)),
          h("div", { class: "movimiento-texto" }, h("p", {}, item ? nombreItem(item) : `Elemento #${mov.item_id}`),
            h("span", { class: "texto-suave" }, `#${mov.item_id} · ${TIPOS_MOVIMIENTO[mov.tipo]?.etiqueta || mov.tipo}`)),
          h("time", { datetime: mov.created_at }, formatearFechaHora(mov.created_at)),
          h("span", { class: "movimiento-cantidad" }, `${mov.cantidad_anterior} → ${mov.cantidad_nueva}`));
      }))
      : h("p", { class: "texto-suave" }, "Sin movimientos registrados.")));
  }
  if (foco) {
    const destino = document.getElementById(foco) || (foco === "analitica-reintentar" ? document.getElementById("analitica-actualizar") : null);
    destino?.focus();
  }
}

document.addEventListener("DOMContentLoaded", () => {
  document.getElementById("contenido").addEventListener("change", (e) => {
    const campo = e.target.dataset.reporte;
    if (!campo) return;
    estado.reporteFiltros[campo] = campo === "includeRetired" ? e.target.checked : e.target.value;
    cargarAnalitica();
  });
});
