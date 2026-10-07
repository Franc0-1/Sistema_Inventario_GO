package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
)

// writeTime es la hora del reloj fijo de los tests de escritura: posterior a
// todas las fechas del fixture.
var writeTime = time.Date(2026, 9, 28, 15, 30, 0, 123456789, time.Local)

// newWriteRepo abre una copia temporal del fixture con el reloj fijo en writeTime.
func newWriteRepo(t *testing.T) (*ExcelRepository, string) {
	t.Helper()
	path := copyFixture(t)
	repo, err := NewExcelRepository(path)
	must(t, err)
	repo.now = func() time.Time { return writeTime }
	return repo, path
}

// sheetRows lee una hoja completa en valores crudos, directo del archivo.
func sheetRows(t *testing.T, path, sheet string) [][]string {
	t.Helper()
	f, err := openWorkbook(path)
	must(t, err)
	defer f.Close()
	rows, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	must(t, err)
	return rows
}

func cellValue(t *testing.T, path, sheet, cell string) string {
	t.Helper()
	f, err := openWorkbook(path)
	must(t, err)
	defer f.Close()
	v, err := f.GetCellValue(sheet, cell, excelize.Options{RawCellValue: true})
	must(t, err)
	return v
}

// sameItem compara dos ítems usando Equal para las fechas (misma hora aunque
// la zona horaria de lectura sea otra).
func sameItem(a, b models.Item) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) || !a.UpdatedAt.Equal(b.UpdatedAt) {
		return false
	}
	if (a.LoanedAt == nil) != (b.LoanedAt == nil) || (a.LoanedAt != nil && !a.LoanedAt.Equal(*b.LoanedAt)) {
		return false
	}
	a.CreatedAt, a.UpdatedAt, a.LoanedAt = time.Time{}, time.Time{}, nil
	b.CreatedAt, b.UpdatedAt, b.LoanedAt = time.Time{}, time.Time{}, nil
	return a == b
}

func mustGet(t *testing.T, repo *ExcelRepository, id int) models.Item {
	t.Helper()
	item, err := repo.GetByID(context.Background(), id)
	must(t, err)
	return item
}

// ============================================================
// Create
// ============================================================

func TestCreate(t *testing.T) {
	repo, path := newWriteRepo(t)
	ctx := context.Background()
	movimientosAntes := sheetRows(t, path, SheetMovements)
	categoriasAntes := sheetRows(t, path, SheetCategories)

	input := models.Item{
		ID: 999, CreatedAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), // deben ignorarse
		InventoryNumber: "2050", HasInventory: true, DeviceType: "Monitor", Brand: "LG",
		Model: "24MK430H", SerialNumber: "LG-24-0001", Quantity: 1, Location: "Dirección",
		Status: models.StatusOperational, Notes: "Nuevo", Availability: models.Available,
	}
	created, err := repo.Create(ctx, input)
	must(t, err)

	t.Run("genera el ID siguiente al último asignado", func(t *testing.T) {
		if created.ID != 8 { // el fixture ya asignó el 7 (a un ítem luego eliminado)
			t.Errorf("ID = %d; se esperaba 8", created.ID)
		}
	})
	t.Run("genera CreatedAt y UpdatedAt", func(t *testing.T) {
		if !created.CreatedAt.Equal(writeTime) || !created.UpdatedAt.Equal(writeTime) {
			t.Errorf("fechas = %v / %v; se esperaba %v", created.CreatedAt, created.UpdatedAt, writeTime)
		}
	})
	t.Run("conserva los datos proporcionados", func(t *testing.T) {
		want := input
		want.ID, want.CreatedAt, want.UpdatedAt = 8, writeTime, writeTime
		if !sameItem(created, want) {
			t.Errorf("creado =\n%+v\nse esperaba\n%+v", created, want)
		}
	})
	t.Run("la lectura de la Etapa 2 lo devuelve igual", func(t *testing.T) {
		if got := mustGet(t, repo, 8); !sameItem(got, created) {
			t.Errorf("GetByID(8) =\n%+v\nse esperaba\n%+v", got, created)
		}
	})
	t.Run("se agrega al final de los datos", func(t *testing.T) {
		if v := cellValue(t, path, SheetInventory, "A9"); v != "8" { // la última fila con datos era la 8
			t.Errorf("A9 = %q; se esperaba 8", v)
		}
	})
	t.Run("no modifica otras hojas", func(t *testing.T) {
		if !reflect.DeepEqual(sheetRows(t, path, SheetMovements), movimientosAntes) ||
			!reflect.DeepEqual(sheetRows(t, path, SheetCategories), categoriasAntes) {
			t.Error("las hojas Movimientos o Categorias cambiaron")
		}
	})
}

func TestCreate_IDsConsecutivos(t *testing.T) {
	repo, _ := newWriteRepo(t)
	ctx := context.Background()
	for _, want := range []int{8, 9, 10} {
		item, err := repo.Create(ctx, models.Item{DeviceType: "Mouse", Quantity: 3})
		must(t, err)
		if item.ID != want {
			t.Errorf("ID = %d; se esperaba %d", item.ID, want)
		}
	}
}

func TestCreate_ConHuecosUsaElMayorMasUno(t *testing.T) {
	path := validInventoryWorkbook(t, map[int][]any{
		2: filaConsumible(1), 3: filaConsumible(2), 4: filaConsumible(3), 5: filaConsumible(5),
	})
	repo, err := NewExcelRepository(path)
	must(t, err)

	item, err := repo.Create(context.Background(), models.Item{DeviceType: "Teclado"})
	must(t, err)
	if item.ID != 6 {
		t.Errorf("ID = %d; se esperaba 6 (IDs existentes 1, 2, 3, 5)", item.ID)
	}
}

func TestCreate_LibroNuevo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nuevo.xlsx")
	repo, err := NewExcelRepository(path)
	must(t, err)

	item, err := repo.Create(context.Background(), models.Item{DeviceType: "Notebook", HasInventory: true, InventoryNumber: "1", Quantity: 1})
	must(t, err)
	if item.ID != 1 || cellValue(t, path, SheetInventory, "A2") != "1" {
		t.Errorf("en un libro vacío el primer ítem debería tener ID 1 en la fila 2; ID = %d", item.ID)
	}
}

// ============================================================
// Update
// ============================================================

func TestUpdate(t *testing.T) {
	repo, path := newWriteRepo(t)
	ctx := context.Background()
	before := mustGet(t, repo, 3)

	changed := before
	changed.Brand, changed.Model, changed.Quantity = "Genius", "DX-110", 20
	changed.Location, changed.Notes = "Depósito", "Reubicado"
	changed.CreatedAt = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC) // debe ignorarse

	updated, err := repo.Update(ctx, changed, before.UpdatedAt, nil)
	must(t, err)

	t.Run("conserva ID y CreatedAt", func(t *testing.T) {
		if updated.ID != 3 || !updated.CreatedAt.Equal(before.CreatedAt) {
			t.Errorf("ID=%d CreatedAt=%v; se esperaba 3 y %v", updated.ID, updated.CreatedAt, before.CreatedAt)
		}
	})
	t.Run("modifica UpdatedAt", func(t *testing.T) {
		if !updated.UpdatedAt.Equal(writeTime) {
			t.Errorf("UpdatedAt = %v; se esperaba %v", updated.UpdatedAt, writeTime)
		}
	})
	t.Run("guarda los campos modificados", func(t *testing.T) {
		want := changed
		want.CreatedAt, want.UpdatedAt = before.CreatedAt, writeTime
		if got := mustGet(t, repo, 3); !sameItem(got, want) {
			t.Errorf("GetByID(3) =\n%+v\nse esperaba\n%+v", got, want)
		}
	})
	t.Run("escribe en la misma fila sin agregar filas", func(t *testing.T) {
		if a, e := cellValue(t, path, SheetInventory, "A4"), cellValue(t, path, SheetInventory, "E4"); a != "3" || e != "Genius" {
			t.Errorf("fila 4 = ID %q, Brand %q; se esperaba 3, Genius", a, e)
		}
		items, err := repo.GetAll(ctx)
		must(t, err)
		if len(items) != 6 {
			t.Errorf("GetAll devolvió %d ítems; se esperaban 6", len(items))
		}
	})
}

func TestUpdate_NoExiste(t *testing.T) {
	repo, _ := newWriteRepo(t)
	_, err := repo.Update(context.Background(), models.Item{ID: 99, DeviceType: "Mouse"}, time.Time{}, nil)
	if !errors.Is(err, ErrItemNotFound) {
		t.Errorf("error = %v; se esperaba ErrItemNotFound", err)
	}
}

func TestUpdate_VersionDesactualizada(t *testing.T) {
	repo, _ := newWriteRepo(t)
	ctx := context.Background()
	leido := mustGet(t, repo, 1)

	primero := leido
	primero.Notes = "primer cambio"
	_, err := repo.Update(ctx, primero, leido.UpdatedAt, nil)
	must(t, err)

	segundo := leido
	segundo.Notes = "cambio sobre una versión vieja"
	if _, err := repo.Update(ctx, segundo, leido.UpdatedAt, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("error = %v; se esperaba ErrConflict", err)
	}
	if got := mustGet(t, repo, 1); got.Notes != "primer cambio" {
		t.Errorf("Notes = %q; el primer cambio no debería perderse", got.Notes)
	}
}

// ============================================================
// Delete
// ============================================================

func TestDelete(t *testing.T) {
	repo, _ := newWriteRepo(t)
	ctx := context.Background()
	before, err := repo.GetAll(ctx)
	must(t, err)

	must(t, repo.Delete(ctx, 2, mustGet(t, repo, 2).UpdatedAt))

	if _, err := repo.GetByID(ctx, 2); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("GetByID(2) error = %v; se esperaba ErrItemNotFound", err)
	}
	after, err := repo.GetAll(ctx)
	must(t, err)
	if got, want := ids(after), []int{1, 3, 4, 5, 6}; !slices.Equal(got, want) {
		t.Fatalf("IDs = %v; se esperaba %v", got, want)
	}
	for i, item := range slices.DeleteFunc(before, func(it models.Item) bool { return it.ID == 2 }) {
		if !sameItem(after[i], item) {
			t.Errorf("el ítem %d cambió al borrar otro:\n%+v\n%+v", item.ID, after[i], item)
		}
	}
}

func TestDelete_NoExiste(t *testing.T) {
	repo, _ := newWriteRepo(t)
	if err := repo.Delete(context.Background(), 99, time.Time{}); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("error = %v; se esperaba ErrItemNotFound", err)
	}
}

func TestDelete_NoReutilizaID(t *testing.T) {
	repo, path := newWriteRepo(t)
	ctx := context.Background()
	crear := func() int {
		item, err := repo.Create(ctx, models.Item{DeviceType: "Mouse"})
		must(t, err)
		return item.ID
	}

	// El fixture ya asignó el 7 (ítem eliminado): el próximo es el 8.
	// Se borra el mayor ID existente: "mayor existente + 1" lo reutilizaría.
	if id := crear(); id != 8 {
		t.Fatalf("primer alta: ID = %d; se esperaba 8", id)
	}
	must(t, repo.Delete(ctx, 8, mustGet(t, repo, 8).UpdatedAt))
	if id := crear(); id != 9 {
		t.Errorf("después de borrar el 8, ID = %d; se esperaba 9", id)
	}

	// El último ID asignado persiste en el archivo, no en memoria.
	must(t, repo.Delete(ctx, 9, mustGet(t, repo, 9).UpdatedAt))
	reabierto, err := NewExcelRepository(path)
	must(t, err)
	item, err := reabierto.Create(ctx, models.Item{DeviceType: "Mouse"})
	must(t, err)
	if item.ID != 10 {
		t.Errorf("con el libro reabierto, ID = %d; se esperaba 10", item.ID)
	}
}

// ============================================================
// UpdateStock
// ============================================================

func TestUpdateStock(t *testing.T) {
	repo, path := newWriteRepo(t)
	ctx := context.Background()
	before := mustGet(t, repo, 3)
	filaAntes := sheetRows(t, path, SheetInventory)[3] // fila 4 de Excel

	updated, err := repo.UpdateStock(ctx, 3, 5, before.UpdatedAt, stockMovement(models.MovementStockOut, 7))
	must(t, err)

	if updated.Quantity != 5 || !updated.UpdatedAt.Equal(writeTime) {
		t.Errorf("Quantity=%d UpdatedAt=%v; se esperaba 5 y %v", updated.Quantity, updated.UpdatedAt, writeTime)
	}
	want := before
	want.Quantity, want.UpdatedAt = 5, writeTime
	if got := mustGet(t, repo, 3); !sameItem(got, want) {
		t.Errorf("GetByID(3) =\n%+v\nse esperaba\n%+v", got, want)
	}

	// A nivel de celdas: solo cambian H (Quantity) y M (UpdatedAt).
	filaDespues := sheetRows(t, path, SheetInventory)[3]
	for col := range max(len(filaAntes), len(filaDespues)) {
		if col == colQuantity || col == colUpdatedAt {
			continue
		}
		if cellAt(filaAntes, col) != cellAt(filaDespues, col) {
			name, _ := excelize.ColumnNumberToName(col + 1)
			t.Errorf("la columna %s cambió: %q -> %q", name, cellAt(filaAntes, col), cellAt(filaDespues, col))
		}
	}
}

func TestUpdateStock_NoExiste(t *testing.T) {
	repo, _ := newWriteRepo(t)
	if _, err := repo.UpdateStock(context.Background(), 99, 1, time.Time{}, stockMovement(models.MovementStockIn, 1)); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("error = %v; se esperaba ErrItemNotFound", err)
	}
}

func TestUpdateStock_CantidadNegativa(t *testing.T) {
	repo, _ := newWriteRepo(t)
	before := mustGet(t, repo, 3)
	if _, err := repo.UpdateStock(context.Background(), 3, -1, before.UpdatedAt, stockMovement(models.MovementStockOut, 13)); !errors.Is(err, ErrInvalidItem) {
		t.Errorf("error = %v; se esperaba ErrInvalidItem", err)
	}
	if got := mustGet(t, repo, 3); got.Quantity != before.Quantity {
		t.Errorf("Quantity = %d; no debería haber cambiado", got.Quantity)
	}
}

// ============================================================
// Concurrencia
// ============================================================

// TestEscriturasConcurrentes lanza altas, lecturas y ajustes de stock en
// paralelo. Al final el libro debe abrirse, tener todos los ítems con IDs
// únicos y ningún ajuste de stock perdido.
func TestEscriturasConcurrentes(t *testing.T) {
	path := copyFixture(t)
	repo, err := NewExcelRepository(path) // reloj real
	must(t, err)
	ctx := context.Background()
	const n = 15

	var wg sync.WaitGroup
	errs := make(chan error, 3*n)
	for i := range n {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, err := repo.Create(ctx, models.Item{DeviceType: "Mouse", Brand: fmt.Sprintf("Concurrente %d", i), Quantity: i})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := repo.Search(ctx, models.ItemFilter{Text: "concurrente"})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			errs <- incrementStock(ctx, repo, 3)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	// Se relee con una instancia nueva: valida el archivo, no el estado en memoria.
	fresh, err := NewExcelRepository(path)
	must(t, err)
	items, err := fresh.GetAll(ctx)
	must(t, err)

	got := ids(items)
	slices.Sort(got)
	want := []int{1, 2, 3, 4, 5, 6} // los del fixture; el 7 ya fue asignado (ítem eliminado)
	for id := 8; id < 8+n; id++ {
		want = append(want, id)
	}
	if !slices.Equal(got, want) {
		t.Errorf("IDs = %v; se esperaba %v sin repetidos", got, want)
	}
	if stock := mustGet(t, fresh, 3).Quantity; stock != 12+n {
		t.Errorf("Quantity del ítem 3 = %d; se esperaba %d (ningún ajuste perdido)", stock, 12+n)
	}

	// El historial debe acompañar al stock: un movimiento por ajuste y una
	// cadena sin huecos (cada movimiento parte de donde terminó el anterior).
	movs := mustMovements(t, fresh, 3)
	if len(movs) != 1+n {
		t.Fatalf("el ítem 3 tiene %d movimientos; se esperaban %d", len(movs), 1+n)
	}
	for i := 1; i < len(movs); i++ {
		if movs[i].PreviousQuantity != movs[i-1].NewQuantity {
			t.Errorf("historial inconsistente en el movimiento %d: parte de %d y el anterior terminó en %d",
				movs[i].ID, movs[i].PreviousQuantity, movs[i-1].NewQuantity)
		}
	}
	if ultimo := movs[len(movs)-1]; ultimo.NewQuantity != 12+n {
		t.Errorf("el último movimiento termina en %d; el Inventario dice %d", ultimo.NewQuantity, 12+n)
	}

	// Etapa 8: el archivo final pasa la validación completa (sin IDs duplicados
	// ni stock negativo), no quedan temporales y hay un respaldo por cada
	// escritura exitosa (los reintentos por conflicto no respaldan).
	if err := validateFile(path, validateWorkbook); err != nil {
		t.Errorf("el Excel final no es válido: %v", err)
	}
	for _, it := range items {
		if it.Quantity < 0 {
			t.Errorf("el ítem %d quedó con stock negativo (%d)", it.ID, it.Quantity)
		}
	}
	if tmp := temporales(t, path); len(tmp) > 0 {
		t.Errorf("quedaron temporales: %v", tmp)
	}
	if got := len(backups(t, repo)); got != 2*n {
		t.Errorf("hay %d respaldos; se esperaban %d (uno por escritura exitosa)", got, 2*n)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Error("quedó un archivo temporal sin renombrar")
	}
}

// incrementStock suma 1 con concurrencia optimista: si otro escribió en el
// medio, relee y reintenta (lo que hará la capa de servicios).
func incrementStock(ctx context.Context, repo *ExcelRepository, id int) error {
	for {
		item, err := repo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		_, err = repo.UpdateStock(ctx, id, item.Quantity+1, item.UpdatedAt, stockMovement(models.MovementStockIn, 1))
		if !errors.Is(err, ErrConflict) {
			return err
		}
	}
}
