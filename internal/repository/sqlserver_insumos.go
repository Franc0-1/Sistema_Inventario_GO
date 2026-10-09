package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	mssql "github.com/microsoft/go-mssqldb"

	"inventario/internal/models"
)

// InsumoRepository guarda los insumos y sus movimientos. El stock solo
// cambia en RegistrarMovimiento, que bloquea la fila del insumo y guarda el
// movimiento en la misma transacción.
type InsumoRepository interface {
	ListInsumos(ctx context.Context, f models.FiltroInsumos) ([]models.Insumo, error)
	GetInsumo(ctx context.Context, id int) (models.Insumo, error)
	// CreateInsumo registra el stock inicial (si es mayor que 0) como un ajuste.
	CreateInsumo(ctx context.Context, i models.Insumo, usuario int) (models.Insumo, error)
	// UpdateInsumo no modifica el stock.
	UpdateInsumo(ctx context.Context, i models.Insumo, usuario int) (models.Insumo, error)
	// RegistrarMovimiento: en un AJUSTE, m.Cantidad es el stock contado y se
	// guarda la diferencia con el stock actual.
	RegistrarMovimiento(ctx context.Context, m models.MovimientoInsumo, usuario int) (models.MovimientoInsumo, models.Insumo, error)
	ListMovimientosInsumo(ctx context.Context, f models.FiltroMovimientosInsumo) ([]models.MovimientoInsumo, error)
}

var _ InsumoRepository = (*SQLServerRepository)(nil)

const insumoSelect = `SELECT i.ID, ISNULL(i.IDAnterior, 0), i.TipoID, t.Nombre, i.Marca, i.Modelo, i.Stock, i.StockMinimo,
	i.Observacion, i.Activo, i.CreadoEn, i.ActualizadoEn, i.RowVersion
	FROM dbo.Insumos i JOIN dbo.Tipos t ON t.ID = i.TipoID`

func scanInsumo(s rowScanner) (models.Insumo, error) {
	var i models.Insumo
	var v []byte
	err := s.Scan(&i.ID, &i.IDAnterior, &i.TipoID, &i.Tipo, &i.Marca, &i.Modelo, &i.Stock, &i.StockMinimo,
		&i.Observacion, &i.Activo, &i.CreadoEn, &i.ActualizadoEn, &v)
	i.Version = versionTexto(v)
	i.BajoStock = i.StockMinimo > 0 && i.Stock <= i.StockMinimo
	return i, err
}

func (r *SQLServerRepository) ListInsumos(ctx context.Context, f models.FiltroInsumos) ([]models.Insumo, error) {
	texto := strings.TrimSpace(f.Texto)
	return queryAll(ctx, r.db, scanInsumo, insumoSelect+`
		WHERE (@p1 = 0 OR i.TipoID = @p1) AND (@p2 = 0 OR i.Activo = 1)
		AND (@p3 = 0 OR (i.StockMinimo > 0 AND i.Stock <= i.StockMinimo))
		AND (@p4 = N'' OR t.Nombre LIKE @p5 OR i.Marca LIKE @p5 OR i.Modelo LIKE @p5)
		ORDER BY t.Nombre, i.Marca, i.Modelo`, f.TipoID, f.SoloActivos, f.BajoStock, texto, likeTexto(texto))
}

func insumoPorID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int, bloquear bool) (models.Insumo, error) {
	query := insumoSelect + ` WHERE i.ID = @p1`
	if bloquear {
		query = strings.Replace(query, "FROM dbo.Insumos i", "FROM dbo.Insumos i WITH (UPDLOCK, ROWLOCK)", 1)
	}
	i, err := scanInsumo(q.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return i, fmt.Errorf("%w: el insumo %d", ErrNoEncontrado, id)
	}
	return i, err
}

func (r *SQLServerRepository) GetInsumo(ctx context.Context, id int) (models.Insumo, error) {
	i, err := insumoPorID(ctx, r.db, id, false)
	if err != nil && !errors.Is(err, ErrNoEncontrado) {
		return i, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return i, err
}

func duplicadoInsumo(err error) error {
	return duplicadoPorIndice(err, map[string]string{
		"UX_Insumos_TipoMarcaModelo": "ya existe un insumo con ese tipo, marca y modelo",
	})
}

func insertarMovimiento(ctx context.Context, tx *sql.Tx, m models.MovimientoInsumo, usuario int) (int, error) {
	var id int
	err := tx.QueryRowContext(ctx, `INSERT INTO dbo.MovimientosInsumo (InsumoID, Tipo, Cantidad, PersonaID, OficinaID, Observacion, UsuarioID)
		OUTPUT inserted.ID VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7)`,
		m.InsumoID, string(m.Tipo), m.Cantidad, nullInt(m.PersonaID), nullInt(m.OficinaID), m.Observacion, usuario).Scan(&id)
	return id, err
}

func (r *SQLServerRepository) CreateInsumo(ctx context.Context, i models.Insumo, usuario int) (models.Insumo, error) {
	var out models.Insumo
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		var id int
		err := tx.QueryRowContext(ctx, `INSERT INTO dbo.Insumos (TipoID, Marca, Modelo, Stock, StockMinimo, Observacion, Activo, ActualizadoPor)
			OUTPUT inserted.ID VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8)`,
			i.TipoID, i.Marca, i.Modelo, i.Stock, i.StockMinimo, i.Observacion, i.Activo, nullInt(usuario)).Scan(&id)
		if err != nil {
			return duplicadoInsumo(err)
		}
		if i.Stock > 0 {
			if _, err := insertarMovimiento(ctx, tx, models.MovimientoInsumo{InsumoID: id, Tipo: models.MovimientoAjuste,
				Cantidad: i.Stock, Observacion: "Stock inicial"}, usuario); err != nil {
				return err
			}
		}
		out, err = insumoPorID(ctx, tx, id, false)
		return err
	})
	return out, err
}

func (r *SQLServerRepository) UpdateInsumo(ctx context.Context, i models.Insumo, usuario int) (models.Insumo, error) {
	version, err := versionBytes(i.Version)
	if err != nil {
		return i, err
	}
	var out models.Insumo
	err = r.inTx(ctx, func(tx *sql.Tx) error {
		actual, err := insumoPorID(ctx, tx, i.ID, true)
		if err != nil {
			return err
		}
		if actual.Activo && !i.Activo && actual.Stock > 0 {
			return fmt.Errorf("%w: el insumo tiene %d unidades en stock; registrá la salida o un ajuste antes de desactivarlo",
				ErrEnUso, actual.Stock)
		}
		var id int
		err = tx.QueryRowContext(ctx, `UPDATE dbo.Insumos SET TipoID = @p1, Marca = @p2, Modelo = @p3, StockMinimo = @p4,
				Observacion = @p5, Activo = @p6, ActualizadoEn = SYSDATETIMEOFFSET(), ActualizadoPor = @p7
			OUTPUT inserted.ID WHERE ID = @p8 AND RowVersion = @p9`,
			i.TipoID, i.Marca, i.Modelo, i.StockMinimo, i.Observacion, i.Activo, nullInt(usuario), i.ID, version).Scan(&id)
		if err = actualizada(ctx, tx, "dbo.Insumos", "el insumo", i.ID, err); err != nil {
			return duplicadoInsumo(err)
		}
		out, err = insumoPorID(ctx, tx, id, false)
		return err
	})
	return out, err
}

func (r *SQLServerRepository) RegistrarMovimiento(ctx context.Context, m models.MovimientoInsumo, usuario int) (models.MovimientoInsumo, models.Insumo, error) {
	var mov models.MovimientoInsumo
	var insumo models.Insumo
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		actual, err := insumoPorID(ctx, tx, m.InsumoID, true)
		if err != nil {
			return err
		}
		if !actual.Activo {
			return fmt.Errorf("%w: el insumo está inactivo; reactivalo antes de moverlo", ErrInvalidMovement)
		}
		nuevo := actual.Stock
		switch m.Tipo {
		case models.MovimientoEntrada:
			nuevo += m.Cantidad
		case models.MovimientoSalida:
			nuevo -= m.Cantidad
		case models.MovimientoAjuste:
			nuevo, m.Cantidad = m.Cantidad, m.Cantidad-actual.Stock
			if m.Cantidad == 0 {
				return fmt.Errorf("%w: el stock ya es %d; no hay nada que ajustar", ErrInvalidMovement, actual.Stock)
			}
		default:
			return fmt.Errorf("%w: tipo %q", ErrInvalidMovement, m.Tipo)
		}
		if nuevo < 0 {
			return fmt.Errorf("%w: hay %d unidades y se quieren sacar %d", ErrStockInsuficiente, actual.Stock, m.Cantidad)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE dbo.Insumos SET Stock = @p1, ActualizadoEn = SYSDATETIMEOFFSET(), ActualizadoPor = @p2
			WHERE ID = @p3`, nuevo, usuario, m.InsumoID); err != nil {
			return err
		}
		id, err := insertarMovimiento(ctx, tx, m, usuario)
		if err != nil {
			return err
		}
		if mov, err = scanMovimientoInsumo(tx.QueryRowContext(ctx, movimientoInsumoSelect+` WHERE m.ID = @p1`, id)); err != nil {
			return err
		}
		insumo, err = insumoPorID(ctx, tx, m.InsumoID, false)
		return err
	})
	return mov, insumo, err
}

const movimientoInsumoSelect = `SELECT m.ID, m.InsumoID, LTRIM(RTRIM(t.Nombre + N' ' + i.Marca + N' ' + i.Modelo)), m.Tipo, m.Cantidad,
	ISNULL(m.PersonaID, 0), ISNULL(LTRIM(RTRIM(p.Nombre + N' ' + p.Apellido)), N''), ISNULL(m.OficinaID, 0), ISNULL(o.Nombre, N''),
	m.Observacion, m.Fecha, u.Nombre
	FROM dbo.MovimientosInsumo m
	JOIN dbo.Insumos i ON i.ID = m.InsumoID
	JOIN dbo.Tipos t ON t.ID = i.TipoID
	JOIN dbo.Usuarios u ON u.ID = m.UsuarioID
	LEFT JOIN dbo.Personas p ON p.ID = m.PersonaID
	LEFT JOIN dbo.Oficinas o ON o.ID = m.OficinaID`

func scanMovimientoInsumo(s rowScanner) (models.MovimientoInsumo, error) {
	var m models.MovimientoInsumo
	err := s.Scan(&m.ID, &m.InsumoID, &m.Insumo, &m.Tipo, &m.Cantidad, &m.PersonaID, &m.Persona, &m.OficinaID, &m.Oficina,
		&m.Observacion, &m.Fecha, &m.Usuario)
	return m, err
}

// ListMovimientosInsumo filtra por días completos en la zona horaria del
// servidor de la aplicación: Desde a las 0:00 y Hasta hasta el final del día.
func (r *SQLServerRepository) ListMovimientosInsumo(ctx context.Context, f models.FiltroMovimientosInsumo) ([]models.MovimientoInsumo, error) {
	var desde, hasta any
	if !f.Desde.IsZero() {
		desde = mssql.DateTimeOffset(f.Desde)
	}
	if !f.Hasta.IsZero() {
		hasta = mssql.DateTimeOffset(f.Hasta.AddDate(0, 0, 1))
	}
	query := strings.Replace(movimientoInsumoSelect, "SELECT ", "SELECT TOP (@p7) ", 1) + `
		WHERE (@p1 = 0 OR m.InsumoID = @p1) AND (@p2 = 0 OR m.PersonaID = @p2) AND (@p3 = 0 OR m.OficinaID = @p3)
		AND (@p4 = '' OR m.Tipo = @p4) AND (@p5 IS NULL OR m.Fecha >= @p5) AND (@p6 IS NULL OR m.Fecha < @p6)
		ORDER BY m.ID DESC`
	return queryAll(ctx, r.db, scanMovimientoInsumo, query,
		f.InsumoID, f.PersonaID, f.OficinaID, string(f.Tipo), desde, hasta, f.Limite)
}
