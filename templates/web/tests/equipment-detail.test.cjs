const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function entorno() {
  const dialog = { close() {} };
  const sandbox = {
    document: { addEventListener() {}, getElementById: () => dialog, body: { classList: { remove() {} } }, querySelectorAll: () => [] },
    window: {}, api: {}, estado: { items: [], cargando: false, errorCarga: '' }, mensajeDeError: e => e.message,
  };
  vm.createContext(sandbox);
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../static/js/equipment-detail.js'), 'utf8'), sandbox);
  vm.runInContext('renderFichaEquipo = () => {}; mostrarFichaEquipo = () => {}; globalThis.prueba = { fichaEquipo, abrirFichaEquipo, cerrarFichaEquipo, sincronizarFichaEquipo };', sandbox);
  return { ...sandbox.prueba, api: sandbox.api, estado: sandbox.estado };
}

test('la ficha descarta respuestas de selecciones anteriores', async () => {
  const e = entorno();
  let resolver;
  e.api.getInventoryById = () => new Promise(r => { resolver = r; });
  const anterior = e.abrirFichaEquipo(1);
  e.api.getInventoryById = async id => ({ id, modelo: 'Actual' });
  await e.abrirFichaEquipo(2);
  resolver({ id: 1 });
  await anterior;
  assert.equal(e.fichaEquipo.id, 2);
  assert.equal(e.fichaEquipo.datos.id, 2);
});

test('cerrar invalida la solicitud pendiente y no reabre la ficha', async () => {
  const e = entorno();
  let resolver;
  e.api.getInventoryById = () => new Promise(r => { resolver = r; });
  const pendiente = e.abrirFichaEquipo(1);
  e.cerrarFichaEquipo(false);
  resolver({ id: 1 });
  await pendiente;
  assert.equal(e.fichaEquipo.id, null);
  assert.equal(e.fichaEquipo.datos, null);
  assert.equal(e.fichaEquipo.cargando, false);
});

test('la misma seleccion pendiente no duplica solicitudes', async () => {
  const e = entorno();
  let resolver;
  let solicitudes = 0;
  e.api.getInventoryById = () => { solicitudes++; return new Promise(r => { resolver = r; }); };
  const pendiente = e.abrirFichaEquipo(1);
  await e.abrirFichaEquipo(1);
  assert.equal(solicitudes, 1);
  resolver({ id: 1 });
  await pendiente;
});

test('el listado anterior no sobrescribe una ficha mas reciente', async () => {
  const e = entorno();
  e.api.getInventoryById = async id => ({ id, cantidad: 9, updated_at: '2026-10-07T12:00:00Z' });
  await e.abrirFichaEquipo(1);
  e.estado.items = [{ id: 1, cantidad: 3, updated_at: '2026-10-07T11:00:00Z' }];
  e.sincronizarFichaEquipo();
  assert.equal(e.fichaEquipo.datos.cantidad, 9);
});

test('la ficha permite reintentar un error sin cambiar la seleccion', async () => {
  const e = entorno();
  e.api.getInventoryById = async () => { throw new Error('Sin conexion'); };
  await e.abrirFichaEquipo(1);
  assert.equal(e.fichaEquipo.error, 'Sin conexion');
  e.api.getInventoryById = async id => ({ id });
  await e.abrirFichaEquipo(1);
  assert.equal(e.fichaEquipo.error, '');
  assert.equal(e.fichaEquipo.datos.id, 1);
});

test('recargar mantiene la ficha hasta recibir resultados y actualiza o cierra la seleccion', async () => {
  const e = entorno();
  e.api.getInventoryById = async id => ({ id, cantidad: 9 });
  await e.abrirFichaEquipo(1);
  e.estado.cargando = true;
  e.estado.items = [{ id: 1, cantidad: 3 }];
  e.sincronizarFichaEquipo();
  assert.equal(e.fichaEquipo.datos.cantidad, 9);
  e.estado.cargando = false;
  e.sincronizarFichaEquipo();
  assert.equal(e.fichaEquipo.datos.cantidad, 3);
  e.estado.items = [];
  e.sincronizarFichaEquipo();
  assert.equal(e.fichaEquipo.id, null);
});
