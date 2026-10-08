package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
	"inventario/internal/textnorm"
)

// Migración única de los datos del Excel a SQL Server (cmd/migrate).

// MigrationData es todo lo que se copia del Excel.
type MigrationData struct {
	Items          []models.Item
	Movements      []models.Movement
	LastItemID     int // mayor ID de ítem asignado alguna vez (incluye eliminados)
	LastMovementID int
}

// ReadMigrationData lee el libro completo, incluidos los últimos IDs asignados,
// para que SQL Server nunca reutilice el ID de un ítem eliminado.
func (r *ExcelRepository) ReadMigrationData(ctx context.Context) (MigrationData, error) {
	var data MigrationData
	var err error
	if data.Items, err = r.GetAll(ctx); err != nil {
		return data, err
	}
	if data.Movements, err = r.GetMovements(ctx); err != nil {
		return data, err
	}
	err = r.read(ctx, func(f *excelize.File) error {
		var err error
		if data.LastItemID, err = inventoryIDs.lastIssued(f); err != nil {
			return err
		}
		data.LastMovementID, err = movementIDs.lastIssued(f)
		return err
	})
	for _, it := range data.Items {
		data.LastItemID = max(data.LastItemID, it.ID)
	}
	for _, m := range data.Movements {
		data.LastMovementID = max(data.LastMovementID, m.ID)
	}
	return data, err
}

// Largos máximos de las columnas (001_inicial.sql).
var sqlTextLimits = []struct {
	field string
	limit int
	value func(models.Item) string
}{
	{"N° de inventario", 100, func(i models.Item) string { return i.InventoryNumber }},
	{"Tipo", 100, func(i models.Item) string { return i.DeviceType }},
	{"Marca", 100, func(i models.Item) string { return i.Brand }},
	{"Modelo", 100, func(i models.Item) string { return i.Model }},
	{"N° de serie", 100, func(i models.Item) string { return i.SerialNumber }},
	{"Ubicación", 150, func(i models.Item) string { return i.Location }},
	{"Observación", 1000, func(i models.Item) string { return i.Notes }},
	{"Asignado a", 150, func(i models.Item) string { return i.AssignedTo }},
}

// CheckMigrationData informa TODOS los datos que la base rechazaría, para
// corregirlos en el Excel antes de migrar. La base compara sin mayúsculas ni
// tildes: "INV-1" e "inv-1" son el mismo número de inventario.
func CheckMigrationData(data MigrationData) []string {
	var problems []string
	add := func(id int, format string, args ...any) {
		problems = append(problems, fmt.Sprintf("ítem %d: ", id)+fmt.Sprintf(format, args...))
	}
	// numbers guarda el PRIMER ítem con cada N° de inventario: todos los demás
	// con ese número deben pertenecer al mismo equipo (gabinete y componentes).
	numbers, serials, ids := map[string]models.Item{}, map[string]int{}, map[int]bool{}
	for _, it := range data.Items {
		ids[it.ID] = true
	}
	for _, it := range data.Items {
		for _, l := range sqlTextLimits {
			if n := utf8.RuneCountInString(l.value(it)); n > l.limit {
				add(it.ID, "%s tiene %d caracteres (máximo %d)", l.field, n, l.limit)
			}
		}
		if key := textnorm.Fold(it.InventoryNumber); key != "" {
			if first, dup := numbers[key]; !dup {
				numbers[key] = it
			} else if !models.ShareEquipment(it, first) {
				add(it.ID, "N° de inventario %q repetido con el ítem %d, que no es del mismo equipo", it.InventoryNumber, first.ID)
			}
		}
		if key := textnorm.Fold(it.SerialNumber); key != "" {
			if other, dup := serials[key]; dup {
				add(it.ID, "N° de serie %q repetido con el ítem %d (sin distinguir mayúsculas ni tildes)", it.SerialNumber, other)
			}
			serials[key] = it.ID
		}
		if it.EquipmentID != 0 && !ids[it.EquipmentID] {
			add(it.ID, "está vinculado al equipo %d, que no existe", it.EquipmentID)
		}
	}
	return problems
}

// ErrDatabaseNotEmpty: la migración solo carga una base vacía.
var ErrDatabaseNotEmpty = errors.New("la base ya tiene datos")

// LoadMigrationData copia los datos en UNA transacción, conservando los IDs.
// Si algo falla no queda nada cargado. Después ajusta los contadores IDENTITY
// al último ID asignado en el Excel.
func (r *SQLServerRepository) LoadMigrationData(ctx context.Context, data MigrationData) error {
	return r.inTx(ctx, func(tx *sql.Tx) error {
		var existing int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM dbo.Items WITH (TABLOCKX)) +
			(SELECT COUNT(*) FROM dbo.Movements WITH (TABLOCKX))`).Scan(&existing); err != nil {
			return err
		}
		if existing > 0 {
			return fmt.Errorf("%w: %d filas en Items y Movements; la migración solo carga una base vacía", ErrDatabaseNotEmpty, existing)
		}

		if _, err := tx.ExecContext(ctx, `SET IDENTITY_INSERT dbo.Items ON`); err != nil {
			return err
		}
		for _, item := range data.Items {
			sinVinculo := truncarTiempos(item)
			sinVinculo.EquipmentID = 0
			if _, err := tx.ExecContext(ctx, `INSERT INTO dbo.Items (ID, InventoryNumber, HasInventory, DeviceType, Brand, Model,
				SerialNumber, Quantity, Location, Status, Notes, CreatedAt, UpdatedAt, Availability, AssignedTo, LoanedAt, EquipmentID)
				VALUES (@p17, @p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9, @p10, @p11, @p12, @p13, @p14, @p15, @p16)`,
				append(itemValues(normalizeImported(sinVinculo)), item.ID)...); err != nil {
				return fmt.Errorf("ítem %d: %w", item.ID, mapSQLError(err))
			}
		}
		if _, err := tx.ExecContext(ctx, `SET IDENTITY_INSERT dbo.Items OFF`); err != nil {
			return err
		}
		for _, item := range data.Items {
			if item.EquipmentID == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE dbo.Items SET EquipmentID = @p1 WHERE ID = @p2`, item.EquipmentID, item.ID); err != nil {
				return fmt.Errorf("vínculo del ítem %d: %w", item.ID, mapSQLError(err))
			}
		}

		if _, err := tx.ExecContext(ctx, `SET IDENTITY_INSERT dbo.Movements ON`); err != nil {
			return err
		}
		for _, m := range data.Movements {
			if _, err := tx.ExecContext(ctx, `INSERT INTO dbo.Movements (ID, ItemID, MovementType, Quantity, PreviousQuantity,
				NewQuantity, OriginLocation, DestinationLocation, Notes, CreatedAt)
				VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9, @p10)`,
				m.ID, m.ItemID, string(m.MovementType), m.Quantity, m.PreviousQuantity, m.NewQuantity,
				m.OriginLocation, m.DestinationLocation, m.Notes, m.CreatedAt.Truncate(sqlTimeUnit)); err != nil {
				return fmt.Errorf("movimiento %d: %w", m.ID, mapSQLError(err))
			}
		}
		if _, err := tx.ExecContext(ctx, `SET IDENTITY_INSERT dbo.Movements OFF`); err != nil {
			return err
		}

		// El próximo ID será el último asignado en el Excel + 1, aunque ese ítem
		// se haya eliminado (nunca se reutilizan IDs).
		for _, seq := range []struct {
			table string
			last  int
		}{{"dbo.Items", data.LastItemID}, {"dbo.Movements", data.LastMovementID}} {
			if seq.last <= 0 {
				continue
			}
			// En una tabla que nunca tuvo filas, SQL Server usa el valor de
			// RESEED como próximo ID (no el siguiente): ahí se resiembra con last+1.
			var nunca bool
			if err := tx.QueryRowContext(ctx, `SELECT CASE WHEN last_value IS NULL THEN 1 ELSE 0 END
				FROM sys.identity_columns WHERE object_id = OBJECT_ID(@p1)`, seq.table).Scan(&nunca); err != nil {
				return err
			}
			reseed := seq.last
			if nunca {
				reseed++
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DBCC CHECKIDENT ('%s', RESEED, %d) WITH NO_INFOMSGS`, seq.table, reseed)); err != nil {
				return err
			}
		}
		return nil
	})
}

// truncarTiempos lleva las fechas a la precisión de DATETIMEOFFSET(7).
func truncarTiempos(item models.Item) models.Item {
	item.CreatedAt = item.CreatedAt.Truncate(sqlTimeUnit)
	item.UpdatedAt = item.UpdatedAt.Truncate(sqlTimeUnit)
	if item.LoanedAt != nil {
		t := item.LoanedAt.Truncate(sqlTimeUnit)
		item.LoanedAt = &t
	}
	return item
}
