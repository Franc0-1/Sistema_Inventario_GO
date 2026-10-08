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
  if (esVistaAnalitica() || estado.seccion === "pcs") return true;
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
      if (typeof cerrarFichaEquipo === "function") cerrarFichaEquipo(false);
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

