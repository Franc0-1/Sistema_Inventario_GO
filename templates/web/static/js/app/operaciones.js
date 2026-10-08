// ============================================================
// STOCK
// ============================================================

const formStock = { item: null, enviando: false };

async function enviarStock(id, cantidad) {
  const item = await api.updateInventoryStock(id, cantidad);
  aplicarCambio(item);
  return item;
}

function entregarUno(it) {
  return conFilaOcupada(it.id, async () => {
    try {
      const item = await enviarStock(it.id, it.cantidad - 1);
      avisar(`Se entregó 1 unidad de ${nombreItem(item)}. Quedan ${item.cantidad}.`);
    } catch (err) {
      avisar(mensajeDeError(err), "error");
      sincronizarSiNoExiste(err, it.id);
    }
  });
}

function abrirStock(it) {
  formStock.item = it;
  document.getElementById("stock-nombre").textContent = nombreItem(it);
  document.getElementById("stock-actual").textContent = `Cantidad actual: ${plural(it.cantidad, "unidad", "unidades")}`;
  const input = document.getElementById("s-cantidad");
  input.value = String(it.cantidad);
  document.getElementById("error-stock-cantidad").hidden = true;
  input.removeAttribute("aria-invalid");
  mostrarAlerta("form-stock-error", "");
  document.getElementById("dialogo-stock").showModal();
  input.select();
}

async function guardarStock(e) {
  e.preventDefault();
  if (formStock.enviando) return;
  const input = document.getElementById("s-cantidad");
  const error = document.getElementById("error-stock-cantidad");
  const texto = input.value.trim();
  const cantidad = texto === "" ? NaN : Number(texto);
  if (!Number.isInteger(cantidad) || cantidad < 0) {
    error.textContent = "Ingrese un número entero mayor o igual a 0.";
    error.hidden = false;
    input.setAttribute("aria-invalid", "true");
    input.focus();
    return;
  }
  error.hidden = true;
  input.removeAttribute("aria-invalid");

  const { id } = formStock.item;
  const boton = document.getElementById("form-stock-guardar");
  formStock.enviando = true;
  marcarOcupado(boton, true, "Guardando…");
  estado.pendientes.add(id);
  try {
    const item = await enviarStock(id, cantidad);
    document.getElementById("dialogo-stock").close();
    avisar(`Stock de ${nombreItem(item)} actualizado: ${plural(item.cantidad, "unidad", "unidades")}.`);
  } catch (err) {
    mostrarAlerta("form-stock-error", mensajeDeError(err));
    sincronizarSiNoExiste(err, id);
  } finally {
    formStock.enviando = false;
    estado.pendientes.delete(id);
    marcarOcupado(boton, false);
    renderResultados();
  }
}

// ============================================================
// ELIMINAR
// ============================================================

const formEliminar = { item: null, enviando: false };

function abrirEliminar(it) {
  formEliminar.item = it;
  document.getElementById("eliminar-nombre").textContent =
    `${nombreItem(it)}${it.numero_inventario ? ` · N° ${it.numero_inventario}` : ""}`;
  mostrarAlerta("form-eliminar-error", "");
  document.getElementById("dialogo-eliminar").showModal();
  document.querySelector("#dialogo-eliminar [data-cerrar].boton").focus(); // foco en "Cancelar": acción segura
}

// La fila se quita solo después de que el servidor confirma (204).
async function confirmarEliminar(e) {
  e.preventDefault();
  if (formEliminar.enviando) return;
  const it = formEliminar.item;
  const boton = document.getElementById("form-eliminar-confirmar");
  formEliminar.enviando = true;
  marcarOcupado(boton, true, "Eliminando…");
  estado.pendientes.add(it.id);
  try {
    await api.deleteInventoryItem(it.id);
    estado.pendientes.delete(it.id);
    quitarItem(it.id);
    document.getElementById("dialogo-eliminar").close();
    avisar(`Se eliminó ${nombreItem(it)}.`);
  } catch (err) {
    estado.pendientes.delete(it.id);
    if (err instanceof ApiError && err.code === "ITEM_NOT_FOUND") {
      document.getElementById("dialogo-eliminar").close();
      avisar(mensajeDeError(err), "error");
      quitarItem(it.id);
    } else {
      mostrarAlerta("form-eliminar-error", mensajeDeError(err));
      renderResultados();
    }
  } finally {
    formEliminar.enviando = false;
    marcarOcupado(boton, false);
  }
}

