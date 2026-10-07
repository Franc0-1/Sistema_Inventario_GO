package repository

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
)

func importItem(id int, inv, serial string) models.Item {
	return models.Item{
		ID:              id,
		InventoryNumber: inv,
		HasInventory:    inv != "",
		DeviceType:      "Notebook",
		Brand:           "Lenovo",
		Model:           "T14",
		SerialNumber:    serial,
		Quantity:        1,
		Location:        "Sistemas",
		Status:          models.StatusOperational,
		CreatedAt:       time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
		Availability:    models.Available,
	}
}

func importWorkbook(t *testing.T, items ...models.Item) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	must(t, f.SetSheetName(f.GetSheetName(0), SheetInventory))
	must(t, writeHeader(f, inventorySchema, nil))
	for i, item := range items {
		must(t, setRow(f, SheetInventory, headerRow+1+i, itemToRow(item)))
	}
	var buf bytes.Buffer
	_, err := f.WriteTo(&buf)
	must(t, err)
	return buf.Bytes()
}

func importWorkbookWithChange(t *testing.T, change func(*excelize.File)) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	must(t, f.SetSheetName(f.GetSheetName(0), SheetInventory))
	must(t, writeHeader(f, inventorySchema, nil))
	must(t, setRow(f, SheetInventory, 2, itemToRow(importItem(1, "IMP-001", "SER-001"))))
	change(f)
	var buf bytes.Buffer
	_, err := f.WriteTo(&buf)
	must(t, err)
	return buf.Bytes()
}

func TestExportInventory_Valido(t *testing.T) {
	repo := newFixtureRepo(t)
	antes := leerBytes(t, repo.path)
	data, err := repo.ExportInventory(context.Background())
	must(t, err)
	assertIntacto(t, repo.path, antes)
	parsed, err := readImportWorkbook(bytes.NewReader(data))
	must(t, err)
	if !parsed.isSpanish {
		t.Fatal("la exportación debería estar en el formato en español")
	}
	if got, want := len(parsed.spanish), 6; got != want {
		t.Fatalf("exportó %d ítems; se esperaban %d", got, want)
	}
	if it := parsed.spanish[0].item; it.InventoryNumber != "1001" || it.AssignedTo == "" || it.Availability != models.Loaned {
		t.Errorf("la exportación no conservó los datos del ítem: %+v", it)
	}
}

func TestImportInventory_ValidoReemplazaYConservaMovimientos(t *testing.T) {
	repo, _ := newWriteRepo(t)
	movsAntes, err := repo.GetMovements(context.Background())
	must(t, err)

	data := importWorkbook(t,
		importItem(10, "IMP-010", "SER-010"),
		models.Item{ID: 11, HasInventory: false, DeviceType: "Mouse", Brand: "Logitech", Model: "M90", Quantity: 25,
			Location: "Sistemas", Status: models.StatusOperational, CreatedAt: importItem(10, "", "").CreatedAt,
			UpdatedAt: importItem(10, "", "").UpdatedAt, Availability: models.Available},
	)
	imported, err := repo.ImportInventory(context.Background(), bytes.NewReader(data))
	must(t, err)
	if imported != 2 {
		t.Fatalf("imported = %d; se esperaba 2", imported)
	}
	items, err := repo.GetAll(context.Background())
	must(t, err)
	if got := ids(items); !reflect.DeepEqual(got, []int{10, 11}) {
		t.Fatalf("IDs importados = %v; se esperaba reemplazo completo por [10 11]", got)
	}
	movsDespues, err := repo.GetMovements(context.Background())
	must(t, err)
	if !reflect.DeepEqual(movsDespues, movsAntes) {
		t.Error("la importación no debe importar ni borrar movimientos históricos")
	}
}

func TestImportInventory_Validaciones(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want error
		text string
	}{
		{"archivo corrupto", []byte("no es un xlsx"), ErrInvalidWorkbook, ""},
		{"hoja faltante", importWorkbookWithChange(t, func(f *excelize.File) {
			must(t, f.SetSheetName(SheetInventory, "OtraHoja"))
		}), ErrSheetMissing, ""},
		{"headers inválidos", importWorkbookWithChange(t, func(f *excelize.File) {
			must(t, f.SetCellValue(SheetInventory, "B1", "Numero"))
		}), ErrInvalidWorkbook, "columna B"},
		{"fila inválida", importWorkbookWithChange(t, func(f *excelize.File) {
			must(t, f.SetCellValue(SheetInventory, "C2", "quizas"))
		}), ErrInvalidCell, "celda C2"},
		{"cantidad negativa", importWorkbookWithChange(t, func(f *excelize.File) {
			must(t, f.SetCellValue(SheetInventory, "H2", -1))
		}), ErrInvalidCell, "celda H2"},
		{"inventory number duplicado", importWorkbook(t,
			importItem(1, "DUP-1", "SER-1"), importItem(2, "dup-1", "SER-2")), ErrInvalidCell, "duplicado"},
		{"serial duplicado", importWorkbook(t,
			importItem(1, "INV-1", "SER-DUP"), importItem(2, "INV-2", "ser-dup")), ErrInvalidCell, "duplicado"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, path := newWriteRepo(t)
			antes := leerBytes(t, path)
			_, err := repo.ImportInventory(context.Background(), bytes.NewReader(tt.data))
			if !errors.Is(err, tt.want) || (tt.text != "" && !strings.Contains(err.Error(), tt.text)) {
				t.Fatalf("error = %v; se esperaba %v con %q", err, tt.want, tt.text)
			}
			assertIntacto(t, path, antes)
		})
	}
}
