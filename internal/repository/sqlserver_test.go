package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
)

// Las pruebas contra un SQL Server real se ejecutan solo si INVENTARIO_TEST_DSN
// apunta a una base de PRUEBA (nunca la de producción): pueden crear tablas.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("INVENTARIO_TEST_DSN")
	if dsn == "" {
		t.Skip("definir INVENTARIO_TEST_DSN con una base de prueba para correr este test")
	}
	return dsn
}

func TestLoadMigrations_Embebidas(t *testing.T) {
	migrations, err := loadMigrations(sqlServerMigrations)
	must(t, err)
	if len(migrations) == 0 || migrations[0].version != 1 {
		t.Fatalf("migraciones = %+v; se esperaba empezar por la 001", migrations)
	}
	for _, tabla := range []string{"dbo.Items", "dbo.Movements", "dbo.Categories"} {
		if !strings.Contains(migrations[0].sql, "CREATE TABLE "+tabla) {
			t.Errorf("la migración inicial no crea %s", tabla)
		}
	}
}

func TestLoadMigrations_OrdenYValidaciones(t *testing.T) {
	archivo := func(sql string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(sql)} }
	ok := fstest.MapFS{
		"migrations/sqlserver/010_c.sql": archivo("SELECT 10"),
		"migrations/sqlserver/002_b.sql": archivo("SELECT 2"),
		"migrations/sqlserver/001_a.sql": archivo("SELECT 1"),
	}
	migrations, err := loadMigrations(ok)
	must(t, err)
	var versions []int
	for _, m := range migrations {
		versions = append(versions, m.version)
	}
	if !slices.Equal(versions, []int{1, 2, 10}) {
		t.Errorf("orden = %v; se esperaba [1 2 10] (numérico, no alfabético)", versions)
	}

	invalidos := map[string]fstest.MapFS{
		"sin número":       {"migrations/sqlserver/inicial.sql": archivo("SELECT 1")},
		"versión cero":     {"migrations/sqlserver/000_a.sql": archivo("SELECT 1")},
		"versión repetida": {"migrations/sqlserver/001_a.sql": archivo("SELECT 1"), "migrations/sqlserver/001_b.sql": archivo("SELECT 2")},
		"usa GO":           {"migrations/sqlserver/001_a.sql": archivo("CREATE TABLE x (a INT)\ngo\nSELECT 1")},
	}
	for name, files := range invalidos {
		t.Run(name, func(t *testing.T) {
			if _, err := loadMigrations(files); err == nil {
				t.Error("debería rechazarse")
			}
		})
	}
}

func TestNewSQLServerRepository_SinDSN(t *testing.T) {
	_, err := NewSQLServerRepository(context.Background(), "  ")
	if !errors.Is(err, ErrStorageUnavailable) || !strings.Contains(err.Error(), "INVENTARIO_DB_DSN") {
		t.Errorf("error = %v", err)
	}
}

// Contra una base real: aplicar las migraciones dos veces no falla ni las repite.
func TestSQLServer_MigracionesIdempotentes(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		repo, err := NewSQLServerRepository(ctx, dsn)
		must(t, err)
		must(t, repo.Check(ctx))
		versions, err := repo.MigrationVersions(ctx)
		must(t, err)
		if len(versions) == 0 || versions[0] != 1 {
			t.Errorf("versiones = %v", versions)
		}
		must(t, repo.Close())
	}
}

// testSQLRepo abre la base de prueba y la deja con datos conocidos. Por
// seguridad exige que el nombre de la base termine en "_Test": estos tests
// borran las tablas.
func testSQLRepo(t *testing.T) *SQLServerRepository {
	t.Helper()
	ctx := context.Background()
	repo, err := NewSQLServerRepository(ctx, testDSN(t))
	must(t, err)
	t.Cleanup(func() { repo.Close() })
	var nombre string
	must(t, repo.db.QueryRowContext(ctx, `SELECT DB_NAME()`).Scan(&nombre))
	if !strings.HasSuffix(strings.ToLower(nombre), "_test") {
		t.Fatalf("la base %q no termina en _Test: no se borran datos de una base real", nombre)
	}
	_, err = repo.db.ExecContext(ctx, `
		DELETE FROM dbo.Movements;
		UPDATE dbo.Items SET EquipmentID = NULL;
		DELETE FROM dbo.Items;
		SET IDENTITY_INSERT dbo.Items ON;
		INSERT INTO dbo.Items (ID, InventoryNumber, HasInventory, DeviceType, Brand, Model, SerialNumber, Quantity,
			Location, Status, Notes, CreatedAt, UpdatedAt, Availability, AssignedTo, LoanedAt, EquipmentID) VALUES
		(1, N'1001', 1, N'Gabinete', N'Dell', N'OptiPlex 7090', N'DX-1', 1, N'Administración', 'OPERATIVO', N'Color: Negro',
			'2026-09-01T10:00:00.1234567-03:00', '2026-09-15T09:30:00-03:00', 'PRESTADO', N'María Gómez', '2026-09-15T09:30:00-03:00', NULL),
		(2, N'1002', 1, N'Monitor', N'LG', N'24MK430', N'LG-1', 1, N'Sistemas', 'OPERATIVO', N'',
			'2026-09-02T08:00:00-03:00', '2026-09-02T08:00:00-03:00', 'DISPONIBLE', N'', NULL, 1),
		(3, N'', 0, N'Accesorio', N'Genérico', N'Cable HDMI', N'', 12, N'Sistemas', 'OPERATIVO', N'',
			'2026-09-03T08:00:00-03:00', '2026-09-03T08:00:00-03:00', 'DISPONIBLE', N'', NULL, NULL),
		(5, N'1500', 1, N'Impresora', N'HP', N'LaserJet', N'VNB3K', 1, N'Recepción', 'BAJA', N'',
			'2026-01-05T08:00:00-03:00', '2026-09-20T08:00:00-03:00', 'DISPONIBLE', N'', NULL, NULL);
		SET IDENTITY_INSERT dbo.Items OFF;
		SET IDENTITY_INSERT dbo.Movements ON;
		INSERT INTO dbo.Movements (ID, ItemID, MovementType, Quantity, PreviousQuantity, NewQuantity,
			OriginLocation, DestinationLocation, Notes, CreatedAt) VALUES
		(1, 3, 'stock_out', 2, 14, 12, N'', N'', N'Entrega', '2026-09-10T12:00:00-03:00'),
		(2, 4, 'stock_in', 5, 0, 5, N'', N'', N'', '2026-09-11T12:00:00-03:00'),  -- ítem 4 eliminado
		(3, 3, 'transfer', 12, 12, 12, N'Depósito', N'Sistemas', N'', '2026-09-12T12:00:00-03:00');
		SET IDENTITY_INSERT dbo.Movements OFF;`)
	must(t, err)
	return repo
}

func TestSQLServer_Lectura(t *testing.T) {
	repo := testSQLRepo(t)
	ctx := context.Background()

	all, err := repo.GetAll(ctx)
	must(t, err)
	if got := ids(all); !slices.Equal(got, []int{1, 2, 3, 5}) {
		t.Fatalf("GetAll = %v; se esperaba [1 2 3 5] (incluye la baja)", got)
	}

	gab, err := repo.GetByID(ctx, 1)
	must(t, err)
	if gab.Brand != "Dell" || gab.Availability != models.Loaned || gab.AssignedTo != "María Gómez" || gab.LoanedAt == nil ||
		gab.Notes != "Color: Negro" || gab.CreatedAt.Nanosecond() != 123456700 {
		t.Errorf("GetByID(1) = %+v", gab)
	}
	if _, off := gab.CreatedAt.Zone(); off != -3*3600 {
		t.Errorf("la fecha perdió la zona horaria: %v", gab.CreatedAt)
	}
	if mon, _ := repo.GetByID(ctx, 2); mon.EquipmentID != 1 || mon.LoanedAt != nil {
		t.Errorf("GetByID(2) = %+v", mon)
	}
	for _, id := range []int{4, 99, 0} {
		if _, err := repo.GetByID(ctx, id); !errors.Is(err, ErrItemNotFound) {
			t.Errorf("GetByID(%d) error = %v; se esperaba ErrItemNotFound", id, err)
		}
	}

	busquedas := []struct {
		name   string
		filter models.ItemFilter
		want   []int
	}{
		{"bajas ocultas por defecto", models.ItemFilter{}, []int{1, 2, 3}},
		{"texto sin tildes", models.ItemFilter{Text: "generico"}, []int{3}},
		{"área sin tildes", models.ItemFilter{Location: "administracion"}, []int{1}},
		{"por estado BAJA", models.ItemFilter{Status: models.StatusRetired}, []int{5}},
		{"prestados", models.ItemFilter{Availability: models.Loaned}, []int{1}},
		{"combinados", models.ItemFilter{Location: "Sistemas", HasInventory: ptr(true)}, []int{2}},
	}
	for _, b := range busquedas {
		items, err := repo.Search(ctx, b.filter)
		must(t, err)
		if got := ids(items); !slices.Equal(got, b.want) {
			t.Errorf("Search %s = %v; se esperaba %v", b.name, got, b.want)
		}
	}

	movs, err := repo.GetMovements(ctx)
	must(t, err)
	if got := idsDe(movs); !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("GetMovements = %v", got)
	}
	del3, err := repo.GetMovementsByItemID(ctx, 3)
	must(t, err)
	if len(del3) != 2 || del3[1].MovementType != models.MovementTransfer || del3[1].OriginLocation != "Depósito" {
		t.Errorf("movimientos del ítem 3 = %+v", del3)
	}
	if eliminado, _ := repo.GetMovementsByItemID(ctx, 4); len(eliminado) != 1 {
		t.Errorf("el historial de un ítem eliminado debe conservarse: %+v", eliminado)
	}
	if vacio, err := repo.GetMovementsByItemID(ctx, 2); err != nil || vacio == nil || len(vacio) != 0 {
		t.Errorf("sin movimientos debe devolver [] (no nil): %v, %v", vacio, err)
	}

	cats, err := repo.ListCategories(ctx)
	must(t, err)
	if len(cats) != 15 {
		t.Errorf("%d categorías; se esperaban las 15 iniciales", len(cats))
	}
}

func nuevoItem(inv, serie string) models.Item {
	return models.Item{InventoryNumber: inv, HasInventory: inv != "", DeviceType: "Monitor", Brand: "Samsung",
		Model: "S24", SerialNumber: serie, Quantity: 1, Location: "Depósito", Status: models.StatusOperational}
}

func TestSQLServer_CreateYUpdate(t *testing.T) {
	repo := testSQLRepo(t)
	ctx := context.Background()

	creado, err := repo.Create(ctx, nuevoItem("2001", "SM-1"))
	must(t, err)
	if creado.ID <= 5 || creado.CreatedAt.IsZero() || !creado.UpdatedAt.Equal(creado.CreatedAt) || creado.Availability != models.Available {
		t.Fatalf("creado = %+v", creado)
	}
	leido, err := repo.GetByID(ctx, creado.ID)
	must(t, err)
	if !leido.UpdatedAt.Equal(creado.UpdatedAt) || leido.Brand != "Samsung" {
		t.Errorf("releído = %+v; la versión debe coincidir exactamente con la devuelta", leido)
	}

	for name, it := range map[string]models.Item{
		"N° de inventario repetido (sin tildes ni mayúsculas)": nuevoItem("1001", ""),
		"N° de serie repetido":                                 nuevoItem("2002", "lg-1"),
		"cantidad negativa":                                    func() models.Item { i := nuevoItem("2003", ""); i.Quantity = -1; return i }(),
		"estado inválido":                                      func() models.Item { i := nuevoItem("2004", ""); i.Status = "ROTO"; return i }(),
	} {
		if _, err := repo.Create(ctx, it); !errors.Is(err, ErrInvalidItem) {
			t.Errorf("%s: error = %v; se esperaba ErrInvalidItem", name, err)
		}
	}

	cambio := leido
	cambio.Quantity, cambio.Notes = 3, "recuento"
	mov := models.Movement{MovementType: models.MovementAdjustment, Quantity: 2}
	actualizado, err := repo.Update(ctx, cambio, leido.UpdatedAt, &mov)
	must(t, err)
	if !actualizado.UpdatedAt.After(leido.UpdatedAt) || !actualizado.CreatedAt.Equal(leido.CreatedAt) {
		t.Errorf("fechas tras Update = %+v", actualizado)
	}
	movs, err := repo.GetMovementsByItemID(ctx, creado.ID)
	must(t, err)
	if len(movs) != 1 || movs[0].PreviousQuantity != 1 || movs[0].NewQuantity != 3 || !movs[0].CreatedAt.Equal(actualizado.UpdatedAt) {
		t.Errorf("movimiento del ajuste = %+v", movs)
	}

	// Versión vieja: conflicto y nada cambia.
	if _, err := repo.Update(ctx, cambio, leido.UpdatedAt, nil); !errors.Is(err, ErrConflict) {
		t.Errorf("Update con versión vieja: error = %v; se esperaba ErrConflict", err)
	}
	if _, err := repo.Update(ctx, models.Item{ID: 999, Status: models.StatusOperational}, time.Now(), nil); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("Update de inexistente: error = %v", err)
	}
}

func TestSQLServer_UpdateStock(t *testing.T) {
	repo := testSQLRepo(t)
	ctx := context.Background()
	antes, _ := repo.GetByID(ctx, 3) // 12 unidades

	item, err := repo.UpdateStock(ctx, 3, 20, antes.UpdatedAt, models.Movement{MovementType: models.MovementStockIn, Quantity: 8})
	must(t, err)
	if item.Quantity != 20 || item.Location != antes.Location {
		t.Errorf("ítem = %+v", item)
	}
	movs, _ := repo.GetMovementsByItemID(ctx, 3)
	ultimo := movs[len(movs)-1]
	if ultimo.MovementType != models.MovementStockIn || ultimo.PreviousQuantity != 12 || ultimo.NewQuantity != 20 {
		t.Errorf("movimiento = %+v", ultimo)
	}

	// Movimiento que no coincide con el cambio: se revierte todo (ni stock ni movimiento).
	_, err = repo.UpdateStock(ctx, 3, 25, item.UpdatedAt, models.Movement{MovementType: models.MovementStockIn, Quantity: 1})
	if !errors.Is(err, ErrInvalidMovement) {
		t.Fatalf("error = %v; se esperaba ErrInvalidMovement", err)
	}
	if despues, _ := repo.GetByID(ctx, 3); despues.Quantity != 20 || !despues.UpdatedAt.Equal(item.UpdatedAt) {
		t.Errorf("el stock cambió aunque el movimiento falló: %+v", despues)
	}
	if otra, _ := repo.GetMovementsByItemID(ctx, 3); len(otra) != len(movs) {
		t.Error("se guardó un movimiento de una operación fallida")
	}
	if _, err := repo.UpdateStock(ctx, 3, 5, antes.UpdatedAt, models.Movement{MovementType: models.MovementStockOut, Quantity: 15}); !errors.Is(err, ErrConflict) {
		t.Errorf("versión vieja: error = %v; se esperaba ErrConflict", err)
	}
}

func TestSQLServer_DeleteYCreateMovement(t *testing.T) {
	repo := testSQLRepo(t)
	ctx := context.Background()

	gabinete, _ := repo.GetByID(ctx, 1) // tiene el monitor 2 vinculado
	if err := repo.Delete(ctx, 1, gabinete.UpdatedAt); !errors.Is(err, ErrInvalidItem) {
		t.Errorf("borrar un gabinete con componentes: error = %v; se esperaba ErrInvalidItem", err)
	}
	item3, _ := repo.GetByID(ctx, 3)
	if err := repo.Delete(ctx, 3, time.Now()); !errors.Is(err, ErrConflict) {
		t.Errorf("borrar con versión vieja: error = %v", err)
	}
	must(t, repo.Delete(ctx, 3, item3.UpdatedAt))
	if _, err := repo.GetByID(ctx, 3); !errors.Is(err, ErrItemNotFound) {
		t.Error("el ítem 3 debería estar eliminado")
	}
	if movs, _ := repo.GetMovementsByItemID(ctx, 3); len(movs) != 2 {
		t.Errorf("el historial del ítem eliminado debe conservarse: %v", movs)
	}

	mov, err := repo.CreateMovement(ctx, models.Movement{ItemID: 2, MovementType: models.MovementTransfer, Quantity: 1,
		PreviousQuantity: 1, NewQuantity: 1, OriginLocation: "Sistemas", DestinationLocation: "Depósito"})
	must(t, err)
	if mov.ID <= 3 || mov.CreatedAt.IsZero() {
		t.Errorf("movimiento creado = %+v", mov)
	}
}

// Salidas simultáneas: nunca stock negativo y cada salida exitosa tiene su movimiento.
func TestSQLServer_SalidasConcurrentes(t *testing.T) {
	repo := testSQLRepo(t)
	ctx := context.Background()
	movsAntes, _ := repo.GetMovementsByItemID(ctx, 3)

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		exitos int
	)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			actual, err := repo.GetByID(ctx, 3)
			if err != nil || actual.Quantity == 0 {
				return
			}
			_, err = repo.UpdateStock(ctx, 3, actual.Quantity-1, actual.UpdatedAt,
				models.Movement{MovementType: models.MovementStockOut, Quantity: 1})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				exitos++
			case !errors.Is(err, ErrConflict):
				t.Errorf("error inesperado: %v", err)
			}
		}()
	}
	wg.Wait()
	final, _ := repo.GetByID(ctx, 3)
	movs, _ := repo.GetMovementsByItemID(ctx, 3)
	if final.Quantity != 12-exitos || len(movs) != len(movsAntes)+exitos || exitos == 0 {
		t.Errorf("stock %d tras %d salidas exitosas; %d movimientos nuevos", final.Quantity, exitos, len(movs)-len(movsAntes))
	}
}

// Descargar y volver a subir no cambia nada; una fila nueva sin ID recibe un ID
// que nunca se usó, y los vínculos de equipos se conservan.
func TestSQLServer_ExportarEImportar(t *testing.T) {
	repo := testSQLRepo(t)
	ctx := context.Background()
	antes, err := repo.GetAll(ctx)
	must(t, err)
	movsAntes, _ := repo.GetMovements(ctx)

	data, err := repo.ExportInventory(ctx)
	must(t, err)
	n, err := repo.ImportInventory(ctx, bytes.NewReader(data))
	must(t, err)
	despues, err := repo.GetAll(ctx)
	must(t, err)
	if n != len(antes) || len(despues) != len(antes) {
		t.Fatalf("importó %d, quedaron %d; antes había %d", n, len(despues), len(antes))
	}
	for i := range antes {
		a, d := antes[i], despues[i]
		if !a.UpdatedAt.Equal(d.UpdatedAt) || a.EquipmentID != d.EquipmentID || a.Brand != d.Brand || a.Quantity != d.Quantity {
			t.Errorf("ítem %d cambió:\nantes   %+v\ndespués %+v", a.ID, a, d)
		}
	}
	if movs, _ := repo.GetMovements(ctx); len(movs) != len(movsAntes) {
		t.Error("la importación no debe tocar los movimientos")
	}

	// Fila nueva sin ID al final del archivo.
	f, err := excelize.OpenReader(bytes.NewReader(data))
	must(t, err)
	fila := len(antes) + 2
	must(t, f.SetSheetRow(SheetInventory, fmt.Sprintf("A%d", fila), &[]any{nil, "3001", "Teclado", "Logitech", "K120", "", 1, "Sistemas"}))
	conNueva, err := writeBytes(f)
	must(t, err)
	f.Close()
	_, err = repo.ImportInventory(ctx, bytes.NewReader(conNueva))
	must(t, err)
	items, _ := repo.GetAll(ctx)
	nuevo := items[len(items)-1]
	if nuevo.InventoryNumber != "3001" || nuevo.ID <= 5 {
		t.Errorf("ítem nuevo = %+v; debe recibir un ID mayor a todos los usados", nuevo)
	}

	// Un archivo con errores no cambia nada (transacción completa).
	roto := editarExportado(t, data, func(f *excelize.File) { set(t, f, "B3", "1001") }) // N° repetido
	if _, err := repo.ImportInventory(ctx, bytes.NewReader(roto)); err == nil {
		t.Fatal("debería rechazar el archivo con un N° de inventario repetido")
	}
	if final, _ := repo.GetAll(ctx); len(final) != len(items) {
		t.Errorf("un import fallido cambió el inventario: %d ítems, antes %d", len(final), len(items))
	}
}

// Migra el Excel de prueba completo a la base de prueba vacía.
func TestSQLServer_Migracion(t *testing.T) {
	repo := testSQLRepo(t)
	ctx := context.Background()
	_, err := repo.db.ExecContext(ctx, `DELETE FROM dbo.Movements; UPDATE dbo.Items SET EquipmentID = NULL; DELETE FROM dbo.Items;`)
	must(t, err)

	data, err := newFixtureRepo(t).ReadMigrationData(ctx)
	must(t, err)
	if data.LastItemID != 7 {
		t.Fatalf("último ID del fixture = %d; se esperaba 7 (el 7 se eliminó)", data.LastItemID)
	}
	if problemas := CheckMigrationData(data); len(problemas) > 0 {
		t.Fatalf("problemas inesperados: %v", problemas)
	}
	must(t, repo.LoadMigrationData(ctx, data))

	items, _ := repo.GetAll(ctx)
	movs, _ := repo.GetMovements(ctx)
	if len(items) != len(data.Items) || len(movs) != len(data.Movements) {
		t.Fatalf("migrados %d ítems y %d movimientos; se esperaban %d y %d", len(items), len(movs), len(data.Items), len(data.Movements))
	}
	nuevo, err := repo.Create(ctx, nuevoItem("9999", ""))
	must(t, err)
	if nuevo.ID != 8 {
		t.Errorf("el primer ítem nuevo recibió el ID %d; se esperaba 8 (nunca reutilizar el 7)", nuevo.ID)
	}
	if err := repo.LoadMigrationData(ctx, data); !errors.Is(err, ErrDatabaseNotEmpty) {
		t.Errorf("migrar sobre una base con datos: error = %v; se esperaba ErrDatabaseNotEmpty", err)
	}
}

func TestCheckMigrationData(t *testing.T) {
	data := MigrationData{Items: []models.Item{
		{ID: 1, InventoryNumber: "INV-1", SerialNumber: "ABC"},
		{ID: 2, InventoryNumber: "inv-1", SerialNumber: "ÀBC"},
		{ID: 3, Notes: strings.Repeat("x", 1001), EquipmentID: 99},
	}}
	problemas := CheckMigrationData(data)
	if len(problemas) != 4 {
		t.Errorf("problemas = %d; se esperaban 4 (N° repetido, serie repetida, observación larga, equipo inexistente):\n%s",
			len(problemas), strings.Join(problemas, "\n"))
	}
}

// Un componente comparte el N° de inventario de su gabinete; otro equipo no.
func TestCheckMigrationData_NumeroCompartidoPorEquipo(t *testing.T) {
	gabinete := models.Item{ID: 1, InventoryNumber: "9999", HasInventory: true, DeviceType: "Gabinete", Quantity: 1}
	cpu := models.Item{ID: 2, InventoryNumber: "9999", HasInventory: true, DeviceType: "Procesador", Quantity: 1, EquipmentID: 1}
	ram := models.Item{ID: 3, InventoryNumber: "9999", HasInventory: true, DeviceType: "RAM", Quantity: 1, EquipmentID: 1}
	ajeno := models.Item{ID: 4, InventoryNumber: "9999", HasInventory: true, DeviceType: "Monitor", Quantity: 1}
	if p := CheckMigrationData(MigrationData{Items: []models.Item{gabinete, cpu, ram}}); len(p) != 0 {
		t.Errorf("gabinete y componentes con el mismo número no son un problema: %v", p)
	}
	if p := CheckMigrationData(MigrationData{Items: []models.Item{gabinete, cpu, ajeno}}); len(p) != 1 || !strings.Contains(p[0], "ítem 4") {
		t.Errorf("un ítem de otro equipo con el mismo número sí lo es: %v", p)
	}
}

// En la base, un componente puede guardarse con el número de su gabinete.
func TestSQLServer_ComponenteCompartiendoNumero(t *testing.T) {
	repo := testSQLRepo(t)
	componente := nuevoItem("1001", "") // el ítem 1 (gabinete) tiene el N° 1001
	componente.DeviceType, componente.EquipmentID = "Procesador", 1
	if _, err := repo.Create(context.Background(), componente); err != nil {
		t.Errorf("un componente debe poder compartir el N° de su gabinete: %v", err)
	}
}
