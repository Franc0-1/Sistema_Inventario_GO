package repository

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb" // registra el driver "sqlserver"
)

// Almacenamiento en SQL Server. Implementa los mismos contratos que el
// repositorio de Excel; los métodos de lectura y escritura se agregan en las
// etapas siguientes de la migración.

// ErrStorageUnavailable: no se pudo conectar con la base o su esquema no está
// listo. Los handlers lo informan como servicio no disponible.
var ErrStorageUnavailable = errors.New("almacenamiento no disponible")

//go:embed migrations/sqlserver/*.sql
var sqlServerMigrations embed.FS

// SQLServerRepository guarda el inventario en SQL Server.
type SQLServerRepository struct {
	db *sql.DB
}

// NewSQLServerRepository abre el pool de conexiones, verifica que la base
// responda y aplica las migraciones pendientes. El DSN no se registra en los
// errores porque contiene la contraseña.
func NewSQLServerRepository(ctx context.Context, dsn string) (*SQLServerRepository, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("%w: falta INVENTARIO_DB_DSN", ErrStorageUnavailable)
	}
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: DSN inválido", ErrStorageUnavailable)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	if err := migrateSQLServer(ctx, db, sqlServerMigrations); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLServerRepository{db: db}, nil
}

// Check verifica que la base responda y que las tablas existan, sin escribir.
func (r *SQLServerRepository) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	const query = `SELECT
		(SELECT COUNT(*) FROM dbo.Items WHERE 1 = 0) +
		(SELECT COUNT(*) FROM dbo.Movements WHERE 1 = 0)`
	var n int
	if err := r.db.QueryRowContext(ctx, query).Scan(&n); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return nil
}

func (r *SQLServerRepository) Close() error { return r.db.Close() }

// MigrationVersions devuelve las migraciones aplicadas, en orden.
func (r *SQLServerRepository) MigrationVersions(ctx context.Context) ([]int, error) {
	return appliedMigrations(ctx, r.db)
}

// ============================================================
// Migraciones
// ============================================================

// migration es un archivo NNN_descripcion.sql. Cada archivo es un único lote
// (sin "GO") y se aplica en una transacción junto con su registro en
// SchemaMigrations: o se aplica completo o no se aplica.
type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations(files fs.FS) ([]migration, error) {
	names, err := fs.Glob(files, "migrations/sqlserver/*.sql")
	if err != nil {
		return nil, err
	}
	var out []migration
	seen := map[int]string{}
	for _, name := range names {
		base := path.Base(name)
		prefix, _, ok := strings.Cut(base, "_")
		version, err := strconv.Atoi(prefix)
		if !ok || err != nil || version <= 0 {
			return nil, fmt.Errorf("migración %q: el nombre debe empezar con un número, p. ej. 002_descripcion.sql", base)
		}
		if other, dup := seen[version]; dup {
			return nil, fmt.Errorf("migraciones %q y %q tienen la misma versión %d", other, base, version)
		}
		seen[version] = base
		content, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		if hasGoSeparator(string(content)) {
			return nil, fmt.Errorf("migración %q: no use GO; cada archivo es un único lote", base)
		}
		out = append(out, migration{version: version, name: base, sql: string(content)})
	}
	slices.SortFunc(out, func(a, b migration) int { return a.version - b.version })
	return out, nil
}

// hasGoSeparator detecta una línea "GO" (separador de lotes de SSMS, que el
// driver no entiende).
func hasGoSeparator(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), "GO") {
			return true
		}
	}
	return false
}

const createMigrationsTable = `IF OBJECT_ID(N'dbo.SchemaMigrations', N'U') IS NULL
CREATE TABLE dbo.SchemaMigrations (
    Version   INT               NOT NULL CONSTRAINT PK_SchemaMigrations PRIMARY KEY,
    Name      NVARCHAR(200)     NOT NULL,
    AppliedAt DATETIMEOFFSET(7) NOT NULL CONSTRAINT DF_SchemaMigrations_AppliedAt DEFAULT SYSDATETIMEOFFSET()
)`

func migrateSQLServer(ctx context.Context, db *sql.DB, files fs.FS) error {
	migrations, err := loadMigrations(files)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("%w: crear SchemaMigrations: %v", ErrStorageUnavailable, err)
	}
	applied, err := appliedMigrations(ctx, db)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if slices.Contains(applied, m.version) {
			continue
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return fmt.Errorf("%w: migración %s: %v", ErrStorageUnavailable, m.name, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Bloqueo de aplicación: si dos instancias arrancan juntas, la segunda
	// espera y luego ve la migración ya registrada (PRIMARY KEY).
	if _, err := tx.ExecContext(ctx, `EXEC sp_getapplock @Resource = 'inventario_migraciones', @LockMode = 'Exclusive', @LockOwner = 'Transaction', @LockTimeout = 30000`); err != nil {
		return err
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM dbo.SchemaMigrations WHERE Version = @p1`, m.version).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO dbo.SchemaMigrations (Version, Name) VALUES (@p1, @p2)`, m.version, m.name); err != nil {
		return err
	}
	return tx.Commit()
}

func appliedMigrations(ctx context.Context, db *sql.DB) ([]int, error) {
	rows, err := db.QueryContext(ctx, `SELECT Version FROM dbo.SchemaMigrations ORDER BY Version`)
	if err != nil {
		return nil, fmt.Errorf("%w: leer SchemaMigrations: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()
	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}
