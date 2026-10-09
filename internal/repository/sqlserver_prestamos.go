package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	mssql "github.com/microsoft/go-mssqldb"

	"inventario/internal/models"
)

// PrestamoRepository guarda los préstamos. La base impide dos préstamos
// abiertos del mismo equipo; que el tipo sea prestable y la persona esté
// activa lo controla el servicio.
type PrestamoRepository interface {
	ListPrestamos(ctx context.Context, f models.FiltroPrestamos) ([]models.Prestamo, error)
	GetPrestamo(ctx context.Context, id int) (models.Prestamo, error)
	CreatePrestamo(ctx context.Context, p models.Prestamo, usuario int) (models.Prestamo, error)
	// DevolverPrestamo registra la devolución ahora; nota se agrega a la observación.
	DevolverPrestamo(ctx context.Context, id int, nota string, usuario int) (models.Prestamo, error)
}

var _ PrestamoRepository = (*SQLServerRepository)(nil)

const prestamoSelect = `SELECT pr.ID, pr.EquipoID, e.NumeroInventario, LTRIM(RTRIM(t.Nombre + N' ' + e.Marca + N' ' + e.Modelo)),
	pr.PersonaID, LTRIM(RTRIM(p.Nombre + N' ' + p.Apellido)), ISNULL(o.Nombre, N''),
	pr.FechaSalida, pr.DevolucionPrevista, pr.FechaDevolucion,
	CONVERT(BIT, CASE WHEN pr.FechaDevolucion IS NULL AND pr.DevolucionPrevista < SYSDATETIMEOFFSET() THEN 1 ELSE 0 END),
	pr.Observacion, u.Nombre
	FROM dbo.Prestamos pr
	JOIN dbo.Equipos e ON e.ID = pr.EquipoID
	JOIN dbo.Tipos t ON t.ID = e.TipoID
	JOIN dbo.Personas p ON p.ID = pr.PersonaID
	LEFT JOIN dbo.Oficinas o ON o.ID = p.OficinaID
	JOIN dbo.Usuarios u ON u.ID = pr.UsuarioID`

func scanPrestamo(s rowScanner) (models.Prestamo, error) {
	var p models.Prestamo
	var devolucion sql.NullTime
	err := s.Scan(&p.ID, &p.EquipoID, &p.Equipo, &p.EquipoDescripcion, &p.PersonaID, &p.Persona, &p.Oficina,
		&p.FechaSalida, &p.DevolucionPrevista, &devolucion, &p.Vencido, &p.Observacion, &p.Usuario)
	p.FechaDevolucion = ptrTime(devolucion)
	return p, err
}

func (r *SQLServerRepository) ListPrestamos(ctx context.Context, f models.FiltroPrestamos) ([]models.Prestamo, error) {
	// Abiertos primero (los vencidos antes), después los devueltos más recientes.
	query := strings.Replace(prestamoSelect, "SELECT ", "SELECT TOP (@p5) ", 1) + `
		WHERE (@p1 = 0 OR pr.EquipoID = @p1) AND (@p2 = 0 OR pr.PersonaID = @p2)
		AND (@p3 = 0 OR pr.FechaDevolucion IS NULL)
		AND (@p4 = 0 OR (pr.FechaDevolucion IS NULL AND pr.DevolucionPrevista < SYSDATETIMEOFFSET()))
		ORDER BY CASE WHEN pr.FechaDevolucion IS NULL THEN 0 ELSE 1 END, pr.DevolucionPrevista, pr.ID DESC`
	return queryAll(ctx, r.db, scanPrestamo, query, f.EquipoID, f.PersonaID, f.Abiertos, f.Vencidos, f.Limite)
}

func prestamoPorID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int) (models.Prestamo, error) {
	p, err := scanPrestamo(q.QueryRowContext(ctx, prestamoSelect+` WHERE pr.ID = @p1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("%w: el préstamo %d", ErrNoEncontrado, id)
	}
	return p, err
}

func (r *SQLServerRepository) GetPrestamo(ctx context.Context, id int) (models.Prestamo, error) {
	p, err := prestamoPorID(ctx, r.db, id)
	if err != nil && !errors.Is(err, ErrNoEncontrado) {
		return p, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return p, err
}

func (r *SQLServerRepository) CreatePrestamo(ctx context.Context, p models.Prestamo, usuario int) (models.Prestamo, error) {
	var out models.Prestamo
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		// Bloquea el equipo: una baja simultánea espera a que termine el préstamo.
		var estado models.ItemStatus
		err := tx.QueryRowContext(ctx, `SELECT Estado FROM dbo.Equipos WITH (UPDLOCK, ROWLOCK) WHERE ID = @p1`, p.EquipoID).Scan(&estado)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: el equipo %d", ErrNoEncontrado, p.EquipoID)
		}
		if err != nil {
			return err
		}
		if estado == models.StatusRetired {
			return fmt.Errorf("%w: el equipo está dado de baja", ErrEstadoInvalido)
		}
		var id int
		err = tx.QueryRowContext(ctx, `INSERT INTO dbo.Prestamos (EquipoID, PersonaID, FechaSalida, DevolucionPrevista, Observacion, UsuarioID)
			OUTPUT inserted.ID VALUES (@p1, @p2, @p3, @p4, @p5, @p6)`,
			p.EquipoID, p.PersonaID, mssql.DateTimeOffset(p.FechaSalida), mssql.DateTimeOffset(p.DevolucionPrevista),
			p.Observacion, usuario).Scan(&id)
		if n := sqlErrorNumber(err); n == 2601 || n == 2627 {
			var a string
			_ = tx.QueryRowContext(ctx, `SELECT LTRIM(RTRIM(p.Nombre + N' ' + p.Apellido)) FROM dbo.Prestamos pr
				JOIN dbo.Personas p ON p.ID = pr.PersonaID WHERE pr.EquipoID = @p1 AND pr.FechaDevolucion IS NULL`, p.EquipoID).Scan(&a)
			return fmt.Errorf("%w: el equipo ya está prestado a %s", ErrEnUso, a)
		}
		if err != nil {
			return err
		}
		out, err = prestamoPorID(ctx, tx, id)
		return err
	})
	return out, err
}

func (r *SQLServerRepository) DevolverPrestamo(ctx context.Context, id int, nota string, usuario int) (models.Prestamo, error) {
	var out models.Prestamo
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		var devuelto sql.NullTime
		err := tx.QueryRowContext(ctx, `SELECT FechaDevolucion FROM dbo.Prestamos WITH (UPDLOCK, ROWLOCK) WHERE ID = @p1`, id).Scan(&devuelto)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: el préstamo %d", ErrNoEncontrado, id)
		}
		if err != nil {
			return err
		}
		if devuelto.Valid {
			return fmt.Errorf("%w: el préstamo ya se devolvió el %s", ErrEstadoInvalido, devuelto.Time.Local().Format("02/01/2006 15:04"))
		}
		if _, err := tx.ExecContext(ctx, `UPDATE dbo.Prestamos SET FechaDevolucion = @p1,
				Observacion = LEFT(CASE WHEN @p2 = N'' THEN Observacion WHEN Observacion = N'' THEN @p2
					ELSE Observacion + N' | Devolución: ' + @p2 END, 1000),
				ActualizadoEn = SYSDATETIMEOFFSET(), ActualizadoPor = @p3
			WHERE ID = @p4`, mssql.DateTimeOffset(time.Now()), nota, usuario, id); err != nil {
			return err
		}
		out, err = prestamoPorID(ctx, tx, id)
		return err
	})
	return out, err
}

// prestamoAbierto: no se da de baja un equipo prestado.
func prestamoAbierto(ctx context.Context, tx *sql.Tx, equipoID int) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM dbo.Prestamos WHERE EquipoID = @p1 AND FechaDevolucion IS NULL`,
		equipoID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: el equipo está prestado; registrá la devolución antes de darlo de baja", ErrEnUso)
	}
	return nil
}
