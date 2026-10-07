package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
)

func categoryFixture(id int, name string, loanable bool) models.Category {
	return models.Category{ID: id, Name: name, Active: true, Loanable: loanable}
}

// filaConsumible es una fila de Inventario completa y válida (pasa la validación de integridad).
func filaConsumible(id int) []any {
	return []any{id, "", "NO", "Mouse", "Logitech", "M90", "", 1, "Sistemas", "OPERATIVO", "",
		"2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"}
}

// validInventoryWorkbook arma un libro mínimo con las hojas y encabezados correctos y las filas dadas.
func validInventoryWorkbook(t *testing.T, rows map[int][]any) string {
	return writeWorkbook(t, func(f *excelize.File) {
		must(t, f.SetSheetName(f.GetSheetName(0), SheetInventory))
		_, err := f.NewSheet(SheetMovements)
		must(t, err)
		must(t, writeHeader(f, inventorySchema, nil))
		must(t, writeHeader(f, movementSchema, nil))
		for rowNum, values := range rows {
			must(t, setRow(f, SheetInventory, rowNum, values))
		}
	})
}

func ids(items []models.Item) []int {
	out := []int{}
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func localTime(year int, month time.Month, day, hour, min int) time.Time {
	return time.Date(year, month, day, hour, min, 0, 0, time.Local)
}

// ============================================================
// Lectura del Excel
// ============================================================

func TestOpenWorkbook(t *testing.T) {
	sinMovimientos := writeWorkbook(t, func(f *excelize.File) {
		must(t, f.SetSheetName(f.GetSheetName(0), SheetInventory))
	})
	noEsExcel := filepath.Join(t.TempDir(), "inventario.xlsx")
	must(t, os.WriteFile(noEsExcel, []byte("esto no es un libro"), 0o644))

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{"libro válido", copyFixture(t), nil},
		{"falta la hoja Movimientos", sinMovimientos, ErrSheetMissing},
		{"archivo que no es xlsx", noEsExcel, ErrInvalidWorkbook},
		{"archivo inexistente", filepath.Join(t.TempDir(), "no-existe.xlsx"), ErrInvalidWorkbook},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := openWorkbook(tt.path)
			if f != nil {
				defer f.Close()
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v; se esperaba %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetAll(t *testing.T) {
	items, err := newFixtureRepo(t).GetAll(context.Background())
	must(t, err)

	if got, want := ids(items), []int{1, 2, 3, 4, 5, 6}; !slices.Equal(got, want) {
		t.Fatalf("IDs = %v; se esperaba %v (fila vacía ignorada, orden de la hoja)", got, want)
	}

	t.Run("fila completa con préstamo y fechas RFC 3339", func(t *testing.T) {
		it := items[0]
		loanedAt, _ := time.Parse(time.RFC3339, "2026-09-15T09:30:00-03:00")
		updatedAt, _ := time.Parse(time.RFC3339Nano, "2026-09-15T09:30:00.123456789-03:00")
		want := models.Item{
			ID: 1, InventoryNumber: "1001", HasInventory: true, DeviceType: "Notebook",
			Brand: "Dell", Model: "Latitude 5420", SerialNumber: "SN-987654321", Quantity: 1,
			Location: "Administración", Status: models.StatusOperational,
			Availability: models.Loaned, AssignedTo: "María Gómez",
		}
		if !it.UpdatedAt.Equal(updatedAt) {
			t.Errorf("UpdatedAt = %v; se esperaba %v (nanosegundos incluidos)", it.UpdatedAt, updatedAt)
		}
		if it.LoanedAt == nil || !it.LoanedAt.Equal(loanedAt) {
			t.Errorf("LoanedAt = %v; se esperaba %v", it.LoanedAt, loanedAt)
		}
		it.CreatedAt, it.UpdatedAt, it.LoanedAt = time.Time{}, time.Time{}, nil
		if it != want {
			t.Errorf("item =\n%+v\nse esperaba\n%+v", it, want)
		}
	})

	t.Run("fechas numéricas de Excel y columnas N–P ausentes", func(t *testing.T) {
		it := items[1]
		if !it.HasInventory {
			t.Error(`HasInventory: "si" en minúsculas debería ser true`)
		}
		if !it.CreatedAt.Equal(fixtureSerialDate) || !it.UpdatedAt.Equal(fixtureSerialDate) {
			t.Errorf("fechas = %v / %v; se esperaba %v", it.CreatedAt, it.UpdatedAt, fixtureSerialDate)
		}
		if it.Availability != models.Available || it.AssignedTo != "" || it.LoanedAt != nil {
			t.Errorf("sin columnas de préstamo debería quedar DISPONIBLE; = %q %q %v", it.Availability, it.AssignedTo, it.LoanedAt)
		}
	})

	t.Run("consumible con cantidad numérica y fecha con hora", func(t *testing.T) {
		it := items[2]
		if it.HasInventory || it.Quantity != 12 {
			t.Errorf("HasInventory=%v Quantity=%d; se esperaba false, 12", it.HasInventory, it.Quantity)
		}
		if want := localTime(2026, 9, 2, 8, 15); !it.CreatedAt.Equal(want) {
			t.Errorf("CreatedAt = %v; se esperaba %v", it.CreatedAt, want)
		}
	})

	t.Run("celda booleana real y fechas DD/MM/AAAA", func(t *testing.T) {
		it := items[3]
		if !it.HasInventory {
			t.Error("HasInventory: una celda booleana TRUE debería ser true")
		}
		if want := localTime(2026, 8, 10, 0, 0); !it.CreatedAt.Equal(want) {
			t.Errorf("CreatedAt = %v; se esperaba %v", it.CreatedAt, want)
		}
	})

	// Las fechas vacías se prueban en TestRowToItem ("fila corta"): desde la
	// Etapa 8 un ítem sin fechas no pasa la validación de integridad.
	t.Run("celdas vacías", func(t *testing.T) {
		it := items[5]
		if it.HasInventory || it.Quantity != 0 || it.InventoryNumber != "" || it.SerialNumber != "" {
			t.Errorf("celdas vacías deberían dar valores cero; = %+v", it)
		}
	})
}

func TestGetAll_EncabezadoIncompatible(t *testing.T) {
	path := writeWorkbook(t, func(f *excelize.File) {
		must(t, f.SetSheetName(f.GetSheetName(0), SheetInventory))
		_, err := f.NewSheet(SheetMovements)
		must(t, err)
		header := slices.Clone(inventorySchema.header)
		header[colBrand], header[colModel] = header[colModel], header[colBrand]
		must(t, setRow(f, SheetInventory, 1, toAny(header)))
	})
	// Sin NewExcelRepository: se prueba la validación de lectura, no la de arranque.
	repo := &ExcelRepository{path: path}

	_, err := repo.GetAll(context.Background())
	if !errors.Is(err, ErrInvalidWorkbook) || !strings.Contains(err.Error(), "columna E") {
		t.Fatalf("error = %v; se esperaba ErrInvalidWorkbook señalando la columna E", err)
	}
}

func TestGetAll_CeldaInvalidaIndicaUbicacion(t *testing.T) {
	path := validInventoryWorkbook(t, map[int][]any{
		2: {1, "1001", "SI", "Notebook"},
		// fila 3 vacía: la numeración de filas del error debe seguir siendo la de Excel
		4: {2, "1002", "SI", "Notebook", "Dell", "X", "", "muchos"},
	})
	repo := &ExcelRepository{path: path}

	_, err := repo.GetAll(context.Background())
	var cellErr *CellError
	if !errors.Is(err, ErrInvalidCell) || !errors.As(err, &cellErr) {
		t.Fatalf("error = %v; se esperaba un CellError", err)
	}
	if !strings.Contains(err.Error(), "celda H4") || cellErr.Field != "Quantity" {
		t.Errorf("error = %q; debería señalar la celda H4 (Quantity)", err)
	}
}

// ============================================================
// Conversión de filas
// ============================================================

func TestRowToItem(t *testing.T) {
	t.Run("fila corta: las columnas faltantes toman el valor cero", func(t *testing.T) {
		it, err := rowToItem([]string{"7", "2001", "SI", "Monitor"}, 2)
		must(t, err)
		want := models.Item{ID: 7, InventoryNumber: "2001", HasInventory: true, DeviceType: "Monitor", Availability: models.Available}
		if it != want {
			t.Errorf("item = %+v; se esperaba %+v", it, want)
		}
	})

	t.Run("espacios alrededor de los valores", func(t *testing.T) {
		it, err := rowToItem([]string{" 8 ", "  3002 ", " no ", " Mouse ", " Logitech "}, 2)
		must(t, err)
		if it.ID != 8 || it.InventoryNumber != "3002" || it.HasInventory || it.Brand != "Logitech" {
			t.Errorf("item = %+v", it)
		}
	})

	t.Run("cantidad guardada como decimal entero", func(t *testing.T) {
		it, err := rowToItem([]string{"9", "", "NO", "Mouse", "", "", "", "12.0"}, 2)
		must(t, err)
		if it.Quantity != 12 {
			t.Errorf("Quantity = %d; se esperaba 12", it.Quantity)
		}
	})

	for _, v := range []string{"SI", "si", "Sí", "TRUE", "verdadero", "1", "x"} {
		t.Run("booleano verdadero "+v, func(t *testing.T) {
			it, err := rowToItem([]string{"1", "", v}, 2)
			if err != nil || !it.HasInventory {
				t.Errorf("HasInventory(%q) = %v, %v; se esperaba true", v, it.HasInventory, err)
			}
		})
	}
	for _, v := range []string{"NO", "no", "FALSE", "falso", "0", ""} {
		t.Run("booleano falso "+v, func(t *testing.T) {
			it, err := rowToItem([]string{"1", "", v}, 2)
			if err != nil || it.HasInventory {
				t.Errorf("HasInventory(%q) = %v, %v; se esperaba false", v, it.HasInventory, err)
			}
		})
	}

	invalidas := []struct {
		name  string
		cells []string
		celda string
		campo string
	}{
		{"ID vacío en una fila con datos", []string{"", "1001", "SI"}, "A5", "ID"},
		{"ID no numérico", []string{"uno"}, "A5", "ID"},
		{"booleano no reconocido", []string{"1", "", "tal vez"}, "C5", "HasInventory"},
		{"cantidad con decimales", []string{"1", "", "NO", "", "", "", "", "2.5"}, "H5", "Quantity"},
		{"fecha no reconocida", []string{"1", "", "", "", "", "", "", "", "", "", "", "ayer"}, "L5", "CreatedAt"},
	}
	for _, tt := range invalidas {
		t.Run(tt.name, func(t *testing.T) {
			_, err := rowToItem(tt.cells, 5)
			var cellErr *CellError
			if !errors.As(err, &cellErr) || !errors.Is(err, ErrInvalidCell) {
				t.Fatalf("error = %v; se esperaba un CellError", err)
			}
			if !strings.Contains(err.Error(), "celda "+tt.celda) || cellErr.Field != tt.campo {
				t.Errorf("error = %q; debería señalar %s (%s)", err, tt.celda, tt.campo)
			}
		})
	}
}

// ============================================================
// Búsqueda por ID
// ============================================================

func TestGetByID(t *testing.T) {
	repo := newFixtureRepo(t)
	ctx := context.Background()

	for _, tt := range []struct {
		id    int
		brand string
	}{{1, "Dell"}, {4, "BGH"}, {6, "Genérico"}} {
		it, err := repo.GetByID(ctx, tt.id)
		if err != nil || it.ID != tt.id || it.Brand != tt.brand {
			t.Errorf("GetByID(%d) = %d %q, %v; se esperaba %q", tt.id, it.ID, it.Brand, err, tt.brand)
		}
	}

	for _, id := range []int{99, 0, -3} {
		if _, err := repo.GetByID(ctx, id); !errors.Is(err, ErrItemNotFound) {
			t.Errorf("GetByID(%d) error = %v; se esperaba ErrItemNotFound", id, err)
		}
	}
}

func TestGetByID_NoLeeFilasPosteriores(t *testing.T) {
	path := validInventoryWorkbook(t, map[int][]any{
		2: {1, "1001", "SI", "Notebook", "Dell"},
		3: {2, "1002", "quizás"}, // inválida: GetAll falla, GetByID(1) no debería llegar a leerla
	})
	repo := &ExcelRepository{path: path}
	ctx := context.Background()

	if _, err := repo.GetAll(ctx); !errors.Is(err, ErrInvalidCell) {
		t.Fatalf("GetAll error = %v; se esperaba ErrInvalidCell (verifica el caso de prueba)", err)
	}
	it, err := repo.GetByID(ctx, 1)
	if err != nil || it.Brand != "Dell" {
		t.Errorf("GetByID(1) = %+v, %v; debería detenerse en la fila encontrada", it, err)
	}
}

// ============================================================
// Búsqueda por texto
// ============================================================

func TestSearch(t *testing.T) {
	repo := newFixtureRepo(t)

	tests := []struct {
		name   string
		filter models.ItemFilter
		want   []int
	}{
		{"marca sin distinguir mayúsculas", models.ItemFilter{Text: "dELL"}, []int{1}},
		{"modelo", models.ItemFilter{Text: "thinkpad"}, []int{2}},
		{"número de inventario parcial", models.ItemFilter{Text: "300"}, []int{4}},
		{"número de serie", models.ItemFilter{Text: "sn-1122"}, []int{2}},
		{"tipo de dispositivo", models.ItemFilter{Text: "NOTEBOOK"}, []int{1, 2}},
		{"con acentos", models.ItemFilter{Text: "genérico"}, []int{6}},
		{"espacios alrededor", models.ItemFilter{Text: "  latitude  "}, []int{1}},
		{"la ubicación no es campo de texto", models.ItemFilter{Text: "Recepción"}, []int{}},
		{"sin coincidencias", models.ItemFilter{Text: "zzz"}, []int{}},
		{"las bajas se excluyen por defecto", models.ItemFilter{Text: "laserjet"}, []int{}},
		{"las bajas se incluyen si se pide", models.ItemFilter{Text: "VNB3K", IncludeRetired: true}, []int{5}},
		{"texto vacío devuelve los activos", models.ItemFilter{}, []int{1, 2, 3, 4, 6}},
		{"filtro por tipo", models.ItemFilter{DeviceType: "aire acondicionado"}, []int{4}},
		{"filtro por ubicación", models.ItemFilter{Location: "sistemas"}, []int{2, 3, 6}},
		{"solo consumibles", models.ItemFilter{OnlyConsumables: true}, []int{3, 6}},
		{"prestados", models.ItemFilter{Availability: models.Loaned}, []int{1}},
		{"texto y filtro combinados", models.ItemFilter{Text: "notebook", Location: "Sistemas"}, []int{2}},

		// Etapa 9: filtros por campo, sin mayúsculas, tildes ni espacios de más.
		{"texto sin tildes", models.ItemFilter{Text: "GENERICO"}, []int{6}},
		{"ubicación sin tildes", models.ItemFilter{Location: "administracion"}, []int{1, 4}},
		{"tipo con espacios de más", models.ItemFilter{DeviceType: "  aire   acondicionado "}, []int{4}},
		{"el tipo es exacto, no parcial", models.ItemFilter{DeviceType: "aire"}, []int{}},
		{"marca parcial", models.ItemFilter{Brand: "len"}, []int{2}},
		{"modelo parcial", models.ItemFilter{Model: "LATITUDE"}, []int{1}},
		{"número de inventario parcial", models.ItemFilter{InventoryNumber: "100"}, []int{1, 2}},
		{"número de serie parcial", models.ItemFilter{SerialNumber: "sn-"}, []int{1, 2}},
		{"con número de inventario", models.ItemFilter{HasInventory: ptr(true)}, []int{1, 2, 4}},
		{"sin número de inventario", models.ItemFilter{HasInventory: ptr(false)}, []int{3, 6}},
		{"por estado", models.ItemFilter{Status: models.StatusOperational}, []int{1, 2, 3, 6}},
		{"filtrar por BAJA muestra las bajas", models.ItemFilter{Status: models.StatusRetired}, []int{5}},
		{"excluir un tipo", models.ItemFilter{ExcludeDeviceType: "AIRE ACONDICIONADO"}, []int{1, 2, 3, 6}},
		{"disponibles", models.ItemFilter{Availability: models.Available, HasInventory: ptr(true)}, []int{2, 4}},
		{"varios filtros combinados", models.ItemFilter{Brand: "dell", Location: "Administración",
			Status: models.StatusOperational, HasInventory: ptr(true)}, []int{1}},
		{"filtros que se contradicen", models.ItemFilter{Brand: "dell", Location: "Sistemas"}, []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := repo.Search(context.Background(), tt.filter)
			must(t, err)
			if got := ids(items); !slices.Equal(got, tt.want) {
				t.Errorf("Search(%+v) = %v; se esperaba %v", tt.filter, got, tt.want)
			}
		})
	}
}

func toAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

func ptr[T any](v T) *T { return &v }
