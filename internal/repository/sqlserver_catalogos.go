package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	mssql "github.com/microsoft/go-mssqldb"

	"inventario/internal/models"
)

// CatalogoRepository guarda los catálogos del modelo nuevo
// (migrations/borrador/003_modelo_nuevo.sql). Solo existe en SQL Server.
// usuario es quien hace el cambio (0 = sin identificar) y queda en ActualizadoPor.
type CatalogoRepository interface {
	ListCatalogo(ctx context.Context, t models.TablaCatalogo, soloActivos bool) ([]models.Catalogo, error)
	GetCatalogo(ctx context.Context, t models.TablaCatalogo, id int) (models.Catalogo, error)
	CreateCatalogo(ctx context.Context, t models.TablaCatalogo, c models.Catalogo, usuario int) (models.Catalogo, error)
	UpdateCatalogo(ctx context.Context, t models.TablaCatalogo, c models.Catalogo, usuario int) (models.Catalogo, error)

	ListPersonas(ctx context.Context, f models.FiltroPersonas) ([]models.Persona, error)
	GetPersona(ctx context.Context, id int) (models.Persona, error)
	CreatePersona(ctx context.Context, p models.Persona, usuario int) (models.Persona, error)
	UpdatePersona(ctx context.Context, p models.Persona, usuario int) (models.Persona, error)

	ListTipos(ctx context.Context, clase models.Clase, soloActivos bool) ([]models.Tipo, error)
	GetTipo(ctx context.Context, id int) (models.Tipo, error)
	CreateTipo(ctx context.Context, t models.Tipo, usuario int) (models.Tipo, error)
	UpdateTipo(ctx context.Context, t models.Tipo, usuario int) (models.Tipo, error)
}

var _ CatalogoRepository = (*SQLServerRepository)(nil)

// Artículos para los mensajes de error de cada catálogo.
var catalogos = map[models.TablaCatalogo]struct{ un, el, llamado string }{
	models.CatalogoOficinas: {"una oficina", "la oficina", "llamada"},
	models.CatalogoPuestos:  {"un puesto", "el puesto", "llamado"},
	models.CatalogoUsuarios: {"un usuario", "el usuario", "llamado"},
}

// tabla valida el catálogo antes de usarlo como nombre de tabla en el SQL.
func tabla(t models.TablaCatalogo) (string, error) {
	if _, ok := catalogos[t]; !ok {
		return "", fmt.Errorf("catálogo desconocido %q", t)
	}
	return "dbo." + string(t), nil
}

// La versión viaja como texto hexadecimal (la RowVersion son 8 bytes).
func versionTexto(b []byte) string { return hex.EncodeToString(b) }

func versionBytes(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 8 {
		return nil, fmt.Errorf("%w: versión %q inválida; recargá los datos", ErrConflict, s)
	}
	return b, nil
}

func sqlErrorNumber(err error) int32 {
	var e mssql.Error
	if errors.As(err, &e) {
		return e.Number
	}
	return 0
}

// duplicado traduce la violación de un índice único al error de dato repetido.
func duplicado(err error, format string, args ...any) error {
	if n := sqlErrorNumber(err); n == 2601 || n == 2627 {
		return fmt.Errorf("%w: "+format, append([]any{ErrDuplicado}, args...)...)
	}
	return err
}

// actualizada resuelve un UPDATE que no afectó filas: el registro no existe
// o alguien lo cambió después de que el cliente lo leyó.
func actualizada(ctx context.Context, tx *sql.Tx, table, que string, id int, err error) error {
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var n int
	if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE ID = @p1`, id).Scan(&n); e != nil {
		return e
	}
	if n == 0 {
		return fmt.Errorf("%w: %s %d", ErrNoEncontrado, que, id)
	}
	return fmt.Errorf("%w: %s %d; recargá los datos", ErrConflict, que, id)
}

// bloquearActivo lee si el registro está activo y bloquea la fila hasta el
// final de la transacción.
func bloquearActivo(ctx context.Context, tx *sql.Tx, table, que string, id int) (bool, error) {
	var activo bool
	err := tx.QueryRowContext(ctx, `SELECT Activo FROM `+table+` WITH (UPDLOCK, ROWLOCK) WHERE ID = @p1`, id).Scan(&activo)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("%w: %s %d", ErrNoEncontrado, que, id)
	}
	return activo, err
}

// ---- Oficinas, puestos y usuarios ----

const catalogoColumnas = `ID, Nombre, Activo, RowVersion`

func scanCatalogo(s rowScanner) (models.Catalogo, error) {
	var c models.Catalogo
	var v []byte
	err := s.Scan(&c.ID, &c.Nombre, &c.Activo, &v)
	c.Version = versionTexto(v)
	return c, err
}

func (r *SQLServerRepository) ListCatalogo(ctx context.Context, t models.TablaCatalogo, soloActivos bool) ([]models.Catalogo, error) {
	table, err := tabla(t)
	if err != nil {
		return nil, err
	}
	q := `SELECT ` + catalogoColumnas + ` FROM ` + table
	if soloActivos {
		q += ` WHERE Activo = 1`
	}
	return queryAll(ctx, r.db, scanCatalogo, q+` ORDER BY Nombre`)
}

func (r *SQLServerRepository) GetCatalogo(ctx context.Context, t models.TablaCatalogo, id int) (models.Catalogo, error) {
	table, err := tabla(t)
	if err != nil {
		return models.Catalogo{}, err
	}
	c, err := scanCatalogo(r.db.QueryRowContext(ctx, `SELECT `+catalogoColumnas+` FROM `+table+` WHERE ID = @p1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("%w: %s %d", ErrNoEncontrado, catalogos[t].el, id)
	}
	if err != nil {
		return c, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return c, nil
}

func (r *SQLServerRepository) CreateCatalogo(ctx context.Context, t models.TablaCatalogo, c models.Catalogo, usuario int) (models.Catalogo, error) {
	table, err := tabla(t)
	if err != nil {
		return c, err
	}
	var out models.Catalogo
	err = r.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = scanCatalogo(tx.QueryRowContext(ctx, `INSERT INTO `+table+` (Nombre, Activo, ActualizadoPor)
			OUTPUT inserted.ID, inserted.Nombre, inserted.Activo, inserted.RowVersion VALUES (@p1, @p2, @p3)`,
			c.Nombre, c.Activo, nullInt(usuario)))
		return duplicado(err, "ya existe %s %s %q", catalogos[t].un, catalogos[t].llamado, c.Nombre)
	})
	return out, err
}

func (r *SQLServerRepository) UpdateCatalogo(ctx context.Context, t models.TablaCatalogo, c models.Catalogo, usuario int) (models.Catalogo, error) {
	table, err := tabla(t)
	if err != nil {
		return c, err
	}
	version, err := versionBytes(c.Version)
	if err != nil {
		return c, err
	}
	que := catalogos[t].el
	var out models.Catalogo
	err = r.inTx(ctx, func(tx *sql.Tx) error {
		activo, err := bloquearActivo(ctx, tx, table, que, c.ID)
		if err != nil {
			return err
		}
		if activo && !c.Activo && t == models.CatalogoOficinas {
			if err := oficinaEnUso(ctx, tx, c.ID); err != nil {
				return err
			}
		}
		out, err = scanCatalogo(tx.QueryRowContext(ctx, `UPDATE `+table+`
			SET Nombre = @p1, Activo = @p2, ActualizadoEn = SYSDATETIMEOFFSET(), ActualizadoPor = @p3
			OUTPUT inserted.ID, inserted.Nombre, inserted.Activo, inserted.RowVersion
			WHERE ID = @p4 AND RowVersion = @p5`, c.Nombre, c.Activo, nullInt(usuario), c.ID, version))
		if err = actualizada(ctx, tx, table, que, c.ID, err); err != nil {
			return duplicado(err, "ya existe %s %s %q", catalogos[t].un, catalogos[t].llamado, c.Nombre)
		}
		return nil
	})
	return out, err
}

// oficinaEnUso: no se desactiva una oficina con personas activas o con
// equipos o componentes que no estén de baja.
func oficinaEnUso(ctx context.Context, tx *sql.Tx, id int) error {
	var personas, cosas int
	err := tx.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM dbo.Personas WHERE OficinaID = @p1 AND Activo = 1),
		(SELECT COUNT(*) FROM dbo.Equipos WHERE OficinaID = @p1 AND Estado <> 'BAJA')
		+ (SELECT COUNT(*) FROM dbo.Componentes WHERE OficinaID = @p1 AND Estado <> 'BAJA')`, id).Scan(&personas, &cosas)
	if err != nil {
		return err
	}
	if personas+cosas > 0 {
		return fmt.Errorf("%w: la oficina tiene %d personas activas y %d equipos o componentes; movelos antes de desactivarla",
			ErrEnUso, personas, cosas)
	}
	return nil
}

// ---- Personas ----

const personaSelect = `SELECT p.ID, p.Nombre, p.Apellido, ISNULL(p.PuestoID, 0), ISNULL(pu.Nombre, N''),
	ISNULL(p.OficinaID, 0), ISNULL(o.Nombre, N''), p.Activo, p.RowVersion
	FROM dbo.Personas p
	LEFT JOIN dbo.Puestos pu ON pu.ID = p.PuestoID
	LEFT JOIN dbo.Oficinas o ON o.ID = p.OficinaID`

func scanPersona(s rowScanner) (models.Persona, error) {
	var p models.Persona
	var v []byte
	err := s.Scan(&p.ID, &p.Nombre, &p.Apellido, &p.PuestoID, &p.Puesto, &p.OficinaID, &p.Oficina, &p.Activo, &v)
	p.Version = versionTexto(v)
	return p, err
}

// likeTexto arma el patrón de LIKE escapando sus comodines.
func likeTexto(s string) string {
	r := strings.NewReplacer(`[`, `[[]`, `%`, `[%]`, `_`, `[_]`)
	return "%" + r.Replace(strings.TrimSpace(s)) + "%"
}

func (r *SQLServerRepository) ListPersonas(ctx context.Context, f models.FiltroPersonas) ([]models.Persona, error) {
	q := personaSelect + ` WHERE (@p1 = 0 OR p.OficinaID = @p1) AND (@p2 = 0 OR p.Activo = 1)
		AND (@p3 = N'' OR p.Nombre + N' ' + p.Apellido LIKE @p4 OR p.Apellido + N' ' + p.Nombre LIKE @p4)
		ORDER BY p.Apellido, p.Nombre`
	texto := strings.TrimSpace(f.Texto)
	return queryAll(ctx, r.db, scanPersona, q, f.OficinaID, f.SoloActivas, texto, likeTexto(texto))
}

func (r *SQLServerRepository) GetPersona(ctx context.Context, id int) (models.Persona, error) {
	p, err := scanPersona(r.db.QueryRowContext(ctx, personaSelect+` WHERE p.ID = @p1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("%w: la persona %d", ErrNoEncontrado, id)
	}
	if err != nil {
		return p, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return p, nil
}

func (r *SQLServerRepository) CreatePersona(ctx context.Context, p models.Persona, usuario int) (models.Persona, error) {
	var out models.Persona
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		var id int
		if err := tx.QueryRowContext(ctx, `INSERT INTO dbo.Personas (Nombre, Apellido, PuestoID, OficinaID, Activo, ActualizadoPor)
			OUTPUT inserted.ID VALUES (@p1, @p2, @p3, @p4, @p5, @p6)`,
			p.Nombre, p.Apellido, nullInt(p.PuestoID), nullInt(p.OficinaID), p.Activo, nullInt(usuario)).Scan(&id); err != nil {
			return err
		}
		var err error
		out, err = scanPersona(tx.QueryRowContext(ctx, personaSelect+` WHERE p.ID = @p1`, id))
		return err
	})
	return out, err
}

func (r *SQLServerRepository) UpdatePersona(ctx context.Context, p models.Persona, usuario int) (models.Persona, error) {
	version, err := versionBytes(p.Version)
	if err != nil {
		return p, err
	}
	var out models.Persona
	err = r.inTx(ctx, func(tx *sql.Tx) error {
		activo, err := bloquearActivo(ctx, tx, "dbo.Personas", "la persona", p.ID)
		if err != nil {
			return err
		}
		if activo && !p.Activo {
			if err := personaEnUso(ctx, tx, p.ID); err != nil {
				return err
			}
		}
		var id int
		err = tx.QueryRowContext(ctx, `UPDATE dbo.Personas
			SET Nombre = @p1, Apellido = @p2, PuestoID = @p3, OficinaID = @p4, Activo = @p5,
				ActualizadoEn = SYSDATETIMEOFFSET(), ActualizadoPor = @p6
			OUTPUT inserted.ID WHERE ID = @p7 AND RowVersion = @p8`,
			p.Nombre, p.Apellido, nullInt(p.PuestoID), nullInt(p.OficinaID), p.Activo, nullInt(usuario), p.ID, version).Scan(&id)
		if err = actualizada(ctx, tx, "dbo.Personas", "la persona", p.ID, err); err != nil {
			return err
		}
		out, err = scanPersona(tx.QueryRowContext(ctx, personaSelect+` WHERE p.ID = @p1`, id))
		return err
	})
	return out, err
}

// personaEnUso: no se desactiva una persona con equipos asignados o con un
// préstamo sin devolver.
func personaEnUso(ctx context.Context, tx *sql.Tx, id int) error {
	var equipos, prestamos int
	err := tx.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM dbo.Equipos WHERE PersonaID = @p1 AND Estado <> 'BAJA'),
		(SELECT COUNT(*) FROM dbo.Prestamos WHERE PersonaID = @p1 AND FechaDevolucion IS NULL)`, id).Scan(&equipos, &prestamos)
	if err != nil {
		return err
	}
	if equipos+prestamos > 0 {
		return fmt.Errorf("%w: la persona tiene %d equipos asignados y %d préstamos sin devolver; reasignalos antes de desactivarla",
			ErrEnUso, equipos, prestamos)
	}
	return nil
}

// ---- Tipos ----

const tipoColumnas = `ID, Nombre, Clase, Prestable, Activo, RowVersion`

func scanTipo(s rowScanner) (models.Tipo, error) {
	var t models.Tipo
	var v []byte
	err := s.Scan(&t.ID, &t.Nombre, &t.Clase, &t.Prestable, &t.Activo, &v)
	t.Version = versionTexto(v)
	return t, err
}

func (r *SQLServerRepository) ListTipos(ctx context.Context, clase models.Clase, soloActivos bool) ([]models.Tipo, error) {
	return queryAll(ctx, r.db, scanTipo, `SELECT `+tipoColumnas+` FROM dbo.Tipos
		WHERE (@p1 = '' OR Clase = @p1) AND (@p2 = 0 OR Activo = 1) ORDER BY Nombre`, string(clase), soloActivos)
}

func (r *SQLServerRepository) GetTipo(ctx context.Context, id int) (models.Tipo, error) {
	t, err := scanTipo(r.db.QueryRowContext(ctx, `SELECT `+tipoColumnas+` FROM dbo.Tipos WHERE ID = @p1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, fmt.Errorf("%w: el tipo %d", ErrNoEncontrado, id)
	}
	if err != nil {
		return t, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return t, nil
}

func (r *SQLServerRepository) CreateTipo(ctx context.Context, t models.Tipo, usuario int) (models.Tipo, error) {
	var out models.Tipo
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = scanTipo(tx.QueryRowContext(ctx, `INSERT INTO dbo.Tipos (Nombre, Clase, Prestable, Activo, ActualizadoPor)
			OUTPUT inserted.ID, inserted.Nombre, inserted.Clase, inserted.Prestable, inserted.Activo, inserted.RowVersion
			VALUES (@p1, @p2, @p3, @p4, @p5)`, t.Nombre, string(t.Clase), t.Prestable, t.Activo, nullInt(usuario)))
		return duplicado(err, "ya existe un tipo llamado %q", t.Nombre)
	})
	return out, err
}

func (r *SQLServerRepository) UpdateTipo(ctx context.Context, t models.Tipo, usuario int) (models.Tipo, error) {
	version, err := versionBytes(t.Version)
	if err != nil {
		return t, err
	}
	var out models.Tipo
	err = r.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = scanTipo(tx.QueryRowContext(ctx, `UPDATE dbo.Tipos
			SET Nombre = @p1, Clase = @p2, Prestable = @p3, Activo = @p4, ActualizadoEn = SYSDATETIMEOFFSET(), ActualizadoPor = @p5
			OUTPUT inserted.ID, inserted.Nombre, inserted.Clase, inserted.Prestable, inserted.Activo, inserted.RowVersion
			WHERE ID = @p6 AND RowVersion = @p7`, t.Nombre, string(t.Clase), t.Prestable, t.Activo, nullInt(usuario), t.ID, version))
		// Las FK compuestas (TipoID, Clase) de equipos, componentes e
		// insumos impiden cambiar la clase de un tipo con ítems.
		if sqlErrorNumber(err) == 547 && strings.Contains(err.Error(), "FK_") {
			return fmt.Errorf("%w: el tipo %q ya tiene ítems cargados; no se puede cambiar su clase", ErrEnUso, t.Nombre)
		}
		if err = actualizada(ctx, tx, "dbo.Tipos", "el tipo", t.ID, err); err != nil {
			return duplicado(err, "ya existe un tipo llamado %q", t.Nombre)
		}
		return nil
	})
	return out, err
}
