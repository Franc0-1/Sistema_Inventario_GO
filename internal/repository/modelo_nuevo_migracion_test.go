package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
)

// borradorTx abre una transacción en la base de prueba (que siempre se deshace)
// con el modelo nuevo creado y Items cargado con datos de ejemplo.
func borradorTx(t *testing.T, items string) *sql.Tx {
	t.Helper()
	ctx := context.Background()
	db := abrirBaseTest(t)
	borrarModeloNuevo(t, db)
	tx, err := db.BeginTx(ctx, nil)
	must(t, err)
	t.Cleanup(func() { tx.Rollback() })
	_, err = tx.ExecContext(ctx, scriptBorrador(t, "003_modelo_nuevo.sql"))
	must(t, err)
	_, err = tx.ExecContext(ctx, `DELETE FROM dbo.Movements; UPDATE dbo.Items SET EquipmentID = NULL; DELETE FROM dbo.Items;
		SET IDENTITY_INSERT dbo.Items ON;
		INSERT INTO dbo.Items (ID, InventoryNumber, HasInventory, DeviceType, Brand, Model, SerialNumber, Quantity,
			Location, Status, Notes, CreatedAt, UpdatedAt, EquipmentID) VALUES `+items+`;
		SET IDENTITY_INSERT dbo.Items OFF;`)
	must(t, err)
	return tx
}

func migrarDatos(tx *sql.Tx) error {
	script, err := os.ReadFile("migrations/borrador/004_migrar_datos.sql")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(context.Background(), string(script))
	return err
}
func TestSQLServer_MigracionModeloNuevo(t *testing.T) {
	const f = `'2026-09-01T10:00:00-03:00'`
	tx := borradorTx(t, `
		(1, N'1001', 1, N'CPU', N'Dell', N'OptiPlex', N'DX-1', 1, N'Piso 6 / Sistemas', 'OPERATIVO', N'', `+f+`, `+f+`, NULL),
		(2, N'1001', 1, N'Procesador', N'Intel', N'i5', N'CPU-1', 1, N'Piso 6 / Sistemas', 'OPERATIVO', N'', `+f+`, `+f+`, 1),
		(3, N'1001', 1, N'RAM', N'Kingston', N'8GB', N'', 1, N'Piso 6 / Sistemas', 'OPERATIVO', N'', `+f+`, `+f+`, 1),
		(4, N'2001', 1, N'Gabinete', N'Genérico', N'ATX', N'', 1, N'Piso EP / Prensa', 'BAJA', N'', `+f+`, `+f+`, NULL),
		(5, N'', 0, N'Monitor', N'LG', N'24MK', N'', 1, N'Piso 6', 'OPERATIVO', N'', `+f+`, `+f+`, NULL),
		(6, N'', 0, N'Monitor', N'LG', N'24MK', N'', 1, N'Piso 6', 'OPERATIVO', N'', `+f+`, `+f+`, NULL),
		(7, N'3001', 1, N'Fuente de alimentacion', N'Gigabyte', N'500W', N'', 1, N'Piso 6', 'OPERATIVO', N'', `+f+`, `+f+`, NULL),
		(8, N'', 0, N'Pendrive', N'Kingston', N'32GB', N'', 3, N'Piso 6 / Sistemas', 'OPERATIVO', N'Azul', `+f+`, `+f+`, NULL),
		(9, N'', 0, N'Pendrive', N'kingston', N'32gb', N'', 2, N'Piso EP / Prensa', 'OPERATIVO', N'', `+f+`, `+f+`, NULL),
		(10, N'', 0, N'Headset', N'Genius', N'HS', N'', 0, N'Piso 6', 'OPERATIVO', N'', `+f+`, `+f+`, NULL)`)
	if err := migrarDatos(tx); err != nil {
		t.Fatalf("migración: %v", err)
	}

	ctx := context.Background()
	for _, c := range []struct {
		que   string
		query string
		want  int
	}{
		{"oficinas", `SELECT COUNT(*) FROM dbo.Oficinas`, 3},
		{"tipos (CPU y Gabinete son PC)", `SELECT COUNT(*) FROM dbo.Tipos`, 7},
		{"PCs", `SELECT COUNT(*) FROM dbo.Equipos e JOIN dbo.Tipos t ON t.ID = e.TipoID WHERE t.Nombre = N'PC'`, 2},
		{"equipos", `SELECT COUNT(*) FROM dbo.Equipos`, 4},
		{"pendientes de numerar", `SELECT COUNT(*) FROM dbo.Equipos WHERE NumeroInventario = N''`, 2},
		{"bajas con fecha y motivo", `SELECT COUNT(*) FROM dbo.Equipos WHERE Estado = 'BAJA' AND FechaBaja IS NOT NULL AND MotivoBaja <> N''`, 1},
		{"componentes en la PC 1001", `SELECT COUNT(*) FROM dbo.Componentes k JOIN dbo.Equipos e ON e.ID = k.EquipoID
			WHERE e.IDAnterior = 1 AND k.NumeroInventario = N'1001'`, 2},
		{"fuente suelta en su oficina", `SELECT COUNT(*) FROM dbo.Componentes k JOIN dbo.Oficinas o ON o.ID = k.OficinaID
			WHERE k.IDAnterior = 7 AND o.Nombre = N'Piso 6'`, 1},
		{"pendrives agrupados", `SELECT Stock FROM dbo.Insumos WHERE IDAnterior = 8`, 5},
		{"insumos", `SELECT COUNT(*) FROM dbo.Insumos`, 2},
		{"movimiento de stock inicial (sin el de stock 0)", `SELECT COUNT(*) FROM dbo.MovimientosInsumo`, 1},
		{"historial de equipos", `SELECT COUNT(*) FROM dbo.HistorialEquipos`, 4},
		{"historial de componentes", `SELECT COUNT(*) FROM dbo.HistorialComponentes`, 3},
	} {
		var got int
		must(t, tx.QueryRowContext(ctx, c.query).Scan(&got))
		if got != c.want {
			t.Errorf("%s = %d, se esperaba %d", c.que, got, c.want)
		}
	}

	if err := migrarDatos(tx); err == nil || !strings.Contains(err.Error(), "solo corre una vez") {
		t.Errorf("segunda migración: err = %v, se esperaba el rechazo", err)
	}
}

func TestSQLServer_MigracionModeloNuevo_TipoSinClasificar(t *testing.T) {
	tx := borradorTx(t, `(1, N'1', 1, N'Tostadora', N'X', N'Y', N'', 1, N'Cocina', 'OPERATIVO', N'',
		'2026-09-01T10:00:00-03:00', '2026-09-01T10:00:00-03:00', NULL)`)
	if err := migrarDatos(tx); err == nil || !strings.Contains(err.Error(), "Tipos sin clasificar: Tostadora") {
		t.Errorf("err = %v, se esperaba el aviso del tipo sin clasificar", err)
	}
}
