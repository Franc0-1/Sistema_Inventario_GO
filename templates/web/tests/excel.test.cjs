const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

function entorno() {
  const nodos = new Map();
  const nodo = (id) => {
    if (!nodos.has(id)) nodos.set(id, {
      textContent: "", hidden: false, disabled: false, checked: false, files: [], dataset: {},
      classList: { toggle() {}, contains: () => false },
      setAttribute() {}, querySelector: () => nodo(`${id}-texto`),
      querySelectorAll: () => [], reset() {}, showModal() { this.open = true; }, close() { this.open = false; },
    });
    return nodos.get(id);
  };
  const sandbox = {
    console: { error() {} }, FormData, Blob, File, URLSearchParams, setTimeout, clearTimeout, queueMicrotask,
    URL: { createObjectURL: () => "blob:excel", revokeObjectURL() {} },
    document: { getElementById: nodo, addEventListener() {}, body: { append() {} } },
    fetch: async () => { throw new Error("sin red"); },
  };
  vm.createContext(sandbox);
  for (const nombre of ["api", "app"]) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, `../static/js/${nombre}.js`), "utf8"), sandbox);
  }
  vm.runInContext(`
    renderResultados = () => {};
    avisar = () => {};
    globalThis.prueba = { api, estado, ApiError, exportarExcel, importarExcel, cargarItems, cargarCatalogo };
  `, sandbox);
  return { sandbox, nodo, ...sandbox.prueba };
}

const diferida = () => {
  let resolve;
  const promise = new Promise((r) => { resolve = r; });
  return { promise, resolve };
};
const enviar = { preventDefault() {} };
function seleccionar(e, nombre = "datos.xlsx") {
  e.nodo("importar-archivo").files = [new File(["excel"], nombre)];
  e.nodo("importar-confirmacion").checked = true;
}

test("exporta sin filtros y respeta Content-Disposition", async () => {
  const e = entorno();
  e.sandbox.fetch = async (url) => {
    assert.equal(url, "/api/inventory/export");
    return new Response("excel", { headers: { "Content-Disposition": 'attachment; filename="inventario_fecha.xlsx"' } });
  };
  const resultado = await e.api.exportInventory();
  assert.equal(resultado.nombre, "inventario_fecha.xlsx");
  assert.equal(await resultado.archivo.text(), "excel");
});

test("exportación rechaza errores antes de crear una descarga", async () => {
  const e = entorno();
  e.sandbox.fetch = async () => new Response(JSON.stringify({ error: { code: "INTERNAL_ERROR", message: "interno" } }), { status: 500 });
  let descargas = 0;
  e.sandbox.URL.createObjectURL = () => { descargas++; };
  await e.exportarExcel();
  assert.equal(descargas, 0);
});

test("exportación bloquea doble envío y libera la URL temporal", async () => {
  const e = entorno();
  const pendiente = diferida();
  let llamadas = 0;
  let liberada;
  e.api.exportInventory = () => { llamadas++; return pendiente.promise; };
  e.sandbox.URL.revokeObjectURL = (url) => { liberada = url; };
  e.sandbox.setTimeout = (fn) => fn();
  vm.runInContext('h = () => ({ click() {}, remove() {} })', e.sandbox);
  const primera = e.exportarExcel();
  await e.exportarExcel();
  assert.equal(llamadas, 1);
  assert.equal(e.nodo("boton-exportar").disabled, true);
  pendiente.resolve({ archivo: new Blob(["excel"]), nombre: "export.xlsx" });
  await primera;
  assert.equal(liberada, "blob:excel");
  assert.equal(e.nodo("boton-exportar").disabled, false);
});

test("importación envía FormData sin Content-Type manual", async () => {
  const e = entorno();
  e.sandbox.fetch = async (url, opciones) => {
    assert.equal(url, "/api/inventory/import");
    assert.equal(opciones.method, "POST");
    assert.equal(opciones.headers["Content-Type"], undefined);
    assert.equal(opciones.body.get("file").name, "datos.xlsx");
    return Response.json({ data: { imported: 12 } });
  };
  assert.equal((await e.api.importInventory(new File(["excel"], "datos.xlsx"))).imported, 12);
});

test("archivo inválido y falta de confirmación impiden enviar", async () => {
  const e = entorno();
  let llamadas = 0;
  e.api.importInventory = async () => { llamadas++; };
  await e.importarExcel(enviar);
  assert.match(e.nodo("form-importar-error").textContent, /xlsx/);
  seleccionar(e, "datos.csv");
  await e.importarExcel(enviar);
  seleccionar(e);
  e.nodo("importar-confirmacion").checked = false;
  await e.importarExcel(enviar);
  assert.match(e.nodo("form-importar-error").textContent, /Confirme/);
  assert.equal(llamadas, 0);
});

test("éxito bloquea doble envío y actualiza inventario, filtros y contadores", async () => {
  const e = entorno();
  seleccionar(e);
  const pendiente = diferida();
  let envios = 0;
  e.api.importInventory = () => { envios++; return pendiente.promise; };
  e.estado.pagina = 4;
  e.estado.filtros.equipos.marca = "Anterior";
  e.estado.filtros.equipos.area = "Sistemas";
  e.api.getInventory = async (parametros) => {
    if (!parametros.includeRetired) assert.equal(parametros.page, 1);
    return { items: [{ id: 20, marca: "Nueva", ubicacion: "Sistemas", tipo_dispositivo: "Notebook" }], pagination: { totalPages: 1 } };
  };
  const primera = e.importarExcel(enviar);
  await e.importarExcel(enviar);
  assert.equal(envios, 1);
  assert.equal(e.nodo("importar-archivo").disabled, true);
  pendiente.resolve({ imported: 1 });
  await primera;
  assert.equal(e.estado.items[0].id, 20);
  assert.equal(e.estado.catalogo[0].id, 20);
  assert.equal(e.estado.pagina, 1);
  assert.equal(e.estado.filtros.equipos.marca, "");
  assert.equal(e.estado.filtros.equipos.area, "Sistemas");
  assert.match(e.nodo("importar-resultado").textContent, /1 registro importado/);
});

test("validación mantiene datos y muestra fila/campo como texto", async () => {
  const e = entorno();
  seleccionar(e);
  e.estado.items = [{ id: 1 }];
  e.api.importInventory = async () => { throw new e.ApiError("INVALID_REQUEST", "Fila 3 / Quantity: <img src=x>", 400); };
  await e.importarExcel(enviar);
  assert.equal(e.estado.items[0].id, 1);
  assert.equal(e.nodo("form-importar-error").textContent, "Fila 3 / Quantity: <img src=x>");
  assert.equal(e.nodo("importar-enviar").disabled, false);
});

test("fallo de red mantiene inventario y permite reintentar", async () => {
  const e = entorno();
  seleccionar(e);
  e.estado.items = [{ id: 1 }];
  await e.importarExcel(enviar);
  assert.equal(e.estado.items[0].id, 1);
  assert.equal(e.nodo("importar-archivo").disabled, false);
  assert.equal(e.nodo("form-importar-error").hidden, false);
});

test("fallo de recarga conserva éxito y reintenta sin importar otra vez", async () => {
  const e = entorno();
  seleccionar(e);
  let envios = 0;
  e.api.importInventory = async () => { envios++; return { imported: 0 }; };
  e.api.getInventory = async () => { throw new Error("recarga fallida"); };
  await e.importarExcel(enviar);
  assert.match(e.nodo("form-importar-error").textContent, /importación fue exitosa/);
  assert.match(e.nodo("importar-enviar-texto").textContent, /Reintentar/);
  e.api.getInventory = async () => ({ items: [], pagination: { totalPages: 0 } });
  await e.importarExcel(enviar);
  assert.equal(envios, 1);
  assert.equal(e.estado.items.length, 0);
});

test("respuestas viejas de lista y catálogo no revierten la importación", async () => {
  const e = entorno();
  seleccionar(e);
  const vieja = diferida();
  e.api.getInventory = () => vieja.promise;
  const lista = e.cargarItems();
  const catalogo = e.cargarCatalogo();
  e.api.importInventory = async () => ({ imported: 1 });
  e.api.getInventory = async () => ({ items: [{ id: 20 }], pagination: { totalPages: 1 } });
  await e.importarExcel(enviar);
  vieja.resolve({ items: [{ id: 1 }], pagination: { totalPages: 1 } });
  await Promise.all([lista, catalogo]);
  assert.equal(e.estado.items[0].id, 20);
  assert.equal(e.estado.catalogo[0].id, 20);
});
