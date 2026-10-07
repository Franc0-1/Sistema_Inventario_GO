const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function entorno() {
  const sandbox = { document: { addEventListener() {} }, api: {}, mensajeDeError: (error) => error.message };
  vm.createContext(sandbox);
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../static/js/workspace.js'), 'utf8'), sandbox);
  vm.runInContext('renderPanelInventario = () => {}; globalThis.prueba = { panelInventario, cargarPanelInventario };', sandbox);
  return { ...sandbox.prueba, api: sandbox.api };
}

test('el panel descarta respuestas anteriores después de una actualización', async () => {
  const e = entorno();
  let terminar;
  e.api.getSummary = () => new Promise((resolve) => { terminar = resolve; });
  const antigua = e.cargarPanelInventario();
  e.api.getSummary = async () => ({ registros_activos: 8 });
  await e.cargarPanelInventario();
  terminar({ registros_activos: 3 });
  await antigua;
  assert.equal(e.panelInventario.datos.registros_activos, 8);
  assert.equal(e.panelInventario.cargando, false);
});

test('fallo de actualización no conserva indicadores obsoletos y permite reintentar', async () => {
  const e = entorno();
  e.api.getSummary = async () => ({ registros_activos: 8 });
  await e.cargarPanelInventario();
  e.api.getSummary = async () => { throw new Error('Sin conexión'); };
  await e.cargarPanelInventario();
  assert.equal(e.panelInventario.datos, null);
  assert.equal(e.panelInventario.error, 'Sin conexión');
  e.api.getSummary = async () => ({ registros_activos: 9 });
  await e.cargarPanelInventario();
  assert.equal(e.panelInventario.datos.registros_activos, 9);
  assert.equal(e.panelInventario.error, '');
});
