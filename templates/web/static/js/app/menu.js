// ============================================================
// MENÚ DE ACCIONES (⋮)
// ============================================================

let menuAbierto = null;

function alternarMenu(it, boton) {
  if (menuAbierto && menuAbierto.boton === boton) {
    cerrarMenu({ devolverFoco: true });
    return;
  }
  cerrarMenu();
  const opciones = [
    { texto: "Editar", icono: "lapiz", accion: () => editarItem(it) },
    esIndividual(it) ? null : { texto: "Ajustar stock", icono: "caja", accion: () => abrirStock(it) },
    { texto: "Historial", icono: "historial", accion: () => abrirHistorial(it) },
    { texto: "Eliminar", icono: "papelera", accion: () => abrirEliminar(it), peligro: true },
  ].filter(Boolean);

  const menu = h("div", { class: "menu-acciones", role: "menu", "aria-label": `Acciones para ${nombreItem(it)}` },
    opciones.map((o) => h("button", {
      type: "button", role: "menuitem",
      class: `menu-acciones__item${o.peligro ? " menu-acciones__item--peligro" : ""}`,
      onclick: () => { cerrarMenu(); o.accion(); },
    }, iconoNodo(o.icono, 16), o.texto)));
  document.body.append(menu);

  // Debajo del botón, o arriba si no entra; siempre dentro de la ventana.
  const r = boton.getBoundingClientRect();
  const top = r.bottom + 4 + menu.offsetHeight > innerHeight - 8 ? r.top - menu.offsetHeight - 4 : r.bottom + 4;
  const left = Math.max(8, Math.min(r.right - menu.offsetWidth, innerWidth - menu.offsetWidth - 8));
  menu.style.top = `${Math.max(8, top)}px`;
  menu.style.left = `${left}px`;

  boton.setAttribute("aria-expanded", "true");
  menuAbierto = { menu, boton };
  menu.querySelector("button").focus();
}

function cerrarMenu({ devolverFoco = false } = {}) {
  if (!menuAbierto) return;
  const { menu, boton } = menuAbierto;
  menuAbierto = null;
  menu.remove();
  boton.setAttribute("aria-expanded", "false");
  if (devolverFoco && boton.isConnected) boton.focus();
}

function navegarMenu(e) {
  if (!menuAbierto) return;
  const items = [...menuAbierto.menu.querySelectorAll("button")];
  const actual = items.indexOf(document.activeElement);
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    e.preventDefault();
    const paso = e.key === "ArrowDown" ? 1 : -1;
    items[(actual + paso + items.length) % items.length].focus();
  } else if (e.key === "Escape") {
    cerrarMenu({ devolverFoco: true });
  } else if (e.key === "Tab") {
    cerrarMenu();
  }
}

