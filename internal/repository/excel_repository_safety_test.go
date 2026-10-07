package repository

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
)

// ---- Helpers ----

// backups lista los respaldos creados (nombres ordenados); nil si no hay carpeta.
func backups(t *testing.T, repo *ExcelRepository) []string {
	t.Helper()
	entries, err := os.ReadDir(repo.backupDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	must(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// temporales lista los temporales que quedaron junto al Excel.
func temporales(t *testing.T, path string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp"))
	must(t, err)
	return matches
}

func leerBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	must(t, err)
	return b
}

// modificarLibro altera un libro de prueba con excelize, sin pasar por el repositorio.
func modificarLibro(t *testing.T, path string, cambio func(f *excelize.File)) {
	t.Helper()
	f, err := excelize.OpenFile(path)
	must(t, err)
	cambio(f)
	must(t, f.Save())
	must(t, f.Close())
}

// assertIntacto verifica que el original no cambió (byte a byte) y no quedaron temporales.
func assertIntacto(t *testing.T, path string, antes []byte) {
	t.Helper()
	if !bytes.Equal(leerBytes(t, path), antes) {
		t.Error("el Excel original cambió")
	}
	if tmp := temporales(t, path); len(tmp) > 0 {
		t.Errorf("quedaron temporales: %v", tmp)
	}
}

// assertValido verifica que el Excel pasa la validación completa.
func assertValido(t *testing.T, path string) {
	t.Helper()
	if err := validateFile(path, validateWorkbook); err != nil {
		t.Errorf("el Excel no es válido: %v", err)
	}
}

func subirStock(t *testing.T, repo *ExcelRepository, id, cantidad int) error {
	t.Helper()
	antes := mustGet(t, repo, id)
	mov := stockMovement(models.MovementStockIn, cantidad-antes.Quantity)
	if cantidad < antes.Quantity {
		mov = stockMovement(models.MovementStockOut, antes.Quantity-cantidad)
	}
	_, err := repo.UpdateStock(context.Background(), id, cantidad, antes.UpdatedAt, mov)
	return err
}

// ============================================================
// Backup
// ============================================================

func TestBackup_ConservaElEstadoAnterior(t *testing.T) {
	repo, path := newWriteRepo(t)
	antesBytes := leerBytes(t, path)
	antesFilas := sheetRows(t, path, SheetInventory)

	must(t, subirStock(t, repo, 3, 20))

	nombres := backups(t, repo)
	if want := []string{"inventario_2026-09-28_15-30-00.xlsx"}; !slices.Equal(nombres, want) {
		t.Fatalf("respaldos = %v; se esperaba %v", nombres, want)
	}
	respaldo := filepath.Join(repo.backupDir, nombres[0])
	if !bytes.Equal(leerBytes(t, respaldo), antesBytes) {
		t.Error("el respaldo no es una copia exacta del archivo anterior")
	}
	if !reflect.DeepEqual(sheetRows(t, respaldo, SheetInventory), antesFilas) {
		t.Error("el respaldo no conserva el estado anterior")
	}
	if got := mustGet(t, repo, 3).Quantity; got != 20 {
		t.Errorf("el Excel actualizado tiene Quantity %d; se esperaba 20", got)
	}
	if err := validateFile(path, validateWorkbook); err != nil {
		t.Errorf("el Excel actualizado no es válido: %v", err)
	}
}

func TestBackup_NoSeSobrescriben(t *testing.T) {
	repo, _ := newWriteRepo(t) // reloj fijo: las tres escrituras caen en el mismo segundo
	for _, cantidad := range []int{13, 14, 15} {
		must(t, subirStock(t, repo, 3, cantidad))
	}
	want := []string{
		"inventario_2026-09-28_15-30-00.xlsx",
		"inventario_2026-09-28_15-30-00_2.xlsx",
		"inventario_2026-09-28_15-30-00_3.xlsx",
	}
	if got := backups(t, repo); !slices.Equal(got, want) {
		t.Errorf("respaldos = %v; se esperaba %v", got, want)
	}
}

func TestBackup_SoloEnEscriturasExitosas(t *testing.T) {
	repo, _ := newWriteRepo(t)
	ctx := context.Background()

	_, _ = repo.GetAll(ctx)
	_, _ = repo.GetByID(ctx, 1)
	_, _ = repo.Search(ctx, models.ItemFilter{Text: "dell"})
	_, _ = repo.GetMovements(ctx)
	_, _ = repo.GetMovementsByItemID(ctx, 3)
	_ = repo.Check(ctx)
	// Escrituras rechazadas antes de tocar el disco: tampoco generan respaldo.
	_, _ = repo.Update(ctx, models.Item{ID: 99, DeviceType: "Mouse"}, mustGet(t, repo, 1).UpdatedAt, nil)
	_, _ = repo.UpdateStock(ctx, 3, 50, mustGet(t, repo, 1).UpdatedAt, stockMovement(models.MovementStockIn, 38)) // versión de otro ítem

	if got := backups(t, repo); got != nil {
		t.Errorf("no debería haber respaldos: %v", got)
	}
}

func TestBackup_FallidoCancelaLaEscritura(t *testing.T) {
	repo, path := newWriteRepo(t)
	antes := leerBytes(t, path)
	// Un archivo donde debería ir la carpeta de respaldos: MkdirAll falla.
	must(t, os.WriteFile(repo.backupDir, []byte("no soy una carpeta"), 0o644))

	_, err := repo.Create(context.Background(), models.Item{DeviceType: "Mouse", Brand: "X", Quantity: 1})
	if !errors.Is(err, ErrBackupFailed) {
		t.Fatalf("error = %v; se esperaba ErrBackupFailed", err)
	}
	assertIntacto(t, path, antes)
	assertValido(t, path)
}

// ============================================================
// Escritura a través de un temporal
// ============================================================

func TestEscritura_ElTemporalSeValidaAntesDeReemplazar(t *testing.T) {
	repo, path := newWriteRepo(t)
	antes := leerBytes(t, path)
	var visto string

	repo.replaceFile = func(src, dst string) error {
		visto = src
		if filepath.Dir(src) != filepath.Dir(path) {
			t.Errorf("el temporal %s no está junto al original", src)
		}
		if !bytes.Equal(leerBytes(t, dst), antes) {
			t.Error("el original cambió antes del reemplazo")
		}
		// El temporal se puede abrir, es válido y ya tiene el cambio.
		f, err := excelize.OpenFile(src)
		must(t, err)
		defer f.Close()
		if err := validateWorkbook(f); err != nil {
			t.Errorf("el temporal no es válido: %v", err)
		}
		if _, item, err := findItemRow(f, 3); err != nil || item.Quantity != 25 {
			t.Errorf("el temporal no tiene el cambio: %+v, %v", item, err)
		}
		return os.Rename(src, dst)
	}

	must(t, subirStock(t, repo, 3, 25))

	if visto == "" || !strings.HasPrefix(filepath.Base(visto), ".inventario.xlsx.") {
		t.Errorf("temporal = %q; se esperaba un nombre único junto al original", visto)
	}
	if tmp := temporales(t, path); len(tmp) > 0 {
		t.Errorf("quedaron temporales: %v", tmp)
	}
	if got := mustGet(t, repo, 3).Quantity; got != 25 {
		t.Errorf("Quantity = %d; se esperaba 25", got)
	}
}

func TestEscritura_TemporalesUnicos(t *testing.T) {
	repo, _ := newWriteRepo(t)
	var nombres []string
	repo.replaceFile = func(src, dst string) error {
		nombres = append(nombres, src)
		return os.Rename(src, dst)
	}
	must(t, subirStock(t, repo, 3, 13))
	must(t, subirStock(t, repo, 3, 14))
	if len(nombres) != 2 || nombres[0] == nombres[1] {
		t.Errorf("temporales = %v; se esperaban dos nombres distintos", nombres)
	}
}

func TestEscritura_TemporalInvalidoNoReemplaza(t *testing.T) {
	repo, path := newWriteRepo(t)
	antes := leerBytes(t, path)

	// Una modificación que deja datos inválidos en memoria: la validación del
	// temporal debe detenerla antes de reemplazar el original.
	err := repo.write(context.Background(), func(f *excelize.File) error {
		return setInventoryCell(f, 4, colQuantity, -5)
	})
	if !errors.Is(err, ErrSaveFailed) || !errors.Is(err, ErrInvalidCell) {
		t.Fatalf("error = %v; se esperaba ErrSaveFailed por un temporal inválido", err)
	}
	assertIntacto(t, path, antes)
	assertValido(t, path)
}

// Integridad: un fallo durante la escritura (al reemplazar) deja el Excel en
// su estado anterior, sin stock ni movimiento a medias, y no reporta éxito.
func TestEscritura_FalloAlReemplazarConservaElEstadoAnterior(t *testing.T) {
	repo, path := newWriteRepo(t)
	antes := leerBytes(t, path)
	movimientosAntes := mustMovements(t, repo, 3)
	repo.replaceFile = func(string, string) error { return errors.New("el archivo está abierto en otro programa") }

	err := subirStock(t, repo, 3, 40)
	if !errors.Is(err, ErrReplaceFailed) {
		t.Fatalf("error = %v; se esperaba ErrReplaceFailed", err)
	}
	assertIntacto(t, path, antes)
	assertValido(t, path)
	if got := mustGet(t, repo, 3).Quantity; got != 12 {
		t.Errorf("Quantity = %d; debería seguir en 12", got)
	}
	if got := mustMovements(t, repo, 3); !reflect.DeepEqual(got, movimientosAntes) {
		t.Error("no debería haberse registrado el movimiento")
	}
}

func TestEscritura_RechazaUnLibroConDatosInvalidos(t *testing.T) {
	repo, path := newWriteRepo(t)
	modificarLibro(t, path, func(f *excelize.File) {
		must(t, f.SetCellInt(SheetInventory, "A3", 1)) // ID duplicado
	})
	antes := leerBytes(t, path)

	_, err := repo.Create(context.Background(), models.Item{DeviceType: "Mouse", Quantity: 1})
	if !errors.Is(err, ErrInvalidCell) {
		t.Fatalf("error = %v; se esperaba ErrInvalidCell", err)
	}
	assertIntacto(t, path, antes)
	if got := backups(t, repo); got != nil {
		t.Errorf("no debería respaldar un libro que no se va a modificar: %v", got)
	}
}

// ============================================================
// Validación del workbook
// ============================================================

func TestValidateWorkbook(t *testing.T) {
	celda := func(hoja, ref string, valor any) func(*testing.T, *excelize.File) {
		return func(t *testing.T, f *excelize.File) { must(t, f.SetCellValue(hoja, ref, valor)) }
	}
	tests := []struct {
		name   string
		cambio func(*testing.T, *excelize.File)
		want   error
		texto  string
	}{
		{"libro válido (incluye historial de un ítem eliminado)", nil, nil, ""},
		{"falta la hoja Movimientos", func(t *testing.T, f *excelize.File) { must(t, f.DeleteSheet(SheetMovements)) }, ErrSheetMissing, ""},
		{"falta la hoja Inventario", func(t *testing.T, f *excelize.File) { must(t, f.DeleteSheet(SheetInventory)) }, ErrSheetMissing, ""},
		{"encabezado de Inventario incorrecto", celda(SheetInventory, "E1", "Marca"), ErrInvalidWorkbook, "columna E"},
		{"columna desconocida en Inventario", celda(SheetInventory, "Q1", "Extra"), ErrInvalidWorkbook, ""},
		{"encabezado de Movimientos incorrecto", celda(SheetMovements, "C1", "Type"), ErrInvalidWorkbook, "columna C"},
		{"ID duplicado", celda(SheetInventory, "A3", 1), ErrInvalidCell, "duplicado"},
		{"Quantity negativa", celda(SheetInventory, "H4", -1), ErrInvalidCell, "celda H4"},
		{"HasInventory SI sin número", celda(SheetInventory, "B2", ""), ErrInvalidCell, "celda B2"},
		{"HasInventory NO con número", celda(SheetInventory, "B4", "123"), ErrInvalidCell, "celda B4"},
		{"fecha inválida", celda(SheetInventory, "L2", "ayer"), ErrInvalidCell, "celda L2"},
		{"fecha faltante", celda(SheetInventory, "M3", ""), ErrInvalidCell, "celda M3"},
		{"MovementType inválido", celda(SheetMovements, "C2", "robo"), ErrInvalidCell, "celda C2"},
		{"movimiento con cantidad negativa", celda(SheetMovements, "D2", -2), ErrInvalidCell, "celda D2"},
		{"movimiento con stock nuevo negativo", celda(SheetMovements, "F2", -1), ErrInvalidCell, "celda F2"},
		{"movimiento con ID duplicado", celda(SheetMovements, "A3", 1), ErrInvalidCell, "duplicado"},
		{"movimiento sin fecha", celda(SheetMovements, "J2", ""), ErrInvalidCell, "celda J2"},
		{"ItemID que nunca existió", celda(SheetMovements, "B2", 500), ErrInvalidMovementReference, "celda B2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := copyFixture(t)
			if tt.cambio != nil {
				modificarLibro(t, path, func(f *excelize.File) { tt.cambio(t, f) })
			}
			f, err := excelize.OpenFile(path)
			must(t, err)
			defer f.Close()

			err = validateWorkbook(f)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("error inesperado: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) || !strings.Contains(err.Error(), tt.texto) {
				t.Errorf("error = %v; se esperaba %v con %q", err, tt.want, tt.texto)
			}
		})
	}
}

// ============================================================
// Check (base de /api/health)
// ============================================================

func TestCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("libro válido", func(t *testing.T) {
		repo, path := newWriteRepo(t)
		antes := leerBytes(t, path)
		must(t, repo.Check(ctx))
		assertIntacto(t, path, antes) // no escribe
		assertValido(t, path)
	})
	t.Run("hoja faltante", func(t *testing.T) {
		repo, path := newWriteRepo(t)
		modificarLibro(t, path, func(f *excelize.File) { must(t, f.DeleteSheet(SheetMovements)) })
		if err := repo.Check(ctx); !errors.Is(err, ErrSheetMissing) {
			t.Errorf("error = %v; se esperaba ErrSheetMissing", err)
		}
	})
	t.Run("encabezado incorrecto", func(t *testing.T) {
		repo, path := newWriteRepo(t)
		modificarLibro(t, path, func(f *excelize.File) { must(t, f.SetCellValue(SheetInventory, "B1", "Numero")) })
		if err := repo.Check(ctx); !errors.Is(err, ErrInvalidWorkbook) {
			t.Errorf("error = %v; se esperaba ErrInvalidWorkbook", err)
		}
	})
	t.Run("archivo inaccesible", func(t *testing.T) {
		repo, path := newWriteRepo(t)
		must(t, os.Remove(path))
		if err := repo.Check(ctx); !errors.Is(err, ErrInvalidWorkbook) {
			t.Errorf("error = %v; se esperaba ErrInvalidWorkbook", err)
		}
	})
}
