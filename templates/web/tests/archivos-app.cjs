// Archivos en que se dividió app.js, en el orden en que los carga index.html.
// Comparten el ámbito global (scripts clásicos), así que el orden importa.
module.exports = ['base', 'componentes', 'secciones', 'carga', 'menu', 'formulario', 'operaciones', 'historial', 'inicio']
  .map((nombre) => `app/${nombre}`);
