package main

import (
	"fmt"
	"strings"
	"time"

	"inventario/internal/models"
	"inventario/internal/repository"
)

// scriptSQL genera un script T-SQL equivalente a LoadMigrationData, para
// ejecutar en SSMS: una sola transacción, solo sobre una base vacía, que
// conserva los IDs y ajusta los contadores para no reutilizarlos.
func scriptSQL(data repository.MigrationData) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	w("-- Migración del inventario desde Excel (%d ítems, %d movimientos).", len(data.Items), len(data.Movements))
	w("-- Ejecutar sobre la base de destino YA CREADA por la app (tablas vacías).")
	w("SET XACT_ABORT ON;  -- ante cualquier error se deshace todo")
	w("BEGIN TRANSACTION;")
	w("IF EXISTS (SELECT 1 FROM dbo.Items) OR EXISTS (SELECT 1 FROM dbo.Movements)")
	w("    THROW 50001, N'La base ya tiene datos: la migración solo carga una base vacía.', 1;")
	w("")
	w("SET IDENTITY_INSERT dbo.Items ON;")
	for _, it := range data.Items {
		w("INSERT INTO dbo.Items (ID, InventoryNumber, HasInventory, DeviceType, Brand, Model, SerialNumber, Quantity, Location, Status, Notes, CreatedAt, UpdatedAt, Availability, AssignedTo, LoanedAt) VALUES (%d, %s, %d, %s, %s, %s, %s, %d, %s, %s, %s, %s, %s, %s, %s, %s);",
			it.ID, str(it.InventoryNumber), bit(it.HasInventory), str(it.DeviceType), str(it.Brand), str(it.Model),
			str(it.SerialNumber), it.Quantity, str(it.Location), str(string(it.Status)), str(it.Notes),
			fecha(it.CreatedAt), fecha(it.UpdatedAt), str(string(disponibilidad(it))), str(it.AssignedTo), fechaOpcional(it.LoanedAt))
	}
	w("SET IDENTITY_INSERT dbo.Items OFF;")
	for _, it := range data.Items {
		if it.EquipmentID != 0 {
			w("UPDATE dbo.Items SET EquipmentID = %d WHERE ID = %d;", it.EquipmentID, it.ID)
		}
	}
	w("")
	w("SET IDENTITY_INSERT dbo.Movements ON;")
	for _, m := range data.Movements {
		w("INSERT INTO dbo.Movements (ID, ItemID, MovementType, Quantity, PreviousQuantity, NewQuantity, OriginLocation, DestinationLocation, Notes, CreatedAt) VALUES (%d, %d, %s, %d, %d, %d, %s, %s, %s, %s);",
			m.ID, m.ItemID, str(string(m.MovementType)), m.Quantity, m.PreviousQuantity, m.NewQuantity,
			str(m.OriginLocation), str(m.DestinationLocation), str(m.Notes), fecha(m.CreatedAt))
	}
	w("SET IDENTITY_INSERT dbo.Movements OFF;")
	w("")
	w("-- Próximos IDs: después del último asignado en el Excel (nunca se reutilizan).")
	for _, seq := range []struct {
		table string
		last  int
	}{{"dbo.Items", data.LastItemID}, {"dbo.Movements", data.LastMovementID}} {
		if seq.last > 0 {
			w("IF (SELECT last_value FROM sys.identity_columns WHERE object_id = OBJECT_ID(N'%s')) IS NULL", seq.table)
			w("    DBCC CHECKIDENT ('%s', RESEED, %d) WITH NO_INFOMSGS;", seq.table, seq.last+1)
			w("ELSE")
			w("    DBCC CHECKIDENT ('%s', RESEED, %d) WITH NO_INFOMSGS;", seq.table, seq.last)
		}
	}
	w("")
	w("COMMIT TRANSACTION;")
	w("SELECT (SELECT COUNT(*) FROM dbo.Items) AS Items, (SELECT COUNT(*) FROM dbo.Movements) AS Movimientos;")
	return b.String()
}

func str(s string) string { return "N'" + strings.ReplaceAll(strings.TrimSpace(s), "'", "''") + "'" }

func bit(b bool) int {
	if b {
		return 1
	}
	return 0
}

// fecha usa el formato ISO 8601 con zona horaria, que SQL Server convierte a
// DATETIMEOFFSET sin depender de la configuración regional.
func fecha(t time.Time) string {
	return "'" + t.Truncate(100*time.Nanosecond).Format("2006-01-02T15:04:05.0000000-07:00") + "'"
}

func fechaOpcional(t *time.Time) string {
	if t == nil {
		return "NULL"
	}
	return fecha(*t)
}

func disponibilidad(it models.Item) models.Availability {
	if it.Availability == "" {
		return models.Available
	}
	return it.Availability
}
