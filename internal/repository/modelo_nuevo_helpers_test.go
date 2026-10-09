package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
)

// Tablas del modelo nuevo en orden de borrado (primero las que referencian).
const tablasModeloNuevo = `dbo.HistorialComponentes, dbo.HistorialEquipos, dbo.MovimientosInsumo, dbo.Prestamos,
	dbo.Insumos, dbo.Componentes, dbo.Equipos, dbo.Tipos, dbo.Personas, dbo.Puestos, dbo.Oficinas, dbo.Usuarios`

// abrirBaseTest abre la base de INVENTARIO_TEST_DSN y se niega a seguir si no
// termina en _Test.
func abrirBaseTest(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlserver", testDSN(t))
	must(t, err)
	t.Cleanup(func() { db.Close() })
	var nombre string
	must(t, db.QueryRowContext(context.Background(), `SELECT DB_NAME()`).Scan(&nombre))
	if !strings.HasSuffix(strings.ToLower(nombre), "_test") {
		t.Fatalf("la base %q no termina en _Test", nombre)
	}
	return db
}

// borrarModeloNuevo quita las tablas que haya dejado un test interrumpido.
func borrarModeloNuevo(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `DROP TABLE IF EXISTS `+tablasModeloNuevo)
	must(t, err)
}

func scriptBorrador(t *testing.T, nombre string) string {
	t.Helper()
	b, err := os.ReadFile("migrations/borrador/" + nombre)
	must(t, err)
	return string(b)
}

// testModeloNuevo crea el modelo nuevo en la base de prueba (vacío, salvo el
// usuario Sistema) y lo borra al terminar el test.
func testModeloNuevo(t *testing.T) *SQLServerRepository {
	t.Helper()
	db := abrirBaseTest(t)
	borrarModeloNuevo(t, db)
	t.Cleanup(func() { db.ExecContext(context.Background(), `DROP TABLE IF EXISTS `+tablasModeloNuevo) })
	_, err := db.ExecContext(context.Background(), scriptBorrador(t, "003_modelo_nuevo.sql"))
	must(t, err)
	return &SQLServerRepository{db: db}
}
