package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	mssql "github.com/microsoft/go-mssqldb"

	"inventario/internal/models"
)

// Escritura en SQL Server. Cada operación es una transacción: el ítem y su
// movimiento se guardan juntos o no se guarda nada (lo que en Excel hacían el
// respaldo, el temporal y el reemplazo). La fila se bloquea con UPDLOCK al
// leerla, así dos escrituras simultáneas sobre el mismo ítem se ordenan y la
// segunda ve la versión nueva (ErrConflict), igual que con Excel.

// sqlTimeUnit es la precisión de DATETIMEOFFSET(7).
const sqlTimeUnit = 100 * time.Nanosecond

// sqlTimestamp devuelve "ahora" con la precisión de la base y siempre posterior
// a prev: UpdatedAt es la versión del ítem y nunca puede repetirse.
func sqlTimestamp(prev time.Time) time.Time {
	t := time.Now().Round(0).Truncate(sqlTimeUnit)
	if !t.After(prev) {
		t = prev.Add(sqlTimeUnit)
	}
	return t
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

// inTx ejecuta fn en una transacción y traduce los errores de la base a los
// errores del repositorio.
func (r *SQLServerRepository) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return mapSQLError(err)
	}
	if err := tx.Commit(); err != nil {
		return mapSQLError(err)
	}
	return nil
}

// mapSQLError conserva los errores del repositorio y traduce las violaciones
// de restricciones a errores de datos (400), no a errores internos.
func mapSQLError(err error) error {
	for _, known := range []error{ErrConflict, ErrItemNotFound, ErrInvalidItem, ErrInvalidMovement, ErrStorageUnavailable, ErrDatabaseNotEmpty} {
		if errors.Is(err, known) {
			return err
		}
	}
	var sqlErr mssql.Error
	if errors.As(err, &sqlErr) {
		switch sqlErr.Number {
		case 2601, 2627: // índice o restricción UNIQUE
			return fmt.Errorf("%w: número de inventario o de serie duplicado", ErrInvalidItem)
		case 547: // CHECK o clave foránea
			return fmt.Errorf("%w: el dato no cumple una regla del inventario (%s)", ErrInvalidItem, sqlErr.Message)
		}
	}
	return fmt.Errorf("%w: %v", ErrWriteFailed, err)
}

// lockItem lee el ítem bloqueando su fila hasta el final de la transacción.
func lockItem(ctx context.Context, tx *sql.Tx, id int) (models.Item, error) {
	item, err := scanItem(tx.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM dbo.Items WITH (UPDLOCK, ROWLOCK) WHERE ID = @p1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return models.Item{}, fmt.Errorf("%w: id %d", ErrItemNotFound, id)
	}
	return item, err
}

func insertMovement(ctx context.Context, tx *sql.Tx, mov models.Movement) (models.Movement, error) {
	err := tx.QueryRowContext(ctx, `INSERT INTO dbo.Movements (ItemID, MovementType, Quantity, PreviousQuantity, NewQuantity,
		OriginLocation, DestinationLocation, Notes, CreatedAt) OUTPUT INSERTED.ID
		VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9)`,
		mov.ItemID, string(mov.MovementType), mov.Quantity, mov.PreviousQuantity, mov.NewQuantity,
		mov.OriginLocation, mov.DestinationLocation, mov.Notes, mov.CreatedAt).Scan(&mov.ID)
	return mov, err
}

// itemValues son los parámetros @p1…@p16 de INSERT y UPDATE, en el orden de itemColumns sin el ID.
func itemValues(item models.Item) []any {
	return []any{item.InventoryNumber, item.HasInventory, item.DeviceType, item.Brand, item.Model, item.SerialNumber,
		item.Quantity, item.Location, string(item.Status), item.Notes, item.CreatedAt, item.UpdatedAt,
		string(item.Availability), item.AssignedTo, nullTime(item.LoanedAt), nullInt(item.EquipmentID)}
}

func (r *SQLServerRepository) Create(ctx context.Context, item models.Item) (models.Item, error) {
	if err := checkStorable(item); err != nil {
		return models.Item{}, err
	}
	item.CreatedAt = sqlTimestamp(time.Time{})
	item.UpdatedAt = item.CreatedAt
	if item.Availability == "" {
		item.Availability = models.Available
	}
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO dbo.Items (InventoryNumber, HasInventory, DeviceType, Brand, Model,
			SerialNumber, Quantity, Location, Status, Notes, CreatedAt, UpdatedAt, Availability, AssignedTo, LoanedAt, EquipmentID)
			OUTPUT INSERTED.ID
			VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9, @p10, @p11, @p12, @p13, @p14, @p15, @p16)`,
			itemValues(item)...).Scan(&item.ID)
	})
	if err != nil {
		return models.Item{}, fmt.Errorf("crear ítem: %w", err)
	}
	return item, nil
}

func (r *SQLServerRepository) Update(ctx context.Context, item models.Item, expectedVersion time.Time, mov *models.Movement) (models.Item, error) {
	if err := checkStorable(item); err != nil {
		return models.Item{}, err
	}
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		current, err := lockItem(ctx, tx, item.ID)
		if err != nil {
			return err
		}
		if err := checkVersion(current, expectedVersion); err != nil {
			return err
		}
		item.CreatedAt = current.CreatedAt
		item.UpdatedAt = sqlTimestamp(current.UpdatedAt)
		_, err = tx.ExecContext(ctx, `UPDATE dbo.Items SET InventoryNumber = @p1, HasInventory = @p2, DeviceType = @p3,
			Brand = @p4, Model = @p5, SerialNumber = @p6, Quantity = @p7, Location = @p8, Status = @p9, Notes = @p10,
			CreatedAt = @p11, UpdatedAt = @p12, Availability = @p13, AssignedTo = @p14, LoanedAt = @p15, EquipmentID = @p16
			WHERE ID = @p17`, append(itemValues(item), item.ID)...)
		if err != nil || mov == nil {
			return err
		}
		stored, err := stockMovementFor(*mov, current, item)
		if err != nil {
			return err
		}
		_, err = insertMovement(ctx, tx, stored)
		return err
	})
	if err != nil {
		return models.Item{}, fmt.Errorf("actualizar ítem %d: %w", item.ID, err)
	}
	return item, nil
}

// Delete borra la fila. Los movimientos se conservan (no hay clave foránea
// desde Movements) y el ID no se reutiliza (IDENTITY).
func (r *SQLServerRepository) Delete(ctx context.Context, id int, expectedVersion time.Time) error {
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		current, err := lockItem(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := checkVersion(current, expectedVersion); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM dbo.Items WHERE ID = @p1`, id)
		return err
	})
	if err != nil {
		return fmt.Errorf("eliminar ítem %d: %w", id, err)
	}
	return nil
}

func (r *SQLServerRepository) UpdateStock(ctx context.Context, id, quantity int, expectedVersion time.Time, mov models.Movement) (models.Item, error) {
	if quantity < 0 {
		return models.Item{}, fmt.Errorf("%w: la cantidad no puede ser negativa (%d)", ErrInvalidItem, quantity)
	}
	var updated models.Item
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		current, err := lockItem(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := checkVersion(current, expectedVersion); err != nil {
			return err
		}
		updated = current
		updated.Quantity = quantity
		updated.UpdatedAt = sqlTimestamp(current.UpdatedAt)
		stored, err := stockMovementFor(mov, current, updated)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE dbo.Items SET Quantity = @p1, UpdatedAt = @p2 WHERE ID = @p3`,
			updated.Quantity, updated.UpdatedAt, id); err != nil {
			return err
		}
		_, err = insertMovement(ctx, tx, stored)
		return err
	})
	if err != nil {
		return models.Item{}, fmt.Errorf("actualizar stock del ítem %d: %w", id, err)
	}
	return updated, nil
}

func (r *SQLServerRepository) CreateMovement(ctx context.Context, mov models.Movement) (models.Movement, error) {
	if err := checkMovementStorable(mov); err != nil {
		return models.Movement{}, err
	}
	mov.CreatedAt = sqlTimestamp(time.Time{})
	var created models.Movement
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		created, err = insertMovement(ctx, tx, mov)
		return err
	})
	if err != nil {
		return models.Movement{}, fmt.Errorf("registrar movimiento: %w", err)
	}
	return created, nil
}
