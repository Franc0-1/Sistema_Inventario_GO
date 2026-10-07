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

// Indicador del resumen. Con `filtros` es un botón que abre el listado correspondiente.
function indicador({ etiqueta, ayuda, cantidad, filtros = null, clase = "", simbolo = "caja" }) {
  const props = { class: `indicador ${clase}` };
  if (filtros) {
    props.type = "button";
    props.title = "Ver el listado";
    props.onclick = () => abrirConsultaResumen(filtros);
  }
  return h(filtros ? "button" : "div", props,
    h("span", { class: "indicador__encabezado" }, iconoNodo(simbolo, 16), h("span", {}, etiqueta),
      filtros ? iconoNodo("siguiente", 14) : null),
    h("span", { class: "indicador__valor" }, String(cantidad)),
    h("span", { class: "indicador__ayuda" }, ayuda));
}

const TITULOS_REPORTE = { location: "Por área", deviceType: "Por tipo de dispositivo", status: "Por condición" };
const ICONOS_REPORTE = { location: "pin", deviceType: "caja", status: "check" };

function tablaReporte(filas, campo, incluirBajas, navegable) {
  const titulo = TITULOS_REPORTE[campo];
  const maximo = filas.reduce((max, fila) => Math.max(max, fila.registros), 1);
  return h("section", { class: `reporte-bloque reporte-bloque--${campo}` },
    h("h3", {}, iconoNodo(ICONOS_REPORTE[campo], 16), titulo),
    filas.length ? h("div", { class: "reporte-tabla-scroll" }, h("table", { class: "reporte-tabla" },
      h("thead", {}, h("tr", {},
        h("th", { scope: "col" }, h("span", { class: "solo-lector" }, titulo)),
        h("th", { scope: "col", title: "Cantidad de registros (filas) del inventario" }, "Elementos"),
        h("th", { scope: "col", title: "Suma de las cantidades" }, "Unidades"))),
      h("tbody", {}, filas.map((fila) => {
        const condicionFila = campo === "status" ? CONDICIONES[fila.valor] : null;
        const texto = condicionFila ? condicionFila.etiqueta : fila.valor;
        const barra = condicionFila ? ` reporte-barra--${condicionFila.clase}` : "";
        return h("tr", {},
          h("th", { scope: "row" },
            navegable && fila.valor
              ? h("button", { type: "button", class: "reporte-enlace", title: "Ver el listado",
                onclick: () => abrirConsultaResumen({ [campo]: fila.valor, includeRetired: incluirBajas }) }, texto)
              : h("span", {}, texto || "Sin especificar"),
            h("span", { class: `reporte-barra${barra}`, "aria-hidden": "true" },
              h("span", { style: `width:${(fila.registros / maximo) * 100}%` }))),
          h("td", { class: "reporte-numero" }, String(fila.registros)),
          h("td", {}, String(fila.unidades)));
      })))) : h("p", { class: "texto-suave" }, "Sin datos."));
}

function filtrosReporte() {
  const selectorReporte = (campo, etiqueta, textoTodos, opciones) => h("label", { class: "campo" },
    h("span", { class: "campo__etiqueta" }, etiqueta),
    h("span", { class: "selector" },
      h("select", { id: `reporte-${campo}`, dataset: { reporte: campo } },
        h("option", { value: "", selected: !estado.reporteFiltros[campo] }, textoTodos),
        opciones.map((valor) => h("option", { value: valor, selected: estado.reporteFiltros[campo] === valor }, valor))),
      iconoNodo("chevron", 16)));
  const opciones = (campo, filtro) => unicos([...estado.catalogo.map((it) => it[campo]), estado.reporteFiltros[filtro]]);
  return h("div", { class: "reporte-filtros" },
    selectorReporte("location", "Área", "Todas las áreas", opciones("ubicacion", "location")),
    selectorReporte("deviceType", "Tipo de dispositivo", "Todos los tipos", opciones("tipo_dispositivo", "deviceType")),
    h("label", { class: "casilla" },
      h("input", { id: "reporte-bajas", type: "checkbox", checked: estado.reporteFiltros.includeRetired, dataset: { reporte: "includeRetired" } }),
      "Incluir dados de baja"));
}

function listaMovimientos(movimientos) {
  return h("ol", { class: "movimientos-recientes" }, movimientos.map((mov) => {
    const item = estado.catalogo.find((it) => it.id === mov.item_id);
    const tipo = TIPOS_MOVIMIENTO[mov.tipo];
    return h("li", {},
      h("span", { class: `movimiento-simbolo movimiento-simbolo--${mov.tipo}`, "aria-hidden": "true" },
        iconoNodo(mov.tipo === "stock_in" ? "flechaAbajo" : mov.tipo === "stock_out" ? "flechaArriba" : "flechas", 16)),
      h("div", { class: "movimiento-texto" },
        h("p", {}, item ? nombreItem(item) : `Elemento #${mov.item_id}`),
        h("span", { class: "texto-suave" }, `${tipo?.etiqueta || mov.tipo} · #${mov.item_id}`)),
      h("time", { datetime: mov.created_at }, formatearFechaHora(mov.created_at)),
      h("span", { class: "movimiento-cantidad" },
        h("span", { class: "movimiento-cantidad__etiqueta" }, "Stock "), `${mov.cantidad_anterior} → ${mov.cantidad_nueva}`));
  }));
}

function renderAnalitica() {
  const foco = document.activeElement?.id;
  const resumen = estado.seccion === "resumen";
  const { datos, cargando, error } = estado.analitica;
  const contenido = document.getElementById("contenido");
  contenido.replaceChildren(cabeceraVista(
    resumen ? "Resumen" : "Reportes",
    resumen
      ? "Panorama general del inventario. Haga clic en un indicador o en un nombre para ver el listado."
      : "Cantidad de elementos y unidades por área, tipo y condición. Use los filtros para acotar el reporte.",
    h("button", { id: "analitica-actualizar", type: "button", class: "boton boton--secundario", disabled: cargando, onclick: cargarAnalitica },
      iconoNodo("actualizar", 16), "Actualizar")));
  if (!resumen) contenido.append(filtrosReporte());
  if (cargando) contenido.append(estadoCargando("Actualizando datos…"));
  else if (error) contenido.append(h("div", { class: "vacio vacio--error", role: "alert" },
    iconoNodo("alerta", 40), h("p", {}, error),
    h("button", { id: "analitica-reintentar", type: "button", class: "boton boton--secundario", onclick: cargarAnalitica }, "Reintentar")));
  else if (datos) {
    if (resumen) {
      contenido.append(h("div", { class: "indicadores" },
        indicador({ etiqueta: "Elementos activos", ayuda: "Todo lo que no está dado de baja",
          cantidad: datos.registros_activos, filtros: {} }),
        indicador({ etiqueta: "Unidades en total", ayuda: "Suma de las cantidades de los elementos activos",
          cantidad: datos.unidades_activas, simbolo: "grafico" }),
        indicador({ etiqueta: "Equipos disponibles", ayuda: "Con N° de inventario, listos para usar",
          cantidad: datos.equipos_disponibles, filtros: { availability: "DISPONIBLE", hasInventory: true },
          clase: "indicador--disponible", simbolo: "check" }),
        indicador({ etiqueta: "Equipos prestados", ayuda: "Con N° de inventario, entregados en préstamo",
          cantidad: datos.equipos_prestados, filtros: { availability: "PRESTADO", hasInventory: true },
          clase: "indicador--prestado", simbolo: "userCheck" }),
        indicador({ etiqueta: "Dados de baja", ayuda: "Ya no se usan; se conservan como registro",
          cantidad: datos.registros_baja, filtros: { status: "BAJA" }, clase: "indicador--baja", simbolo: "packageMinus" })));
    } else {
      contenido.append(h("div", { class: "reporte-totales", role: "status" },
        h("p", {}, h("strong", {}, String(datos.registros)), datos.registros === 1 ? " elemento" : " elementos"),
        h("p", {}, h("strong", {}, String(datos.unidades)), datos.unidades === 1 ? " unidad" : " unidades"),
        h("span", { class: "insignia insignia--neutra" },
          estado.reporteFiltros.includeRetired ? "Incluye dados de baja" : "Sin dados de baja")));
    }
    if (!datos.registros) contenido.append(vacio(!resumen && (estado.reporteFiltros.location || estado.reporteFiltros.deviceType || !estado.reporteFiltros.includeRetired) && estado.catalogo.length
      ? "No hay resultados para estos filtros." : "No hay datos de inventario."));
    else {
      if (resumen) contenido.append(h("p", { class: "nota-seccion" }, "Las distribuciones incluyen los elementos dados de baja."));
      contenido.append(h("div", { class: "reporte-distribuciones" },
        tablaReporte(datos.ubicaciones, "location", true, resumen),
        tablaReporte(datos.tipos, "deviceType", true, resumen),
        tablaReporte(datos.condiciones, "status", true, resumen)));
    }
    if (resumen) contenido.append(h("section", { class: "reporte-bloque" },
      h("h3", {}, iconoNodo("historial", 16), "Últimos movimientos de stock"),
      datos.movimientos.length ? listaMovimientos(datos.movimientos)
        : h("p", { class: "texto-suave" }, "Todavía no hay movimientos registrados.")));
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
