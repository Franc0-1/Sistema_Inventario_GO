const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function entorno() {
  const sandbox = { console, FormData, URLSearchParams, clearTimeout, setTimeout, queueMicrotask,
    document: { addEventListener() {} }, history: { replaceState() {} }, location: { pathname: '/' } };
  vm.createContext(sandbox);
  for (const nombre of ['api', ...require('./archivos-app.cjs'), 'analytics']) vm.runInContext(fs.readFileSync(path.join(__dirname, `../static/js/${nombre}.js`), 'utf8'), sandbox);
  vm.runInContext(`
    renderAnalitica = () => {};
    renderSeccion = () => {};
    renderResultados = () => {};
    actualizarItem = () => {};
    globalThis.prueba = { api, estado, ApiError, cargarAnalitica, abrirConsultaResumen, parametrosConsulta, cambiarSeccion, aplicarCambio };
  `, sandbox);
  return { sandbox, ...sandbox.prueba };
}

test('los enlaces del resumen consultan todas las categorías sin restringir la condición disponible', async () => {
  const e = entorno();
  e.estado.seccion = 'resumen';
  e.api.getInventory = async () => ({ items: [], pagination: { totalPages: 0 } });
  e.abrirConsultaResumen({ availability: 'DISPONIBLE', hasInventory: true });
  const filtros = e.parametrosConsulta();
  assert.equal(filtros.excludeDeviceType, '');
  assert.equal(filtros.hasInventory, 'true');
  assert.equal(filtros.availability, 'DISPONIBLE');
  assert.equal(filtros.status, '');
  assert.equal(filtros.includeRetired, false);
  e.estado.seccion = 'resumen';
  e.abrirConsultaResumen({ location: 'Sistemas', includeRetired: true });
  assert.equal(e.parametrosConsulta().location, 'Sistemas');
  assert.equal(e.parametrosConsulta().includeRetired, true);
});

test('navegar por analítica conserva filtros, orden y página del inventario', async () => {
  const e = entorno();
  e.estado.pagina = 4;
  e.estado.consulta = 'Lenovo';
  e.estado.filtros.equipos.marca = 'Lenovo';
  e.api.getSummary = async () => ({ registros: 20 });
  e.api.getInventory = async () => ({ items: [{ id: 20 }], pagination: { totalPages: 4 } });
  e.cambiarSeccion('resumen');
  await e.cargarAnalitica();
  e.cambiarSeccion('equipos');
  assert.equal(e.estado.pagina, 4);
  assert.equal(e.estado.consulta, 'Lenovo');
  assert.equal(e.estado.filtros.equipos.marca, 'Lenovo');
});

test('el reporte aplica filtros y descarta respuestas viejas', async () => {
  const e = entorno();
  e.estado.seccion = 'reportes';
  let completar;
  e.api.getReports = () => new Promise((resolve) => { completar = resolve; });
  const antigua = e.cargarAnalitica();
  e.estado.reporteFiltros = { location: 'Sistemas', deviceType: 'Mouse', includeRetired: true };
  e.api.getReports = async (filtros) => {
    assert.equal(filtros.location, 'Sistemas');
    assert.equal(filtros.deviceType, 'Mouse');
    assert.equal(filtros.includeRetired, true);
    return { registros: 2 };
  };
  await e.cargarAnalitica();
  completar({ registros: 999 });
  await antigua;
  assert.equal(e.estado.analitica.datos.registros, 2);
});

test('error permite reintento y una mutación recarga el resumen', async () => {
  const e = entorno();
  e.estado.seccion = 'resumen';
  e.api.getSummary = async () => { throw new e.ApiError('NETWORK', 'red', 0); };
  await e.cargarAnalitica();
  assert.match(e.estado.analitica.error, /conectar/);
  e.api.getSummary = async () => ({ registros: 2 });
  await e.cargarAnalitica();
  assert.equal(e.estado.analitica.error, '');
  e.api.getSummary = async () => ({ registros: 3 });
  e.aplicarCambio({ id: 3 });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(e.estado.analitica.datos.registros, 3);
});
