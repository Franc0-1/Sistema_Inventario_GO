const equiposPC = { datos: null, cargando: false, error: "", solicitud: 0, seleccionado: null, busqueda: "", bajas: false, area: "", orden: "id" };
const FALLAS = new Set(["EN_REPARACION", "FUERA_DE_SERVICIO"]);
const mismaUbicacion = (a, b) => (a || "").trim().toLowerCase() === (b || "").trim().toLowerCase();
const ORDENES_PC = { id: ["Orden de alta", (a, b) => a.gabinete.id - b.gabinete.id],
  nombre: ["Nombre", (a, b) => nombrePC(a.gabinete).localeCompare(nombrePC(b.gabinete), "es", { numeric: true })],
  ubicacion: ["Ubicación", (a, b) => (a.gabinete.ubicacion || "").localeCompare(b.gabinete.ubicacion || "", "es") || a.gabinete.id - b.gabinete.id],
  componentes: ["Componentes", (a, b) => b.componentes.length - a.componentes.length || a.gabinete.id - b.gabinete.id] };

const vinculoPC = { equipo: null, componente: null, cargando: false, enviando: false, solicitud: 0 };
const esGabinete = (item) => ["CPU", "GABINETE", "PC"].includes((item.tipo_dispositivo || "").trim().toUpperCase());
const esEquipoPC = (item) => esGabinete(item) && !item.equipo_id && item.tiene_inventario && item.cantidad === 1;
const nombrePC = (item) => `PC ${item.numero_inventario}`;

function enlaceEquipo(id) {
  const gabinete = estado.catalogo.find(item => item.id === id);
  return h("button", { type: "button", class: "equipo-enlace", onclick: () => abrirEquipoPC(id) },
    iconoNodo("monitor", 12), gabinete ? `Pertenece a ${nombrePC(gabinete)}` : `Equipo #${id}`);
}

function abrirEquipoPC(id) {
  equiposPC.seleccionado = id;
  equiposPC.enfocar = true;
  if (estado.seccion === "pcs") { renderEquiposPC(); enfocarEquipoPC(); equiposPC.enfocar = false; }
  else cambiarSeccion("pcs");
}

function enfocarEquipoPC() { document.getElementById("pc-detalle-titulo")?.focus(); }

function invalidarEquipos() {
  ++equiposPC.solicitud;
  equiposPC.datos = null;
  if (estado.seccion === "pcs") cargarEquiposPC();
}

async function cargarEquiposPC() {
  if (estado.seccion !== "pcs") return;
  const solicitud = ++equiposPC.solicitud;
  equiposPC.cargando = true;
  equiposPC.error = "";
  renderEquiposPC();
  try {
    const datos = await api.getEquipments();
    if (solicitud !== equiposPC.solicitud || estado.seccion !== "pcs") return;
    equiposPC.datos = datos;
    if (!datos.some(pc => pc.gabinete.id === equiposPC.seleccionado)) equiposPC.seleccionado = null;
  } catch (err) {
    if (solicitud !== equiposPC.solicitud || estado.seccion !== "pcs") return;
    equiposPC.error = mensajeDeError(err);
  } finally {
    if (solicitud === equiposPC.solicitud && estado.seccion === "pcs") {
      equiposPC.cargando = false;
      renderEquiposPC();
      if (equiposPC.enfocar) { enfocarEquipoPC(); equiposPC.enfocar = false; }
    }
  }
}

function prepararEquipoFormulario(item) {
  const select = document.getElementById("f-equipo");
  const opciones = estado.catalogo.filter(esEquipoPC);
  select.replaceChildren(h("option", { value: "0" }, "Sin equipo"),
    ...opciones.map(pc => h("option", { value: String(pc.id) }, `${nombrePC(pc)} · ${pc.ubicacion}`)));
  if (item?.equipo_id && !opciones.some(pc => pc.id === item.equipo_id)) select.append(h("option", { value: String(item.equipo_id) }, `Equipo #${item.equipo_id}`));
  select.value = String(item?.equipo_id || 0);
  select.disabled = Boolean(item);
  document.getElementById("campo-equipo").hidden = Boolean(item && !item.equipo_id);
}

function renderEquiposPC() {
  if (estado.seccion !== "pcs") return;
  const contenido = document.getElementById("contenido");
  const foco = document.activeElement;
  const idFoco = foco?.id;
  const posicion = foco?.selectionStart;
  const { datos, cargando, error, busqueda, bajas, area, orden } = equiposPC;
  const areas = unicos((datos || []).map(pc => pc.gabinete.ubicacion));
  const selectPC = (id, etiqueta, valor, opciones, alCambiar) => h("label", { class: "selector" }, h("span", { class: "solo-lector" }, etiqueta),
    h("select", { id, onchange: (e) => { alCambiar(e.target.value); renderEquiposPC(); } }, opciones.map(([v, t]) => h("option", { value: v, selected: v === valor }, t))), iconoNodo("chevron", 16));
  contenido.replaceChildren(cabeceraVista("Equipos", "",
    h("button", { id: "pcs-actualizar", type: "button", class: "boton-icono pcs-actualizar", disabled: cargando, title: "Actualizar", "aria-label": "Actualizar equipos", onclick: cargarEquiposPC }, iconoNodo("actualizar", 18)),
    h("button", { type: "button", class: "boton boton--primario", onclick: () => { abrirFormulario(); campoForm("tipo_dispositivo").value = "Gabinete"; } }, iconoNodo("plus", 16), "Nuevo equipo")));
  contenido.setAttribute("aria-busy", String(cargando));
  if (cargando && !datos) { contenido.append(estadoCargando("Cargando equipos…")); return; }
  if (error) {
    contenido.append(h("div", { class: "vacio vacio--error", role: "alert" }, h("p", {}, error), h("button", { type: "button", class: "boton boton--secundario", onclick: cargarEquiposPC }, "Reintentar")));
    return;
  }
  if (!datos) return;

  const query = busqueda.trim().toLocaleLowerCase();
  const visibles = datos.filter(pc => (bajas || pc.gabinete.estado !== "BAJA") && (!area || pc.gabinete.ubicacion === area) && (!query || [pc.gabinete, ...pc.componentes].some(it =>
    [it.numero_inventario, it.numero_serie, it.ubicacion, it.marca, it.modelo, it.tipo_dispositivo].some(text => String(text || "").toLocaleLowerCase().includes(query)))));
  visibles.sort(ORDENES_PC[orden][1]);
  // En pantalla ancha siempre hay un equipo abierto, como en una ficha maestro-detalle.
  if (!visibles.some(pc => pc.gabinete.id === equiposPC.seleccionado) && visibles.length && window.matchMedia("(min-width: 1100px)").matches) {
    equiposPC.seleccionado = visibles[0].gabinete.id;
  }
  const seleccionado = visibles.find(pc => pc.gabinete.id === equiposPC.seleccionado);
  const buscar = h("input", { id: "pcs-buscar", type: "search", class: "buscador__input", value: busqueda, placeholder: "Buscar equipo…", title: "Busca en PC, componentes, N° de inventario, serie y ubicación",
    oninput: (e) => { equiposPC.busqueda = e.target.value; clearTimeout(equiposPC.espera); equiposPC.espera = setTimeout(renderEquiposPC, 200); } });

  contenido.append(h("div", { class: "pcs-vista" },
    h("aside", { class: "pcs-listado", "aria-label": "Lista de equipos" },
      h("label", { class: "buscador" }, iconoNodo("search", 16), h("span", { class: "solo-lector" }, "Buscar equipos"), buscar),
      selectPC("pcs-area", "Área", area, [["", "Todas las áreas"], ...areas.map(a => [a, a])], v => { equiposPC.area = v; }),
      h("div", { class: "pcs-opciones" },
        selectPC("pcs-orden", "Ordenar por", orden, Object.entries(ORDENES_PC).map(([v, [t]]) => [v, `Ordenar: ${t}`]), v => { equiposPC.orden = v; }),
        h("label", { class: "casilla" }, h("input", { id: "pcs-bajas", type: "checkbox", checked: bajas, onchange: (e) => { equiposPC.bajas = e.target.checked; renderEquiposPC(); } }), "Bajas")),
      h("p", { class: "pcs-cantidad" }, plural(visibles.length, "equipo", "equipos")),
      visibles.length ? h("ul", { class: "pcs-lista" }, visibles.map(pc => h("li", {}, tarjetaPC(pc, pc === seleccionado))))
        : vacio(datos.length ? "Sin resultados para estos filtros." : "Sin equipos registrados.")),
    h("section", { class: "pcs-detalle", "aria-label": "Detalle del equipo" }, seleccionado ? detalleEquipoPC(seleccionado) : resumenEquiposPC(visibles))));

  if (idFoco) {
    const nuevo = document.getElementById(idFoco);
    nuevo?.focus({ preventScroll: true });
    if (nuevo?.type === "search" && posicion !== null) nuevo.setSelectionRange(posicion, posicion);
  }
}

const ubicacionCorta = (texto) => (texto || "Sin área").replace(/\s*\/\s*/g, " · ");

function tarjetaPC(pc, activa) {
  const fallas = pc.componentes.filter(it => FALLAS.has(it.estado)).length;
  return h("button", { id: `pc-${pc.gabinete.id}`, type: "button", class: `pcs-tarjeta${activa ? " pcs-tarjeta--activa" : ""}`,
    "aria-pressed": String(activa), onclick: () => abrirEquipoPC(pc.gabinete.id) },
  iconoNodo("monitor", 22),
  h("span", { class: "pcs-tarjeta__texto" },
    h("strong", {}, nombrePC(pc.gabinete)),
    h("span", {}, ubicacionCorta(pc.gabinete.ubicacion)),
    h("span", {}, plural(pc.componentes.length, "componente", "componentes"),
      fallas ? h("span", { class: "pcs-falla" }, ` · ${plural(fallas, "con falla", "con falla")}`) : null)),
  iconoNodo("siguiente", 16));
}

function detalleEquipoPC(pc) {
  const gabinete = pc.gabinete;
  const c = CONDICIONES[gabinete.estado] || { etiqueta: gabinete.estado || "Sin condición", clase: "neutro" };
  const datos = [["Gabinete", nombreItem(gabinete)], ["Inventario", gabinete.numero_inventario || "—"],
    gabinete.numero_serie ? ["Serie", gabinete.numero_serie] : null, ["Ubicación", gabinete.ubicacion || "—"]].filter(Boolean);
  return h("div", { class: "pcs-ficha" },
    h("header", { class: "pcs-ficha__cabecera" },
      h("div", { class: "pcs-ficha__identidad" },
        h("div", { class: "pcs-ficha__titulo" },
          h("h2", { id: "pc-detalle-titulo", tabindex: "-1" }, nombrePC(gabinete)),
          h("span", { class: `pcs-estado pcs-estado--${c.clase}` }, h("span", { class: "condicion__punto" }), c.etiqueta)),
        h("p", { class: "pcs-ficha__ubicacion" }, iconoNodo("pin", 16), gabinete.ubicacion || "Sin área")),
      h("div", { class: "pcs-ficha__acciones" },
        h("button", { type: "button", class: "boton boton--secundario", onclick: () => editarItem(gabinete) }, iconoNodo("lapiz", 16), "Editar gabinete"),
        h("button", { type: "button", class: "boton boton--secundario", onclick: () => abrirHistorial(gabinete) }, iconoNodo("historial", 16), "Historial"),
        h("button", { type: "button", class: "boton-icono pcs-cerrar", "aria-label": "Cerrar detalle",
          onclick: () => { equiposPC.seleccionado = null; renderEquiposPC(); document.getElementById(`pc-${gabinete.id}`)?.focus(); } }, iconoNodo("x", 16)))),
    h("dl", { class: "pcs-datos" }, datos.map(([etiqueta, valor]) => h("div", {}, h("dt", {}, etiqueta), h("dd", {}, valor)))),
    h("div", { class: "pcs-subcabecera" }, h("h3", {}, "Componentes vinculados"),
      h("div", { class: "pcs-ficha__acciones" },
        h("button", { type: "button", class: "boton boton--secundario", disabled: vinculoPC.enviando, onclick: () => abrirVinculoPC(gabinete) }, iconoNodo("vincular", 16), "Vincular existente"),
        h("button", { type: "button", class: "boton boton--primario", onclick: () => { abrirFormulario(); document.getElementById("campo-equipo").hidden = false; campoForm("equipo_id").value = String(gabinete.id); } }, iconoNodo("plus", 16), "Crear componente"))),
    pc.componentes.length ? tablaComponentesPC(pc) : h("p", { class: "texto-suave pcs-sin-componentes" }, "Sin componentes vinculados."),
    h("p", { class: "pcs-pie" }, plural(pc.componentes.length, "componente vinculado", "componentes vinculados")),
    h("section", { class: "pcs-observaciones" }, h("h3", {}, "Observaciones"), h("p", {}, gabinete.observacion || "Sin observaciones.")));
}

function tablaComponentesPC(pc) {
  const gabinete = pc.gabinete;
  return h("div", { class: "pcs-tabla-scroll" }, h("table", { class: "pcs-tabla" },
    h("thead", {}, h("tr", {}, ["Componente", "Inventario", "Serie", "Condición", "Acciones"].map(t => h("th", { scope: "col" }, t)))),
    h("tbody", {}, pc.componentes.map(item => h("tr", {},
      h("th", { scope: "row" }, h("div", { class: "pcs-comp" }, iconoNodo(ICONO_POR_TIPO[item.tipo_dispositivo] || "caja", 20),
        h("div", {}, h("strong", {}, item.tipo_dispositivo), h("span", {}, nombreItem(item)),
          mismaUbicacion(item.ubicacion, gabinete.ubicacion) ? null
            : h("span", { class: "pcs-aviso" }, iconoNodo("alerta", 12), `En otra ubicación: ${item.ubicacion || "sin área"}`)))),
      h("td", { "data-etiqueta": "Inventario" }, item.numero_inventario || "—"),
      h("td", { "data-etiqueta": "Serie" }, item.numero_serie || "—"),
      h("td", { "data-etiqueta": "Condición" }, condicion(item)),
      h("td", { class: "pcs-tabla__acciones" },
        h("button", { type: "button", class: "boton-icono", title: "Editar componente", "aria-label": `Editar ${nombreItem(item)}`, onclick: () => editarItem(item) }, iconoNodo("lapiz", 16)),
        h("button", { type: "button", class: "boton-icono", title: "Historial", "aria-label": `Historial de ${nombreItem(item)}`, onclick: () => abrirHistorial(item) }, iconoNodo("historial", 16)),
        h("button", { type: "button", class: "boton-icono", title: "Desvincular del equipo", "aria-label": `Desvincular ${nombreItem(item)}`, disabled: vinculoPC.enviando, onclick: () => abrirVinculoPC(gabinete, item) }, iconoNodo("desvincular", 16))))))));
}

// Panel sin selección: panorama de los equipos visibles.
function resumenEquiposPC(lista) {
  const componentes = lista.reduce((n, pc) => n + pc.componentes.length, 0);
  const datos = [["Equipos", lista.length], ["Componentes", componentes],
    ["Con fallas", lista.filter(pc => FALLAS.has(pc.gabinete.estado) || pc.componentes.some(it => FALLAS.has(it.estado))).length],
    ["Sin componentes", lista.filter(pc => !pc.componentes.length).length]];
  return h("div", {}, h("h2", {}, "Resumen"),
    h("dl", { class: "pcs-panorama" }, datos.map(([label, valor]) => h("div", {}, h("dt", {}, label), h("dd", {}, String(valor))))),
    h("p", { class: "texto-suave" }, "Seleccione un equipo para ver sus componentes."));
}

async function abrirVinculoPC(gabinete, componente = null) {
  vinculoPC.equipo = gabinete;
  vinculoPC.componente = componente;
  vinculoPC.cargando = !componente;
  const solicitud = ++vinculoPC.solicitud;
  const dialogo = document.getElementById("dialogo-vincular");
  const select = document.getElementById("vincular-componente");
  document.getElementById("vincular-titulo").textContent = componente ? "Desvincular componente" : "Vincular componente";
  document.getElementById("vincular-descripcion").textContent = componente ? `${nombreItem(componente)} dejará de pertenecer a ${nombrePC(gabinete)}. Se conservará en el inventario con su número original.` : nombrePC(gabinete);
  document.getElementById("vincular-campo").hidden = Boolean(componente);
  select.required = !componente;
  select.replaceChildren(h("option", { value: "" }, "Cargando…"));
  mostrarAlerta("vincular-error", "");
  document.getElementById("vincular-guardar").textContent = componente ? "Desvincular" : "Vincular";
  document.getElementById("vincular-guardar").disabled = !componente;
  dialogo.showModal();
  try {
    if (componente) return;
    const { items } = await api.getInventory({ includeRetired: true });
    if (solicitud !== vinculoPC.solicitud || !dialogo.open) return;
    const candidatos = items.filter(it => !esGabinete(it) && !it.equipo_id && it.cantidad === 1 && it.estado !== "BAJA");
    const area = vinculoPC.equipo.ubicacion;
    const opcion = it => h("option", { value: String(it.id) }, `#${it.id} · ${it.tipo_dispositivo} · ${nombreItem(it)} · ${it.numero_inventario || "Sin número"} · ${it.ubicacion}`);
    const cercanos = candidatos.filter(it => mismaUbicacion(it.ubicacion, area)), otros = candidatos.filter(it => !mismaUbicacion(it.ubicacion, area));
    const grupo = (etiqueta, lista) => lista.length ? h("optgroup", { label: etiqueta }, lista.map(opcion)) : null;
    select.replaceChildren(h("option", { value: "" }, candidatos.length ? "Seleccionar componente" : "Sin componentes disponibles"),
      grupo(`Misma área (${area})`, cercanos), grupo("Otras áreas", otros));
    document.getElementById("vincular-guardar").disabled = !candidatos.length;
    select.focus();
  } catch (err) {
    if (solicitud === vinculoPC.solicitud && dialogo.open) mostrarAlerta("vincular-error", mensajeDeError(err));
  } finally { if (solicitud === vinculoPC.solicitud) vinculoPC.cargando = false; }
}

async function guardarVinculoPC(e) {
  e.preventDefault();
  if (vinculoPC.enviando || vinculoPC.cargando) return;
  const componente = vinculoPC.componente;
  const id = componente?.id || Number(document.getElementById("vincular-componente").value);
  if (!id) return;
  vinculoPC.enviando = true;
  const dialogo = document.getElementById("dialogo-vincular");
  dialogo.querySelectorAll("button, select").forEach(el => { el.disabled = true; });
  mostrarAlerta("vincular-error", "");
  try {
    const item = await api.setEquipment(id, componente ? 0 : vinculoPC.equipo.id);
    aplicarCambio(item);
    dialogo.close();
    avisar(componente ? "Componente desvinculado. Se conserva en el inventario." : "Componente vinculado al equipo.");
  } catch (err) { mostrarAlerta("vincular-error", mensajeDeError(err)); }
  finally {
    vinculoPC.enviando = false;
    dialogo.querySelectorAll("button, select").forEach(el => { el.disabled = false; });
    if (dialogo.open) document.getElementById("vincular-guardar").focus();
    else enfocarEquipoPC();
  }
}

document.addEventListener("DOMContentLoaded", () => {
  document.getElementById("form-vincular").addEventListener("submit", guardarVinculoPC);
  const dialogo = document.getElementById("dialogo-vincular");
  dialogo.addEventListener("cancel", e => { if (vinculoPC.enviando) e.preventDefault(); });
  dialogo.addEventListener("close", () => { ++vinculoPC.solicitud; });
});
