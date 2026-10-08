// ============================================================
// ALTA Y EDICIÓN
// ============================================================

const formItem = { idEditando: null, enviando: false };
const campoForm = (nombre) => document.getElementById("form-item").elements[nombre];

// HasInventory se elige con dos opciones: equipo individual o material por cantidad.
const tieneInventarioForm = () => document.getElementById("f-modo-individual").checked;
function marcarTieneInventario(tiene) {
  document.getElementById(tiene ? "f-modo-individual" : "f-modo-stock").checked = true;
}

function llenarSugerencias() {
  const opciones = (valores) => valores.map((v) => h("option", { value: v }));
  document.getElementById("lista-tipos").replaceChildren(
    ...opciones(unicos([...TIPOS_SUGERIDOS, ...estado.catalogo.map((i) => i.tipo_dispositivo)])));
  document.getElementById("lista-areas").replaceChildren(...opciones(unicos(estado.catalogo.map((i) => i.ubicacion))));
}

// Coherencia HasInventory / InventoryNumber: sin inventario no hay número, y
// un equipo individual es unitario (así lo muestra la lista).
function sincronizarInventario() {
  const tiene = tieneInventarioForm();
  const numero = campoForm("numero_inventario");
  const cantidad = campoForm("cantidad");
  numero.disabled = !tiene;
  document.getElementById("campo-numero").hidden = !tiene;
  if (!tiene) {
    numero.value = "";
    limpiarErrorCampo("numero_inventario");
  }
  cantidad.disabled = tiene;
  if (tiene) cantidad.value = "1";
  document.getElementById("f-cantidad-ayuda").hidden = !tiene;
}

function abrirFormulario(item = null) {
  const form = document.getElementById("form-item");
  form.reset();
  limpiarErroresFormulario();
  llenarSugerencias();
  formItem.idEditando = item ? item.id : null;

  document.getElementById("dialogo-item-titulo").textContent = item ? `Editar elemento #${item.id}` : "Ingresar nuevo elemento";
  document.querySelector("#form-item-guardar .boton__texto").textContent = item ? "Guardar cambios" : "Crear elemento";

  const valores = item || { tiene_inventario: true, cantidad: 1, estado: "OPERATIVO" };
  marcarTieneInventario(Boolean(valores.tiene_inventario));
  for (const nombre of ["numero_inventario", "numero_serie", "tipo_dispositivo", "ubicacion", "marca", "modelo", "observacion"]) {
    campoForm(nombre).value = valores[nombre] || "";
  }
  campoForm("cantidad").value = String(valores.cantidad ?? 0);
  campoForm("estado").value = valores.estado || "OPERATIVO";
  prepararEquipoFormulario(item);
  sincronizarInventario();

  const fechas = document.getElementById("form-item-fechas");
  const alta = item && formatearFechaHora(item.created_at);
  const cambio = item && formatearFechaHora(item.updated_at);
  fechas.textContent = [alta && `Alta: ${alta}`, cambio && `Última modificación: ${cambio}`].filter(Boolean).join(" · ");
  fechas.hidden = !fechas.textContent;

  document.getElementById("dialogo-item").showModal();
  campoForm(item ? "marca" : "tipo_dispositivo").focus();
}

function leerFormulario() {
  const valor = (nombre) => campoForm(nombre).value.trim();
  const tiene = tieneInventarioForm();
  const cantidadTexto = valor("cantidad");
  return {
    numero_inventario: tiene ? valor("numero_inventario") : "",
    tiene_inventario: tiene,
    tipo_dispositivo: valor("tipo_dispositivo"),
    marca: valor("marca"),
    modelo: valor("modelo"),
    numero_serie: valor("numero_serie"),
    cantidad: cantidadTexto === "" ? NaN : Number(cantidadTexto),
    ubicacion: valor("ubicacion"),
    estado: campoForm("estado").value,
    observacion: valor("observacion"),
    ...(formItem.idEditando === null ? { equipo_id: Number(valor("equipo_id")) } : {}),
  };
}

// Validación básica para ayudar al usuario. La autoridad sigue siendo el backend.
function validarFormulario(datos) {
  const errores = {};
  const obligatorios = { tipo_dispositivo: "el tipo", marca: "la marca", modelo: "el modelo", ubicacion: "el área", estado: "la condición" };
  for (const [campo, nombre] of Object.entries(obligatorios)) {
    if (!datos[campo]) errores[campo] = `Ingrese ${nombre}.`;
  }
  if (datos.tiene_inventario && !datos.numero_inventario) errores.numero_inventario = "Ingrese el número de inventario.";
  if (!Number.isInteger(datos.cantidad) || datos.cantidad < 0) errores.cantidad = "Ingrese un número entero mayor o igual a 0.";
  return errores;
}

function mostrarErrorCampo(campo, mensaje) {
  const error = document.getElementById(`error-${campo}`);
  error.textContent = mensaje;
  error.hidden = false;
  const input = campoForm(campo);
  input.setAttribute("aria-invalid", "true");
  input.setAttribute("aria-describedby", error.id);
  input.closest(".campo").classList.add("campo--invalido");
}

function limpiarErrorCampo(campo) {
  const error = document.getElementById(`error-${campo}`);
  if (!error) return;
  error.hidden = true;
  const input = campoForm(campo);
  input.removeAttribute("aria-invalid");
  input.removeAttribute("aria-describedby");
  input.closest(".campo").classList.remove("campo--invalido");
}

function limpiarErroresFormulario() {
  document.querySelectorAll("#form-item [id^='error-']").forEach((e) => limpiarErrorCampo(e.id.slice("error-".length)));
  mostrarAlerta("form-item-error", "");
}

async function guardarItem(e) {
  e.preventDefault();
  if (formItem.enviando) return;

  limpiarErroresFormulario();
  const datos = leerFormulario();
  const errores = validarFormulario(datos);
  const campos = Object.keys(errores);
  if (campos.length) {
    campos.forEach((c) => mostrarErrorCampo(c, errores[c]));
    campoForm(campos[0]).focus();
    return;
  }

  const boton = document.getElementById("form-item-guardar");
  const editando = formItem.idEditando;
  formItem.enviando = true;
  marcarOcupado(boton, true, editando ? "Guardando…" : "Creando…");
  try {
    const item = editando
      ? await api.updateInventoryItem(editando, datos)
      : await api.createInventoryItem(datos);
    aplicarCambio(item);
    document.getElementById("dialogo-item").close();
    avisar(editando ? `Se guardaron los cambios de ${nombreItem(item)}.` : `Se creó ${nombreItem(item)}.`);
  } catch (err) {
    const campo = err instanceof ApiError ? CAMPO_DEL_ERROR[err.code] : null;
    if (campo) {
      mostrarErrorCampo(campo, mensajeDeError(err));
      campoForm(campo).focus();
    } else {
      mostrarAlerta("form-item-error", mensajeDeError(err));
    }
    if (editando) sincronizarSiNoExiste(err, editando);
  } finally {
    formItem.enviando = false;
    marcarOcupado(boton, false);
  }
}

// La edición parte de los datos actuales del servidor, no de la lista en pantalla.
function editarItem(it) {
  return conFilaOcupada(it.id, async () => {
    try {
      const actual = await api.getInventoryById(it.id);
      actualizarItem(actual);
      abrirFormulario(actual);
    } catch (err) {
      avisar(mensajeDeError(err), "error");
      sincronizarSiNoExiste(err, it.id);
    }
  });
}

