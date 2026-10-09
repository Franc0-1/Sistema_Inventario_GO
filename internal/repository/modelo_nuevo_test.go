package repository

import (
	"context"
	"testing"
)

// TestSQLServer_ModeloNuevo aplica el borrador del modelo nuevo dentro de una
// transacción que siempre se deshace: comprueba que el script corre y que la
// base hace cumplir cada regla del DER, sin dejar nada en Inventario_Test.
func TestSQLServer_ModeloNuevo(t *testing.T) {
	ctx := context.Background()
	db := abrirBaseTest(t)
	borrarModeloNuevo(t, db)
	tx, err := db.BeginTx(ctx, nil)
	must(t, err)
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, scriptBorrador(t, "003_modelo_nuevo.sql")); err != nil {
		t.Fatalf("el script no corre: %v", err)
	}

	exec := func(stmt string) error { _, err := tx.ExecContext(ctx, stmt); return err }
	// Datos válidos. Las tablas se crean en esta transacción: los IDs empiezan en 1.
	for _, stmt := range []string{
		`INSERT INTO dbo.Oficinas (Nombre) VALUES (N'Sistemas'), (N'Contabilidad')`,
		`INSERT INTO dbo.Puestos (Nombre) VALUES (N'Contador')`,
		`INSERT INTO dbo.Personas (Nombre, Apellido, PuestoID, OficinaID) VALUES (N'María', N'Gómez', 1, 2)`,
		`INSERT INTO dbo.Tipos (Nombre, Clase, Prestable) VALUES
			(N'PC', 'EQUIPO', 0), (N'Notebook', 'EQUIPO', 1), (N'RAM', 'COMPONENTE', 0), (N'Mouse', 'INSUMO', 0)`,
		`INSERT INTO dbo.Equipos (NumeroInventario, TipoID, NumeroSerie, OficinaID) VALUES (N'1001', 1, N'SN-1', 1)`,
		// Notebook de una persona de Contabilidad, ubicada en Sistemas.
		`INSERT INTO dbo.Equipos (NumeroInventario, TipoID, OficinaID, PersonaID) VALUES (N'1002', 2, 1, 1)`,
		`INSERT INTO dbo.Equipos (NumeroInventario, TipoID, OficinaID, Estado, FechaBaja, MotivoBaja)
			VALUES (N'1003', 1, 1, 'BAJA', SYSDATETIMEOFFSET(), N'Placa quemada')`,
		// Dos equipos pendientes de numerar: el N° vacío no cuenta como repetido.
		`INSERT INTO dbo.Equipos (TipoID, OficinaID) VALUES (1, 1), (1, 2)`,
		// Dos componentes con el N° de inventario de la PC; uno fuera de un equipo.
		`INSERT INTO dbo.Componentes (NumeroInventario, TipoID, NumeroSerie, EquipoID) VALUES (N'1001', 3, N'RAM-1', 1)`,
		`INSERT INTO dbo.Componentes (NumeroInventario, TipoID, OficinaID) VALUES (N'1001', 3, 1)`,
		`INSERT INTO dbo.Insumos (TipoID, Marca, Modelo, Stock) VALUES (4, N'Logitech', N'M90', 10)`,
		`INSERT INTO dbo.Prestamos (EquipoID, PersonaID, FechaSalida, DevolucionPrevista, UsuarioID)
			VALUES (2, 1, SYSDATETIMEOFFSET(), DATEADD(HOUR, 4, SYSDATETIMEOFFSET()), 1)`,
		`INSERT INTO dbo.MovimientosInsumo (InsumoID, Tipo, Cantidad, OficinaID, UsuarioID) VALUES (1, 'SALIDA', 2, 2, 1)`,
		`INSERT INTO dbo.MovimientosInsumo (InsumoID, Tipo, Cantidad, UsuarioID) VALUES (1, 'AJUSTE', -1, 1)`,
		`INSERT INTO dbo.HistorialEquipos (EquipoID, OficinaNuevaID, Estado, UsuarioID) VALUES (1, 1, 'OPERATIVO', 1)`,
		`INSERT INTO dbo.HistorialComponentes (ComponenteID, EquipoAnteriorID, UsuarioID) VALUES (2, 1, 1)`,
	} {
		if err := exec(stmt); err != nil {
			t.Fatalf("dato válido rechazado: %v\n%s", err, stmt)
		}
	}

	rechazos := []struct{ regla, stmt string }{
		{"equipo con tipo de componente", `INSERT INTO dbo.Equipos (NumeroInventario, TipoID, OficinaID) VALUES (N'2001', 3, 1)`},
		{"insumo con tipo de equipo", `INSERT INTO dbo.Insumos (TipoID, Modelo) VALUES (1, N'X')`},
		{"cambiar la clase de un tipo en uso", `UPDATE dbo.Tipos SET Clase = 'INSUMO', Prestable = 0 WHERE ID = 1`},
		{"insumo prestable", `INSERT INTO dbo.Tipos (Nombre, Clase, Prestable) VALUES (N'Cable', 'INSUMO', 1)`},
		{"N° de inventario de equipo repetido", `INSERT INTO dbo.Equipos (NumeroInventario, TipoID, OficinaID) VALUES (N'1001', 1, 1)`},
		{"serie de equipo repetida (sin mayúsculas)", `INSERT INTO dbo.Equipos (NumeroInventario, TipoID, NumeroSerie, OficinaID) VALUES (N'2002', 1, N'sn-1', 1)`},
		{"serie de componente repetida", `INSERT INTO dbo.Componentes (TipoID, NumeroSerie, OficinaID) VALUES (3, N'RAM-1', 1)`},
		{"componente en equipo y en oficina", `INSERT INTO dbo.Componentes (TipoID, EquipoID, OficinaID) VALUES (3, 1, 1)`},
		{"componente sin ubicación", `INSERT INTO dbo.Componentes (TipoID) VALUES (3)`},
		{"baja sin motivo", `UPDATE dbo.Equipos SET Estado = 'BAJA', FechaBaja = SYSDATETIMEOFFSET() WHERE ID = 1`},
		{"fecha de baja sin estado BAJA", `UPDATE dbo.Equipos SET FechaBaja = SYSDATETIMEOFFSET(), MotivoBaja = N'x' WHERE ID = 1`},
		{"estado inválido", `UPDATE dbo.Componentes SET Estado = 'ROTO' WHERE ID = 1`},
		{"stock negativo", `UPDATE dbo.Insumos SET Stock = -1 WHERE ID = 1`},
		{"insumo repetido (sin mayúsculas ni tildes)", `INSERT INTO dbo.Insumos (TipoID, Marca, Modelo) VALUES (4, N'logitech', N'm90')`},
		{"segundo préstamo abierto", `INSERT INTO dbo.Prestamos (EquipoID, PersonaID, FechaSalida, DevolucionPrevista, UsuarioID)
			VALUES (2, 1, SYSDATETIMEOFFSET(), SYSDATETIMEOFFSET(), 1)`},
		{"devolución antes de la salida", `UPDATE dbo.Prestamos SET FechaDevolucion = DATEADD(DAY, -1, FechaSalida) WHERE ID = 1`},
		{"salida sin destino", `INSERT INTO dbo.MovimientosInsumo (InsumoID, Tipo, Cantidad, UsuarioID) VALUES (1, 'SALIDA', 1, 1)`},
		{"entrada de 0 unidades", `INSERT INTO dbo.MovimientosInsumo (InsumoID, Tipo, Cantidad, UsuarioID) VALUES (1, 'ENTRADA', 0, 1)`},
		{"ajuste sin diferencia", `INSERT INTO dbo.MovimientosInsumo (InsumoID, Tipo, Cantidad, UsuarioID) VALUES (1, 'AJUSTE', 0, 1)`},
		{"movimiento sin usuario", `INSERT INTO dbo.MovimientosInsumo (InsumoID, Tipo, Cantidad, OficinaID) VALUES (1, 'ENTRADA', 1, 1)`},
		{"borrar una oficina en uso", `DELETE FROM dbo.Oficinas WHERE ID = 1`},
		{"borrar un equipo con componentes", `DELETE FROM dbo.Equipos WHERE ID = 1`},
	}
	for _, r := range rechazos {
		if err := exec(r.stmt); err == nil {
			t.Errorf("la base aceptó: %s", r.regla)
		}
	}

	// Después de los rechazos la transacción sigue viva y los datos válidos intactos.
	var equipos int
	must(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM dbo.Equipos`).Scan(&equipos))
	if equipos != 5 {
		t.Errorf("equipos = %d, se esperaban 5", equipos)
	}
}
