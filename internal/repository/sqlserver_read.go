package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"inventario/internal/models"
)

// Lectura desde SQL Server. Search lee la tabla y filtra con el mismo
// itemMatcher que el repositorio de Excel: así ambos almacenamientos dan
// exactamente los mismos resultados (sin tildes, sin mayúsculas, bajas
// ocultas por defecto). Para el volumen de un inventario esto es rápido; si
// algún día crece mucho, el filtro puede pasarse a un WHERE.

const itemColumns = `ID, InventoryNumber, HasInventory, DeviceType, Brand, Model, SerialNumber, Quantity,
	Location, Status, Notes, CreatedAt, UpdatedAt, Availability, AssignedTo, LoanedAt, EquipmentID`

const sqlMovementColumns = `ID, ItemID, MovementType, Quantity, PreviousQuantity, NewQuantity,
	OriginLocation, DestinationLocation, Notes, CreatedAt`

// rowScanner es lo común entre *sql.Row y *sql.Rows.
type rowScanner interface{ Scan(dest ...any) error }

func scanItem(row rowScanner) (models.Item, error) {
	var (
		item         models.Item
		status       string
		availability string
		loanedAt     sql.NullTime
		equipmentID  sql.NullInt64
	)
	err := row.Scan(&item.ID, &item.InventoryNumber, &item.HasInventory, &item.DeviceType, &item.Brand, &item.Model,
		&item.SerialNumber, &item.Quantity, &item.Location, &status, &item.Notes, &item.CreatedAt, &item.UpdatedAt,
		&availability, &item.AssignedTo, &loanedAt, &equipmentID)
	if err != nil {
		return models.Item{}, err
	}
	item.Status = models.ItemStatus(status)
	item.Availability = models.Availability(availability)
	if loanedAt.Valid {
		t := loanedAt.Time
		item.LoanedAt = &t
	}
	item.EquipmentID = int(equipmentID.Int64)
	return item, nil
}

func scanMovement(row rowScanner) (models.Movement, error) {
	var (
		mov  models.Movement
		tipo string
	)
	err := row.Scan(&mov.ID, &mov.ItemID, &tipo, &mov.Quantity, &mov.PreviousQuantity, &mov.NewQuantity,
		&mov.OriginLocation, &mov.DestinationLocation, &mov.Notes, &mov.CreatedAt)
	mov.MovementType = models.MovementType(tipo)
	return mov, err
}

// queryAll ejecuta la consulta y convierte cada fila. Nunca devuelve nil:
// una lista vacía se serializa como [].
func queryAll[T any](ctx context.Context, db *sql.DB, scan func(rowScanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return out, nil
}

// GetAll devuelve todos los ítems, incluidas las bajas, ordenados por ID.
func (r *SQLServerRepository) GetAll(ctx context.Context) ([]models.Item, error) {
	return queryAll(ctx, r.db, scanItem, `SELECT `+itemColumns+` FROM dbo.Items ORDER BY ID`)
}

func (r *SQLServerRepository) GetByID(ctx context.Context, id int) (models.Item, error) {
	if id <= 0 {
		return models.Item{}, fmt.Errorf("%w: id %d", ErrItemNotFound, id)
	}
	item, err := scanItem(r.db.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM dbo.Items WHERE ID = @p1`, id))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return models.Item{}, fmt.Errorf("%w: id %d", ErrItemNotFound, id)
	case err != nil:
		return models.Item{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return item, nil
}

func (r *SQLServerRepository) Search(ctx context.Context, filter models.ItemFilter) ([]models.Item, error) {
	all, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	matcher := newItemMatcher(filter)
	result := []models.Item{}
	for _, item := range all {
		if matcher.matches(item) {
			result = append(result, item)
		}
	}
	return result, nil
}

// GetMovements devuelve todo el historial ordenado por ID.
func (r *SQLServerRepository) GetMovements(ctx context.Context) ([]models.Movement, error) {
	return queryAll(ctx, r.db, scanMovement, `SELECT `+sqlMovementColumns+` FROM dbo.Movements ORDER BY ID`)
}

// GetMovementsByItemID incluye los movimientos de ítems ya eliminados.
func (r *SQLServerRepository) GetMovementsByItemID(ctx context.Context, itemID int) ([]models.Movement, error) {
	return queryAll(ctx, r.db, scanMovement, `SELECT `+sqlMovementColumns+` FROM dbo.Movements WHERE ItemID = @p1 ORDER BY ID`, itemID)
}

func (r *SQLServerRepository) ListCategories(ctx context.Context) ([]models.Category, error) {
	return queryAll(ctx, r.db, func(row rowScanner) (models.Category, error) {
		var c models.Category
		err := row.Scan(&c.ID, &c.Name, &c.Active, &c.Loanable)
		return c, err
	}, `SELECT ID, Name, Active, Loanable FROM dbo.Categories ORDER BY Name`)
}
