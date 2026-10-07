package repository

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
)

func exportar(t *testing.T, repo *ExcelRepository) []byte {
	t.Helper()
	data, err := repo.ExportInventory(context.Background())
	must(t, err)
	return data
}

// filasExportadas devuelve las celdas (como texto) de la hoja descargada.
func filasExportadas(t *testing.T, data []byte) [][]string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	must(t, err)
	defer f.Close()
	rows, err := f.GetRows(SheetInventory)
	must(t, err)
	return rows
}

// editarExportado abre el archivo descargado, aplica change (como lo haría
// alguien en Excel) y devuelve el archivo resultante.
func editarExportado(t *testing.T, data []byte, change func(f *excelize.File)) []byte {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	must(t, err)
	defer f.Close()
	change(f)
	out, err := writeBytes(f)
	must(t, err)
	return out
}

func set(t *testing.T, f *excelize.File, cell string, value any) {
	t.Helper()
	must(t, f.SetCellValue(SheetInventory, cell, value))
}

func TestExport_EnEspanolYSoloDatosUtiles(t *testing.T) {
	repo := newFixtureRepo(t)
	rows := filasExportadas(t, exportar(t, repo))

	want := []string{"ID", "N° de inventario", "Tipo de dispositivo", "Marca", "Modelo", "N° de serie",
		"Cantidad", "Área", "Estado", "Disponibilidad", "Asignado a", "Observación", "ID del equipo"}
	if !slices.Equal(rows[0], want) {
		t.Fatalf("encabezado = %q\nse esperaba  %q", rows[0], want)
	}
	if len(rows) != 7 {
		t.Fatalf("%d filas; se esperaban encabezado + 6 ítems", len(rows))
	}
	fila1 := []string{"1", "1001", "Notebook", "Dell", "Latitude 5420", "SN-987654321", "1", "Administración",
		"Utilizable", "Prestado", "María Gómez"}
	if !slices.Equal(rows[1], fila1) {
		t.Errorf("fila del ítem 1 = %q\nse esperaba     %q", rows[1], fila1)
	}
	estados := map[string]string{}
	for _, r := range rows[1:] {
		estados[r[0]] = r[8]
	}
	if estados["4"] != "En reparación" || estados["5"] != "Dado de baja" {
		t.Errorf("los estados no están en español: %v", estados)
	}
	for _, r := range rows {
		for _, cell := range r {
			if strings.Contains(cell, "OPERATIVO") || strings.Contains(cell, "DISPONIBLE") || strings.Contains(cell, "T09:") {
				t.Errorf("la descarga contiene datos internos: %q", r)
			}
		}
	}
}

func TestExport_FormatoDeHoja(t *testing.T) {
	repo := newFixtureRepo(t)
	f, err := excelize.OpenReader(bytes.NewReader(exportar(t, repo)))
	must(t, err)
	defer f.Close()
	panes, err := f.GetPanes(SheetInventory)
	must(t, err)
	if !panes.Freeze || panes.YSplit != 1 {
		t.Errorf("el encabezado debería quedar fijo: %+v", panes)
	}
	if w, _ := f.GetColWidth(SheetInventory, "L"); w < 30 {
		t.Errorf("la columna Observación debería ser ancha (%v)", w)
	}
}

// Descargar y volver a subir el mismo archivo no cambia nada: mismos datos,
// mismas fechas y el historial intacto.
func TestImportarLoDescargado_SinCambios(t *testing.T) {
	repo, _ := newWriteRepo(t)
	ctx := context.Background()
	antes, err := repo.GetAll(ctx)
	must(t, err)
	movsAntes, err := repo.GetMovements(ctx)
	must(t, err)

	n, err := repo.ImportInventory(ctx, bytes.NewReader(exportar(t, repo)))
	must(t, err)
	if n != len(antes) {
		t.Fatalf("importó %d; se esperaban %d", n, len(antes))
	}
	despues, err := repo.GetAll(ctx)
	must(t, err)
	if len(despues) != len(antes) {
		t.Fatalf("%d ítems después; %d antes", len(despues), len(antes))
	}
	for i := range antes {
		a, d := antes[i], despues[i]
		if !a.CreatedAt.Equal(d.CreatedAt) || !a.UpdatedAt.Equal(d.UpdatedAt) || (a.LoanedAt == nil) != (d.LoanedAt == nil) ||
			(a.LoanedAt != nil && !a.LoanedAt.Equal(*d.LoanedAt)) {
			t.Errorf("ítem %d: cambiaron las fechas\nantes   %+v\ndespués %+v", a.ID, a, d)
		}
		a.CreatedAt, a.UpdatedAt, a.LoanedAt = time.Time{}, time.Time{}, nil
		d.CreatedAt, d.UpdatedAt, d.LoanedAt = time.Time{}, time.Time{}, nil
		if a != d {
			t.Errorf("ítem cambió:\nantes   %+v\ndespués %+v", a, d)
		}
	}
	movsDespues, err := repo.GetMovements(ctx)
	must(t, err)
	if !reflect.DeepEqual(movsDespues, movsAntes) {
		t.Error("el historial cambió")
	}
}

// Editar el archivo en Excel: cambiar datos, agregar filas sin ID y borrar filas.
func TestImportarLoDescargado_ConCambios(t *testing.T) {
	repo, path := newWriteRepo(t)
	ctx := context.Background()
	item2, item3 := mustGet(t, repo, 2), mustGet(t, repo, 3)

	// Filas: 2=ID 1, 3=ID 2, 4=ID 3, 5=ID 4, 6=ID 5, 7=ID 6.
	data := editarExportado(t, exportar(t, repo), func(f *excelize.File) {
		set(t, f, "I3", "en reparacion") // sin tilde ni mayúsculas
		set(t, f, "H3", "Depósito")
		must(t, f.RemoveRow(SheetInventory, 7)) // se elimina el ítem 6
		must(t, f.SetSheetRow(SheetInventory, "A7", &[]any{"", "", "Teclado", "Logitech", "K120", "", 15, "Sistemas"}))
		must(t, f.SetSheetRow(SheetInventory, "A8", &[]any{nil, "9001", "Monitor", "LG", "24MK430", "LG-1", nil, "Dirección", "Utilizable", "Prestado", "Juan Pérez"}))
	})
	_, err := repo.ImportInventory(ctx, bytes.NewReader(data))
	must(t, err)

	items, err := repo.GetAll(ctx)
	must(t, err)
	if got := ids(items); !slices.Equal(got, []int{1, 2, 3, 4, 5, 8, 9}) {
		t.Fatalf("IDs = %v; los nuevos deben recibir 8 y 9 (el 6 y el 7 ya se usaron)", got)
	}
	nuevo2 := mustGet(t, repo, 2)
	if nuevo2.Status != models.StatusInRepair || nuevo2.Location != "Depósito" || !nuevo2.UpdatedAt.After(item2.UpdatedAt) ||
		!nuevo2.CreatedAt.Equal(item2.CreatedAt) {
		t.Errorf("ítem 2 = %+v", nuevo2)
	}
	if nuevo3 := mustGet(t, repo, 3); !nuevo3.UpdatedAt.Equal(item3.UpdatedAt) {
		t.Error("un ítem sin cambios no debe cambiar su fecha de modificación")
	}
	teclado := mustGet(t, repo, 8)
	if teclado.HasInventory || teclado.Quantity != 15 || teclado.Status != models.StatusOperational ||
		teclado.Availability != models.Available || teclado.CreatedAt.IsZero() {
		t.Errorf("ítem nuevo sin N° = %+v", teclado)
	}
	monitor := mustGet(t, repo, 9)
	if !monitor.HasInventory || monitor.Quantity != 1 || monitor.Availability != models.Loaned ||
		monitor.AssignedTo != "Juan Pérez" || monitor.LoanedAt == nil {
		t.Errorf("ítem nuevo con N° y prestado = %+v", monitor)
	}
	assertValido(t, path)

	// Un ID de un ítem eliminado no se reutiliza.
	data = editarExportado(t, exportar(t, repo), func(f *excelize.File) {
		set(t, f, "A2", 6)
	})
	_, err = repo.ImportInventory(ctx, bytes.NewReader(data))
	if !errors.Is(err, ErrInvalidCell) || !strings.Contains(err.Error(), "no existe") {
		t.Errorf("error = %v; se esperaba que el ID 6 no existe", err)
	}
}

func TestImportarLoDescargado_Errores(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, f *excelize.File)
		want   error
		text   string
	}{
		{"ID inexistente", func(t *testing.T, f *excelize.File) { set(t, f, "A2", 99) }, ErrInvalidCell, "celda A2"},
		{"ID no numérico", func(t *testing.T, f *excelize.File) { set(t, f, "A2", "uno") }, ErrInvalidCell, "celda A2"},
		{"ID repetido", func(t *testing.T, f *excelize.File) { set(t, f, "A3", 1) }, ErrInvalidCell, "repetido"},
		{"estado desconocido", func(t *testing.T, f *excelize.File) { set(t, f, "I2", "Roto") }, ErrInvalidCell, "celda I2"},
		{"disponibilidad desconocida", func(t *testing.T, f *excelize.File) { set(t, f, "J2", "Libre") }, ErrInvalidCell, "celda J2"},
		{"falta la marca", func(t *testing.T, f *excelize.File) { set(t, f, "D3", "") }, ErrInvalidCell, "celda D3"},
		{"falta el área", func(t *testing.T, f *excelize.File) { set(t, f, "H3", " ") }, ErrInvalidCell, "celda H3"},
		{"cantidad negativa", func(t *testing.T, f *excelize.File) { set(t, f, "G4", -3) }, ErrInvalidCell, "celda G4"},
		{"cantidad vacía en un consumible", func(t *testing.T, f *excelize.File) { set(t, f, "G4", "") }, ErrInvalidCell, "celda G4"},
		{"asignado a un ítem disponible", func(t *testing.T, f *excelize.File) { set(t, f, "K3", "Ana") }, ErrInvalidCell, "celda K3"},
		{"N° de inventario repetido", func(t *testing.T, f *excelize.File) { set(t, f, "B3", "1001") }, ErrInvalidCell, "repetido"},
		{"N° de serie repetido", func(t *testing.T, f *excelize.File) { set(t, f, "F3", "sn-987654321") }, ErrInvalidCell, "repetido"},
		{"columna renombrada", func(t *testing.T, f *excelize.File) { set(t, f, "D1", "Fabricante") }, ErrInvalidWorkbook, "columna D"},
		{"columna eliminada", func(t *testing.T, f *excelize.File) { must(t, f.RemoveCol(SheetInventory, "F")) }, ErrInvalidWorkbook, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, path := newWriteRepo(t)
			data := editarExportado(t, exportar(t, repo), func(f *excelize.File) { tt.change(t, f) })
			antes := leerBytes(t, path)
			_, err := repo.ImportInventory(context.Background(), bytes.NewReader(data))
			if !errors.Is(err, tt.want) || !strings.Contains(err.Error(), tt.text) {
				t.Fatalf("error = %v; se esperaba %v con %q", err, tt.want, tt.text)
			}
			assertIntacto(t, path, antes)
		})
	}
}
