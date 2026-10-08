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
  if (typeof equiposPC !== "undefined") ++equiposPC.solicitud;
  estado.cargando = false;
  estado.seccion = seccion;
  estado.analitica.datos = null;
  estado.analitica.error = "";
  estado.analitica.cargando = false;
  history.replaceState(null, "", seccion === "equipos" ? location.pathname : `#${seccion}`);
  renderSeccion();
  if (seccion === "pcs") cargarEquiposPC();
  else if (esVistaAnalitica()) cargarAnalitica(); else cargarItems();
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

  if (["#aires", "#resumen", "#reportes", "#pcs"].includes(location.hash)) estado.seccion = location.hash.slice(1);
  renderSeccion();
  cargarTodo();
  if (esVistaAnalitica()) cargarAnalitica();
  if (estado.seccion === "pcs") cargarEquiposPC();

  document.getElementById("secciones").addEventListener("click", (e) => {
    const boton = e.target.closest("[data-seccion]");
    if (boton) cambiarSeccion(boton.dataset.seccion);
  });

  const contenido = document.getElementById("contenido");

  // Todos los filtros los resuelve el backend: los de texto con espera, los
  // selectores al instante. Cualquier cambio vuelve a la página 1.
  function manejarEntradaFiltros(e) {
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
  }
  contenido.addEventListener("input", manejarEntradaFiltros);
  const filtrosLaterales = document.getElementById("filtros-laterales-contenido");
  filtrosLaterales.addEventListener("input", manejarEntradaFiltros);
  filtrosLaterales.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && FILTROS_DE_TEXTO.has(e.target.dataset.filtro)) recargarDesdeElInicio();
  });
  filtrosLaterales.addEventListener("click", (e) => {
    const mas = e.target.closest('[data-accion="mas-filtros"]');
    if (mas) alternarMasFiltros(mas);
    else if (e.target.closest('[data-accion="limpiar"]')) limpiarFiltros();
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
  document.querySelectorAll('[name="modo_registro"]').forEach((r) => r.addEventListener("change", sincronizarInventario));
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
