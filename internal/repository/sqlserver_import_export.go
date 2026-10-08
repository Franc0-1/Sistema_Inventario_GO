package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io"

	"inventario/internal/models"
)

var _ Repository = (*SQLServerRepository)(nil)

// ExportInventory genera el mismo Excel en español que el repositorio de Excel.
func (r *SQLServerRepository) ExportInventory(ctx context.Context) ([]byte, error) {
	items, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	return writeExportWorkbook(items)
}

// ImportInventory valida el archivo completo y reemplaza el inventario en una
// única transacción: si algo falla, la base queda exactamente como estaba. Los
// movimientos no se tocan. Se conservan los IDs (IDENTITY_INSERT) para que el
// historial siga apuntando a cada ítem.
func (r *SQLServerRepository) ImportInventory(ctx context.Context, src io.Reader) (int, error) {
	data, err := readImportWorkbook(src)
	if err != nil {
		return 0, err
	}
	var items []models.Item
	err = r.inTx(ctx, func(tx *sql.Tx) error {
		// Bloqueo de toda la tabla durante el reemplazo.
		current, err := queryAllTx(ctx, tx, scanItem, `SELECT `+itemColumns+` FROM dbo.Items WITH (TABLOCKX, HOLDLOCK) ORDER BY ID`)
		if err != nil {
			return err
		}
		var lastIssued int
		if err := tx.QueryRowContext(ctx, `SELECT ISNULL(CONVERT(INT, last_value), 0) FROM sys.identity_columns
			WHERE object_id = OBJECT_ID(N'dbo.Items')`).Scan(&lastIssued); err != nil {
			return err
		}
		if items, _, err = prepareImport(data, current, lastIssued, sqlTimestamp); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE dbo.Items SET EquipmentID = NULL; DELETE FROM dbo.Items;`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `SET IDENTITY_INSERT dbo.Items ON`); err != nil {
			return err
		}
		// Primero sin vínculos (un componente puede aparecer antes que su gabinete)
		// y después se restauran los vínculos.
		for _, item := range items {
			sinVinculo := item
			sinVinculo.EquipmentID = 0
			if _, err := tx.ExecContext(ctx, `INSERT INTO dbo.Items (ID, InventoryNumber, HasInventory, DeviceType, Brand, Model,
				SerialNumber, Quantity, Location, Status, Notes, CreatedAt, UpdatedAt, Availability, AssignedTo, LoanedAt, EquipmentID)
				VALUES (@p17, @p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9, @p10, @p11, @p12, @p13, @p14, @p15, @p16)`,
				append(itemValues(sinVinculo), item.ID)...); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `SET IDENTITY_INSERT dbo.Items OFF`); err != nil {
			return err
		}
		for _, item := range items {
			if item.EquipmentID == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE dbo.Items SET EquipmentID = @p1 WHERE ID = @p2`, item.EquipmentID, item.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("importar inventario: %w", err)
	}
	return len(items), nil
}

// queryAllTx es queryAll dentro de una transacción.
func queryAllTx[T any](ctx context.Context, tx *sql.Tx, scan func(rowScanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
