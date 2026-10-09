package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"inventario/internal/models"
)

// EquipoRepository guarda equipos y componentes del modelo nuevo. Cada
// escritura que cambia la oficina, la persona o el estado de un equipo, o el
// equipo de un componente, agrega su fila de historial en la misma
// transacción. usuario es obligatorio (lo controla el servicio) y nota queda
// como observación del historial.
type EquipoRepository interface {
	ListEquipos(ctx context.Context, f models.FiltroEquipos) ([]models.Equipo, error)
	GetEquipo(ctx context.Context, id int) (models.Equipo, error)
	CreateEquipo(ctx context.Context, e models.Equipo, usuario int, nota string) (models.Equipo, error)
	UpdateEquipo(ctx context.Context, e models.Equipo, usuario int, nota string) (models.Equipo, error)
	HistorialEquipo(ctx context.Context, id int) ([]models.HistorialEquipo, error)

	ListComponentes(ctx context.Context, f models.FiltroComponentes) ([]models.Componente, error)
	GetComponente(ctx context.Context, id int) (models.Componente, error)
	CreateComponente(ctx context.Context, c models.Componente, usuario int, nota string) (models.Componente, error)
	UpdateComponente(ctx context.Context, c models.Componente, usuario int, nota string) (models.Componente, error)
	HistorialComponente(ctx context.Context, id int) ([]models.HistorialComponente, error)
}

var _ EquipoRepository = (*SQLServerRepository)(nil)

func ptrTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// duplicadoPorIndice traduce la violación de un índice único al mensaje del
// campo correspondiente (el nombre del índice viene en el error de SQL Server).
func duplicadoPorIndice(err error, mensajes map[string]string) error {
	if n := sqlErrorNumber(err); n == 2601 || n == 2627 {
		for indice, msg := range mensajes {
			if strings.Contains(err.Error(), indice) {
				return fmt.Errorf("%w: %s", ErrDuplicado, msg)
			}
		}
		return fmt.Errorf("%w: %v", ErrDuplicado, err)
	}
	return err
}

// ---- Equipos ----

const nombrePersona = `LTRIM(RTRIM(p.Nombre + N' ' + p.Apellido))`

const equipoSelect = `SELECT e.ID, ISNULL(e.IDAnterior, 0), e.NumeroInventario, e.TipoID, t.Nombre, e.Marca, e.Modelo,
	e.NumeroSerie, e.Estado, e.OficinaID, o.Nombre, ISNULL(e.PersonaID, 0), ISNULL(` + nombrePersona + `, N''),
	e.Observacion, e.FechaBaja, e.MotivoBaja, e.CreadoEn, e.ActualizadoEn, e.RowVersion,
	(SELECT COUNT(*) FROM dbo.Componentes k WHERE k.EquipoID = e.ID),
	ISNULL(pr.ID, 0), ISNULL(LTRIM(RTRIM(pp.Nombre + N' ' + pp.Apellido)), N'')
	FROM dbo.Equipos e
	JOIN dbo.Tipos t ON t.ID = e.TipoID
	JOIN dbo.Oficinas o ON o.ID = e.OficinaID
	LEFT JOIN dbo.Personas p ON p.ID = e.PersonaID
	LEFT JOIN dbo.Prestamos pr ON pr.EquipoID = e.ID AND pr.FechaDevolucion IS NULL
	LEFT JOIN dbo.Personas pp ON pp.ID = pr.PersonaID`

func scanEquipo(s rowScanner) (models.Equipo, error) {
	var e models.Equipo
	var baja sql.NullTime
	var v []byte
	err := s.Scan(&e.ID, &e.IDAnterior, &e.NumeroInventario, &e.TipoID, &e.Tipo, &e.Marca, &e.Modelo,
		&e.NumeroSerie, &e.Estado, &e.OficinaID, &e.Oficina, &e.PersonaID, &e.Persona,
		&e.Observacion, &baja, &e.MotivoBaja, &e.CreadoEn, &e.ActualizadoEn, &v, &e.CantidadComponentes,
		&e.PrestamoID, &e.PrestadoA)
	e.FechaBaja, e.Version = ptrTime(baja), versionTexto(v)
	return e, err
}

func (r *SQLServerRepository) ListEquipos(ctx context.Context, f models.FiltroEquipos) ([]models.Equipo, error) {
	texto := strings.TrimSpace(f.Texto)
	// Los N° de inventario numéricos se ordenan por largo y valor (999 antes
	// que 1000); los pendientes de numerar van al final.
	return queryAll(ctx, r.db, scanEquipo, equipoSelect+`
		WHERE (@p1 = 0 OR e.TipoID = @p1) AND (@p2 = 0 OR e.OficinaID = @p2) AND (@p3 = 0 OR e.PersonaID = @p3)
		AND (@p4 = '' OR e.Estado = @p4) AND (@p5 = 1 OR @p4 = 'BAJA' OR e.Estado <> 'BAJA')
		AND (@p6 = 0 OR e.NumeroInventario = N'')
		AND (@p7 = N'' OR e.NumeroInventario LIKE @p8 OR e.NumeroSerie LIKE @p8 OR e.Marca LIKE @p8
			OR e.Modelo LIKE @p8 OR t.Nombre LIKE @p8)
		ORDER BY CASE WHEN e.NumeroInventario = N'' THEN 1 ELSE 0 END, LEN(e.NumeroInventario), e.NumeroInventario, e.ID`,
		f.TipoID, f.OficinaID, f.PersonaID, string(f.Estado), f.IncluirBajas, f.Pendientes, texto, likeTexto(texto))
}

func equipoPorID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int) (models.Equipo, error) {
	e, err := scanEquipo(q.QueryRowContext(ctx, equipoSelect+` WHERE e.ID = @p1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, fmt.Errorf("%w: el equipo %d", ErrNoEncontrado, id)
	}
	return e, err
}

func (r *SQLServerRepository) GetEquipo(ctx context.Context, id int) (models.Equipo, error) {
	e, err := equipoPorID(ctx, r.db, id)
	if err != nil && !errors.Is(err, ErrNoEncontrado) {
		return e, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return e, err
}

func duplicadoEquipo(err error, e models.Equipo) error {
	return duplicadoPorIndice(err, map[string]string{
		"UX_Equipos_NumeroInventario": fmt.Sprintf("ya existe un equipo con el N° de inventario %q", e.NumeroInventario),
		"UX_Equipos_NumeroSerie":      fmt.Sprintf("ya existe un equipo con el N° de serie %q", e.NumeroSerie),
	})
}

func historialEquipo(ctx context.Context, tx *sql.Tx, id, ofiAnt, perAnt, ofiNueva, perNueva int, estado models.ItemStatus, nota string, usuario int) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO dbo.HistorialEquipos (EquipoID, OficinaAnteriorID, OficinaNuevaID,
		PersonaAnteriorID, PersonaNuevaID, Estado, Observacion, UsuarioID) VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8)`,
		id, nullInt(ofiAnt), nullInt(ofiNueva), nullInt(perAnt), nullInt(perNueva), string(estado), nota, usuario)
	return err
}

func (r *SQLServerRepository) CreateEquipo(ctx context.Context, e models.Equipo, usuario int, nota string) (models.Equipo, error) {
	var out models.Equipo
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		var id int
		err := tx.QueryRowContext(ctx, `INSERT INTO dbo.Equipos (NumeroInventario, TipoID, Marca, Modelo, NumeroSerie, Estado,
			OficinaID, PersonaID, Observacion, FechaBaja, MotivoBaja, ActualizadoPor)
			OUTPUT inserted.ID
			VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9,
				CASE WHEN @p6 = 'BAJA' THEN SYSDATETIMEOFFSET() END, CASE WHEN @p6 = 'BAJA' THEN @p10 ELSE N'' END, @p11)`,
			e.NumeroInventario, e.TipoID, e.Marca, e.Modelo, e.NumeroSerie, string(e.Estado),
			e.OficinaID, nullInt(e.PersonaID), e.Observacion, e.MotivoBaja, usuario).Scan(&id)
		if err != nil {
			return duplicadoEquipo(err, e)
		}
		if err := historialEquipo(ctx, tx, id, 0, 0, e.OficinaID, e.PersonaID, e.Estado, nota, usuario); err != nil {
			return err
		}
		out, err = equipoPorID(ctx, tx, id)
		return err
	})
	return out, err
}

func (r *SQLServerRepository) UpdateEquipo(ctx context.Context, e models.Equipo, usuario int, nota string) (models.Equipo, error) {
	version, err := versionBytes(e.Version)
	if err != nil {
		return e, err
	}
	var out models.Equipo
	err = r.inTx(ctx, func(tx *sql.Tx) error {
		var ofiAnt int
		var perAnt sql.NullInt64
		var estadoAnt models.ItemStatus
		err := tx.QueryRowContext(ctx, `SELECT OficinaID, PersonaID, Estado FROM dbo.Equipos WITH (UPDLOCK, ROWLOCK) WHERE ID = @p1`,
			e.ID).Scan(&ofiAnt, &perAnt, &estadoAnt)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: el equipo %d", ErrNoEncontrado, e.ID)
		}
		if err != nil {
			return err
		}
		if e.Estado == models.StatusRetired && estadoAnt != models.StatusRetired {
			if err := prestamoAbierto(ctx, tx, e.ID); err != nil {
				return err
			}
		}
		var id int
		err = tx.QueryRowContext(ctx, `UPDATE dbo.Equipos SET NumeroInventario = @p1, TipoID = @p2, Marca = @p3, Modelo = @p4,
				NumeroSerie = @p5, Estado = @p6, OficinaID = @p7, PersonaID = @p8, Observacion = @p9,
				FechaBaja = CASE WHEN @p6 = 'BAJA' THEN ISNULL(FechaBaja, SYSDATETIMEOFFSET()) END,
				MotivoBaja = CASE WHEN @p6 = 'BAJA' THEN @p10 ELSE N'' END,
				ActualizadoEn = SYSDATETIMEOFFSET(), ActualizadoPor = @p11
			OUTPUT inserted.ID WHERE ID = @p12 AND RowVersion = @p13`,
			e.NumeroInventario, e.TipoID, e.Marca, e.Modelo, e.NumeroSerie, string(e.Estado),
			e.OficinaID, nullInt(e.PersonaID), e.Observacion, e.MotivoBaja, usuario, e.ID, version).Scan(&id)
		if err = actualizada(ctx, tx, "dbo.Equipos", "el equipo", e.ID, err); err != nil {
			return duplicadoEquipo(err, e)
		}
		if ofiAnt != e.OficinaID || int(perAnt.Int64) != e.PersonaID || estadoAnt != e.Estado {
			if err := historialEquipo(ctx, tx, id, ofiAnt, int(perAnt.Int64), e.OficinaID, e.PersonaID, e.Estado, nota, usuario); err != nil {
				return err
			}
		}
		out, err = equipoPorID(ctx, tx, id)
		return err
	})
	return out, err
}

func (r *SQLServerRepository) HistorialEquipo(ctx context.Context, id int) ([]models.HistorialEquipo, error) {
	return queryAll(ctx, r.db, func(s rowScanner) (models.HistorialEquipo, error) {
		var h models.HistorialEquipo
		err := s.Scan(&h.ID, &h.EquipoID, &h.OficinaAnterior, &h.OficinaNueva, &h.PersonaAnterior, &h.PersonaNueva,
			&h.Estado, &h.Observacion, &h.Fecha, &h.Usuario)
		return h, err
	}, `SELECT h.ID, h.EquipoID, ISNULL(oa.Nombre, N''), ISNULL(onu.Nombre, N''),
		ISNULL(LTRIM(RTRIM(pa.Nombre + N' ' + pa.Apellido)), N''), ISNULL(LTRIM(RTRIM(pn.Nombre + N' ' + pn.Apellido)), N''),
		h.Estado, h.Observacion, h.Fecha, u.Nombre
		FROM dbo.HistorialEquipos h
		JOIN dbo.Usuarios u ON u.ID = h.UsuarioID
		LEFT JOIN dbo.Oficinas oa ON oa.ID = h.OficinaAnteriorID
		LEFT JOIN dbo.Oficinas onu ON onu.ID = h.OficinaNuevaID
		LEFT JOIN dbo.Personas pa ON pa.ID = h.PersonaAnteriorID
		LEFT JOIN dbo.Personas pn ON pn.ID = h.PersonaNuevaID
		WHERE h.EquipoID = @p1 ORDER BY h.ID DESC`, id)
}

// ---- Componentes ----

const componenteSelect = `SELECT k.ID, ISNULL(k.IDAnterior, 0), k.NumeroInventario, k.TipoID, t.Nombre, k.Marca, k.Modelo,
	k.NumeroSerie, k.Estado, ISNULL(k.EquipoID, 0), ISNULL(e.NumeroInventario, N''), ISNULL(k.OficinaID, 0), o.Nombre,
	k.Observacion, k.FechaBaja, k.MotivoBaja, k.CreadoEn, k.ActualizadoEn, k.RowVersion
	FROM dbo.Componentes k
	JOIN dbo.Tipos t ON t.ID = k.TipoID
	LEFT JOIN dbo.Equipos e ON e.ID = k.EquipoID
	JOIN dbo.Oficinas o ON o.ID = ISNULL(k.OficinaID, e.OficinaID)`

func scanComponente(s rowScanner) (models.Componente, error) {
	var c models.Componente
	var baja sql.NullTime
	var v []byte
	err := s.Scan(&c.ID, &c.IDAnterior, &c.NumeroInventario, &c.TipoID, &c.Tipo, &c.Marca, &c.Modelo,
		&c.NumeroSerie, &c.Estado, &c.EquipoID, &c.Equipo, &c.OficinaID, &c.Oficina,
		&c.Observacion, &baja, &c.MotivoBaja, &c.CreadoEn, &c.ActualizadoEn, &v)
	c.FechaBaja, c.Version = ptrTime(baja), versionTexto(v)
	return c, err
}

func (r *SQLServerRepository) ListComponentes(ctx context.Context, f models.FiltroComponentes) ([]models.Componente, error) {
	texto := strings.TrimSpace(f.Texto)
	return queryAll(ctx, r.db, scanComponente, componenteSelect+`
		WHERE (@p1 = 0 OR k.TipoID = @p1) AND (@p2 = 0 OR k.EquipoID = @p2) AND (@p3 = 0 OR o.ID = @p3)
		AND (@p4 = 0 OR k.EquipoID IS NULL) AND (@p5 = 1 OR k.Estado <> 'BAJA')
		AND (@p6 = N'' OR k.NumeroInventario LIKE @p7 OR k.NumeroSerie LIKE @p7 OR k.Marca LIKE @p7
			OR k.Modelo LIKE @p7 OR t.Nombre LIKE @p7)
		ORDER BY t.Nombre, k.NumeroInventario, k.ID`,
		f.TipoID, f.EquipoID, f.OficinaID, f.Sueltos, f.IncluirBajas, texto, likeTexto(texto))
}

func componentePorID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int) (models.Componente, error) {
	c, err := scanComponente(q.QueryRowContext(ctx, componenteSelect+` WHERE k.ID = @p1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("%w: el componente %d", ErrNoEncontrado, id)
	}
	return c, err
}

func (r *SQLServerRepository) GetComponente(ctx context.Context, id int) (models.Componente, error) {
	c, err := componentePorID(ctx, r.db, id)
	if err != nil && !errors.Is(err, ErrNoEncontrado) {
		return c, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return c, err
}

func duplicadoComponente(err error, c models.Componente) error {
	return duplicadoPorIndice(err, map[string]string{
		"UX_Componentes_NumeroSerie": fmt.Sprintf("ya existe un componente con el N° de serie %q", c.NumeroSerie),
	})
}

func historialComponente(ctx context.Context, tx *sql.Tx, id, anterior, nuevo int, nota string, usuario int) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO dbo.HistorialComponentes (ComponenteID, EquipoAnteriorID, EquipoNuevoID, Observacion, UsuarioID)
		VALUES (@p1, @p2, @p3, @p4, @p5)`, id, nullInt(anterior), nullInt(nuevo), nota, usuario)
	return err
}

func (r *SQLServerRepository) CreateComponente(ctx context.Context, c models.Componente, usuario int, nota string) (models.Componente, error) {
	var out models.Componente
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		var id int
		err := tx.QueryRowContext(ctx, `INSERT INTO dbo.Componentes (NumeroInventario, TipoID, Marca, Modelo, NumeroSerie, Estado,
			EquipoID, OficinaID, Observacion, FechaBaja, MotivoBaja, ActualizadoPor)
			OUTPUT inserted.ID
			VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9,
				CASE WHEN @p6 = 'BAJA' THEN SYSDATETIMEOFFSET() END, CASE WHEN @p6 = 'BAJA' THEN @p10 ELSE N'' END, @p11)`,
			c.NumeroInventario, c.TipoID, c.Marca, c.Modelo, c.NumeroSerie, string(c.Estado),
			nullInt(c.EquipoID), nullInt(c.OficinaID), c.Observacion, c.MotivoBaja, usuario).Scan(&id)
		if err != nil {
			return duplicadoComponente(err, c)
		}
		if err := historialComponente(ctx, tx, id, 0, c.EquipoID, nota, usuario); err != nil {
			return err
		}
		out, err = componentePorID(ctx, tx, id)
		return err
	})
	return out, err
}

func (r *SQLServerRepository) UpdateComponente(ctx context.Context, c models.Componente, usuario int, nota string) (models.Componente, error) {
	version, err := versionBytes(c.Version)
	if err != nil {
		return c, err
	}
	var out models.Componente
	err = r.inTx(ctx, func(tx *sql.Tx) error {
		var anterior sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT EquipoID FROM dbo.Componentes WITH (UPDLOCK, ROWLOCK) WHERE ID = @p1`, c.ID).Scan(&anterior)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: el componente %d", ErrNoEncontrado, c.ID)
		}
		if err != nil {
			return err
		}
		var id int
		err = tx.QueryRowContext(ctx, `UPDATE dbo.Componentes SET NumeroInventario = @p1, TipoID = @p2, Marca = @p3, Modelo = @p4,
				NumeroSerie = @p5, Estado = @p6, EquipoID = @p7, OficinaID = @p8, Observacion = @p9,
				FechaBaja = CASE WHEN @p6 = 'BAJA' THEN ISNULL(FechaBaja, SYSDATETIMEOFFSET()) END,
				MotivoBaja = CASE WHEN @p6 = 'BAJA' THEN @p10 ELSE N'' END,
				ActualizadoEn = SYSDATETIMEOFFSET(), ActualizadoPor = @p11
			OUTPUT inserted.ID WHERE ID = @p12 AND RowVersion = @p13`,
			c.NumeroInventario, c.TipoID, c.Marca, c.Modelo, c.NumeroSerie, string(c.Estado),
			nullInt(c.EquipoID), nullInt(c.OficinaID), c.Observacion, c.MotivoBaja, usuario, c.ID, version).Scan(&id)
		if err = actualizada(ctx, tx, "dbo.Componentes", "el componente", c.ID, err); err != nil {
			return duplicadoComponente(err, c)
		}
		if int(anterior.Int64) != c.EquipoID {
			if err := historialComponente(ctx, tx, id, int(anterior.Int64), c.EquipoID, nota, usuario); err != nil {
				return err
			}
		}
		out, err = componentePorID(ctx, tx, id)
		return err
	})
	return out, err
}

func (r *SQLServerRepository) HistorialComponente(ctx context.Context, id int) ([]models.HistorialComponente, error) {
	return queryAll(ctx, r.db, func(s rowScanner) (models.HistorialComponente, error) {
		var h models.HistorialComponente
		err := s.Scan(&h.ID, &h.ComponenteID, &h.EquipoAnteriorID, &h.EquipoAnterior, &h.EquipoNuevoID, &h.EquipoNuevo,
			&h.Observacion, &h.Fecha, &h.Usuario)
		return h, err
	}, `SELECT h.ID, h.ComponenteID, ISNULL(h.EquipoAnteriorID, 0), ISNULL(ea.NumeroInventario, N''),
		ISNULL(h.EquipoNuevoID, 0), ISNULL(en.NumeroInventario, N''), h.Observacion, h.Fecha, u.Nombre
		FROM dbo.HistorialComponentes h
		JOIN dbo.Usuarios u ON u.ID = h.UsuarioID
		LEFT JOIN dbo.Equipos ea ON ea.ID = h.EquipoAnteriorID
		LEFT JOIN dbo.Equipos en ON en.ID = h.EquipoNuevoID
		WHERE h.ComponenteID = @p1 ORDER BY h.ID DESC`, id)
}
