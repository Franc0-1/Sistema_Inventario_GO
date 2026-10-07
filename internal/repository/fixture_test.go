package repository

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// El Excel de prueba se genera con código para que su contenido quede
// documentado y sea reproducible. Para regenerarlo:
//
//	go test ./internal/repository -run TestGenerateFixture -update
var updateFixture = flag.Bool("update", false, "regenera testdata/inventario.xlsx")

const fixturePath = "testdata/inventario.xlsx"

func TestGenerateFixture(t *testing.T) {
	if !*updateFixture {
		t.Skip("usar -update para regenerar " + fixturePath)
	}
	f := buildFixture(t)
	defer f.Close()
	if err := os.MkdirAll(filepath.Dir(fixturePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.SaveAs(fixturePath); err != nil {
		t.Fatal(err)
	}
}

// Fechas que el fixture guarda como número de serie de Excel (celda de fecha
// real, como la escribiría alguien desde Excel).
var fixtureSerialDate = time.Date(2026, 9, 10, 12, 0, 0, 0, time.Local)

// buildFixture arma un libro que cubre los casos de lectura:
//
//	fila 2  ID 1  individual, prestado, fechas RFC 3339 (con nanosegundos)
//	fila 3  ID 2  "si" en minúsculas, fechas numéricas de Excel, sin columnas N–P
//	fila 4  ID 3  consumible ("NO"), cantidad numérica, fechas "AAAA-MM-DD hh:mm:ss"
//	fila 5        vacía (debe ignorarse)
//	fila 6  ID 4  celda booleana real de Excel, aire acondicionado, fechas "DD/MM/AAAA"
//	fila 7  ID 5  "1" como booleano, dado de baja
//	fila 8  ID 6  celdas vacías: HasInventory y cantidad
//
// Movimientos: IDs 1, 3 y 2 (desordenados); el 2 pertenece al ítem 7, que se
// eliminó (el último ID de ítem asignado es 7, así que el próximo alta es el 8).
func buildFixture(t *testing.T) *excelize.File {
	t.Helper()
	f := excelize.NewFile()
	must(t, f.SetSheetName(f.GetSheetName(0), SheetInventory))
	for _, s := range []string{SheetMovements, SheetCategories} {
		_, err := f.NewSheet(s)
		must(t, err)
	}
	for _, s := range allSchemas {
		must(t, writeHeader(f, s, nil))
	}

	rows := map[int][]any{
		2: {1, "1001", "SI", "Notebook", "Dell", "Latitude 5420", "SN-987654321", 1, "Administración", "OPERATIVO", "",
			"2026-09-01T10:00:00-03:00", "2026-09-15T09:30:00.123456789-03:00", "PRESTADO", "María Gómez", "2026-09-15T09:30:00-03:00"},
		3: {2, "1002", "si", "Notebook", "Lenovo", "ThinkPad T14", "SN-112233445", 1, "Sistemas", "OPERATIVO", ""},
		4: {3, "", "NO", "Mouse", "Logitech", "Mouse M280", "", 12, "Sistemas", "OPERATIVO", "Caja en estante 2",
			"2026-09-02 08:15:00", "2026-09-02"},
		6: {4, "3001", nil, "Aire acondicionado", "BGH", "Silent Air BS35", "BGH35-0091", 1, "Administración", "EN_REPARACION", "No enfría",
			"10/08/2026", "20/08/2026"},
		7: {5, "1500", "1", "Impresora", "HP", "LaserJet Pro M404", "vnb3k12345", 1, "Recepción", "BAJA", "Dada de baja por rotura",
			"2026-01-05", "2026-09-20"},
		8: {6, "", "", "Accesorio", "Genérico", "Adaptador USB-C", "", "", "Sistemas", "OPERATIVO", "",
			"2026-03-01", "2026-03-01"},
	}
	for rowNum, values := range rows {
		must(t, setRow(f, SheetInventory, rowNum, values))
	}

	// Fila 3: fechas como número de serie con formato de fecha.
	estiloFecha, err := f.NewStyle(&excelize.Style{NumFmt: 22}) // m/d/yy h:mm
	must(t, err)
	for _, cell := range []string{"L3", "M3"} {
		must(t, f.SetCellFloat(SheetInventory, cell, excelSerial(fixtureSerialDate), -1, 64))
		must(t, f.SetCellStyle(SheetInventory, cell, cell, estiloFecha))
	}
	// Fila 6: celda booleana real.
	must(t, f.SetCellBool(SheetInventory, "C6", true))

	// Movimientos, desordenados por ID a propósito. El 2 es del ítem 7, que se
	// creó y luego se eliminó: su historial debe seguir leyéndose.
	movimientos := map[int][]any{
		2: {1, 3, "stock_out", 2, 14, 12, "", "", "Entrega a Sistemas", "2026-09-10T10:00:00-03:00"},
		3: {3, 1, "transfer", 1, 1, 1, "Sistemas", "Administración", "Asignada a María", "2026-09-15T09:30:00-03:00"},
		4: {2, 7, "stock_in", 5, 0, 5, "", "", "Ítem que luego se eliminó", "2026-09-12"},
	}
	for rowNum, values := range movimientos {
		must(t, setRow(f, SheetMovements, rowNum, values))
	}
	// El ítem 7 existió: el último ID asignado lo registra (así lo deja Delete).
	must(t, inventoryIDs.setLastIssued(f, 7))

	must(t, setRow(f, SheetCategories, 2, categoryToRow(categoryFixture(1, "Notebook", true))))
	must(t, setRow(f, SheetCategories, 3, categoryToRow(categoryFixture(2, "Aire acondicionado", false))))
	return f
}

// excelSerial convierte una hora local a número de serie de Excel (días desde 1899-12-30).
func excelSerial(t time.Time) float64 {
	wall := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
	return wall.Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)).Hours() / 24
}

// ---- Helpers compartidos por los tests ----

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// copyFixture copia el Excel de prueba a un directorio temporal: los tests
// nunca modifican testdata.
func copyFixture(t *testing.T) string {
	t.Helper()
	src, err := os.Open(fixturePath)
	must(t, err)
	defer src.Close()

	dst := filepath.Join(t.TempDir(), "inventario.xlsx")
	out, err := os.Create(dst)
	must(t, err)
	defer out.Close()
	_, err = io.Copy(out, src)
	must(t, err)
	return dst
}

func newFixtureRepo(t *testing.T) *ExcelRepository {
	t.Helper()
	repo, err := NewExcelRepository(copyFixture(t))
	must(t, err)
	return repo
}

// writeWorkbook guarda en un directorio temporal un libro armado por build.
func writeWorkbook(t *testing.T, build func(f *excelize.File)) string {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	build(f)
	path := filepath.Join(t.TempDir(), "libro.xlsx")
	must(t, f.SaveAs(path))
	return path
}
