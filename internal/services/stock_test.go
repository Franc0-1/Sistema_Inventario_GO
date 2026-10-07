package services

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"inventario/internal/models"
	"inventario/internal/repository"
)

// En seed() el ítem 3 es un consumible con 10 unidades en "Sistemas".

func op(tipo models.MovementType, cantidad int) models.StockOperation {
	return models.StockOperation{Type: tipo, Quantity: cantidad}
}

// ============================================================
// Operaciones: cantidades anterior/nueva y movimiento registrado
// ============================================================

func TestApplyMovement_Operaciones(t *testing.T) {
	tests := []struct {
		name      string
		op        models.StockOperation
		wantStock int
		wantMov   models.Movement
	}{
		{"entrada", models.StockOperation{Type: models.MovementStockIn, Quantity: 5, Notes: " compra 123 "}, 15,
			models.Movement{MovementType: models.MovementStockIn, Quantity: 5, PreviousQuantity: 10, NewQuantity: 15, Notes: "compra 123"}},
		{"salida", op(models.MovementStockOut, 4), 6,
			models.Movement{MovementType: models.MovementStockOut, Quantity: 4, PreviousQuantity: 10, NewQuantity: 6}},
		{"salida de todo el stock", op(models.MovementStockOut, 10), 0,
			models.Movement{MovementType: models.MovementStockOut, Quantity: 10, PreviousQuantity: 10, NewQuantity: 0}},
		{"ajuste hacia abajo", op(models.MovementAdjustment, 7), 7,
			models.Movement{MovementType: models.MovementAdjustment, Quantity: 3, PreviousQuantity: 10, NewQuantity: 7}},
		{"ajuste hacia arriba", op(models.MovementAdjustment, 12), 12,
			models.Movement{MovementType: models.MovementAdjustment, Quantity: 2, PreviousQuantity: 10, NewQuantity: 12}},
		{"ajuste a cero", op(models.MovementAdjustment, 0), 0,
			models.Movement{MovementType: models.MovementAdjustment, Quantity: 10, PreviousQuantity: 10, NewQuantity: 0}},
		{"actualización que sube", op(models.MovementStockUpdate, 13), 13,
			models.Movement{MovementType: models.MovementStockIn, Quantity: 3, PreviousQuantity: 10, NewQuantity: 13}},
		{"actualización sin cambio", op(models.MovementStockUpdate, 10), 10,
			models.Movement{MovementType: models.MovementStockUpdate, Quantity: 0, PreviousQuantity: 10, NewQuantity: 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTestService(seed()...)
			item, err := svc.ApplyMovement(ctx, 3, tt.op)
			if err != nil {
				t.Fatal(err)
			}
			if item.Quantity != tt.wantStock || item.Location != "Sistemas" {
				t.Errorf("ítem: cantidad %d en %q; se esperaba %d en Sistemas", item.Quantity, item.Location, tt.wantStock)
			}
			mov, ok := repo.lastMovement()
			if !ok {
				t.Fatal("no se registró el movimiento")
			}
			want := tt.wantMov
			want.ID, want.ItemID, want.CreatedAt = mov.ID, 3, item.UpdatedAt
			if mov != want {
				t.Errorf("movimiento = %+v\nse esperaba  %+v", mov, want)
			}
			if !repo.called("UpdateStock") || repo.called("Update") {
				t.Errorf("stock y movimiento deben guardarse juntos con UpdateStock; llamadas: %v", repo.calls)
			}
		})
	}
}

func TestApplyMovement_Transferencia(t *testing.T) {
	svc, repo := newTestService(seed()...)
	antes, _ := svc.GetByID(ctx, 3)

	item, err := svc.ApplyMovement(ctx, 3, models.StockOperation{Type: models.MovementTransfer,
		DestinationLocation: "  Depósito ", Notes: "reubicación"})
	if err != nil {
		t.Fatal(err)
	}
	if item.Location != "Depósito" || item.Quantity != 10 {
		t.Errorf("ítem en %q con %d; se esperaba Depósito con 10 (la cantidad no cambia)", item.Location, item.Quantity)
	}
	otros := item
	otros.Location, otros.UpdatedAt = antes.Location, antes.UpdatedAt
	if otros != antes {
		t.Errorf("la transferencia cambió otros campos:\n%+v\n%+v", otros, antes)
	}
	mov, _ := repo.lastMovement()
	want := models.Movement{ID: mov.ID, ItemID: 3, MovementType: models.MovementTransfer, Quantity: 10,
		PreviousQuantity: 10, NewQuantity: 10, OriginLocation: "Sistemas", DestinationLocation: "Depósito",
		Notes: "reubicación", CreatedAt: item.UpdatedAt}
	if mov != want {
		t.Errorf("movimiento = %+v\nse esperaba  %+v", mov, want)
	}
}

// ============================================================
// Validaciones: nada se guarda si la operación es inválida
// ============================================================

func TestApplyMovement_Rechaza(t *testing.T) {
	tests := []struct {
		name string
		id   int
		op   models.StockOperation
		want error
	}{
		{"salida mayor al stock", 3, op(models.MovementStockOut, 11), ErrInsufficientStock},
		{"salida sobre stock cero", 3, op(models.MovementStockOut, 11), ErrInsufficientStock},
		{"tipo inválido", 3, op("robo", 1), ErrInvalidMovement},
		{"tipo vacío", 3, op("", 1), ErrInvalidMovement},
		{"tipo con mayúsculas", 3, op("STOCK_IN", 1), ErrInvalidMovement},
		{"entrada de cero", 3, op(models.MovementStockIn, 0), ErrInvalidMovement},
		{"salida de cero", 3, op(models.MovementStockOut, 0), ErrInvalidMovement},
		{"entrada negativa", 3, op(models.MovementStockIn, -2), ErrInvalidQuantity},
		{"salida negativa", 3, op(models.MovementStockOut, -2), ErrInvalidQuantity},
		{"ajuste negativo", 3, op(models.MovementAdjustment, -1), ErrInvalidQuantity},
		{"actualización negativa", 3, op(models.MovementStockUpdate, -1), ErrInvalidQuantity},
		{"entrada que desborda", 3, op(models.MovementStockIn, math.MaxInt), ErrInvalidQuantity},
		{"transferencia sin destino", 3, models.StockOperation{Type: models.MovementTransfer, DestinationLocation: "   "}, ErrInvalidMovement},
		{"transferencia con cantidad", 3, models.StockOperation{Type: models.MovementTransfer, Quantity: 2, DestinationLocation: "Depósito"}, ErrInvalidMovement},
		{"transferencia a la misma ubicación", 3, models.StockOperation{Type: models.MovementTransfer, DestinationLocation: " SISTEMAS "}, ErrInvalidMovement},
		{"ítem inexistente", 99, op(models.MovementStockIn, 1), repository.ErrItemNotFound},
		{"transferencia de ítem inexistente", 99, models.StockOperation{Type: models.MovementTransfer, DestinationLocation: "X"}, repository.ErrItemNotFound},
		{"ID inválido", 0, op(models.MovementStockIn, 1), ErrInvalidID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := seed()
			if tt.name == "salida sobre stock cero" {
				items[2].Quantity = 0
			}
			svc, repo := newTestService(items...)
			_, err := svc.ApplyMovement(ctx, tt.id, tt.op)
			assertErr(t, err, tt.want)
			if repo.called("UpdateStock") || repo.called("Update") {
				t.Errorf("no debería escribir; llamadas: %v", repo.calls)
			}
			if _, ok := repo.lastMovement(); ok {
				t.Error("no debería registrar movimientos")
			}
			if tt.id == 3 {
				if it, _ := svc.GetByID(ctx, 3); it.Quantity != items[2].Quantity || it.Location != "Sistemas" {
					t.Errorf("el ítem cambió: %+v", it)
				}
			}
		})
	}
}

// La versión que lee el servicio viaja al repositorio: si otro escribió en el
// medio, la operación falla con ErrConflict en lugar de pisar el cambio.
func TestApplyMovement_UsaLaVersionLeida(t *testing.T) {
	svc, repo := newTestService(seed()...)
	repo.beforeWrite = func() { repo.items[2].UpdatedAt = repo.tick() } // otra escritura en el medio
	_, err := svc.ApplyMovement(ctx, 3, op(models.MovementStockOut, 1))
	assertErr(t, err, repository.ErrConflict)
	if _, ok := repo.lastMovement(); ok {
		t.Error("un conflicto no debe registrar movimientos")
	}
}

// Salidas concurrentes: nunca stock negativo, y cada salida exitosa tiene su
// movimiento (Inventario ↔ Movimientos consistentes).
func TestApplyMovement_SalidasConcurrentes(t *testing.T) {
	svc, _ := newTestService(seed()...)
	const intentos = 30
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		exitos  int
		errores = map[string]int{}
	)
	for range intentos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.ApplyMovement(ctx, 3, op(models.MovementStockOut, 1))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				exitos++
			case errors.Is(err, repository.ErrConflict), errors.Is(err, ErrInsufficientStock):
				errores[err.Error()]++
			default:
				t.Errorf("error inesperado: %v", err)
			}
		}()
	}
	wg.Wait()

	item, _ := svc.GetByID(ctx, 3)
	movs, _ := svc.GetMovementsByItemID(ctx, 3)
	if item.Quantity < 0 || item.Quantity != 10-exitos || len(movs) != exitos || exitos > 10 {
		t.Fatalf("stock %d, %d salidas exitosas, %d movimientos", item.Quantity, exitos, len(movs))
	}
	stock := 10
	for _, m := range movs {
		if m.PreviousQuantity != stock || m.NewQuantity != stock-1 {
			t.Errorf("movimiento %+v no encadena con el stock %d", m, stock)
		}
		stock = m.NewQuantity
	}
}

// ============================================================
// Con el repositorio de Excel real (copia de testdata)
// ============================================================

func excelRepoDePrueba(t *testing.T) (*repository.ExcelRepository, string) {
	t.Helper()
	src, err := os.Open(filepath.Join("..", "repository", "testdata", "inventario.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	path := filepath.Join(t.TempDir(), "inventario.xlsx")
	dst, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	repo, err := repository.NewExcelRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	return repo, path
}

// Recorre todas las operaciones sobre el Excel y verifica, releyendo el
// archivo con otro repositorio, que el historial explica el stock y la
// ubicación guardados. En el fixture el ítem 3 tiene 12 unidades en Sistemas
// y ya un movimiento previo.
func TestApplyMovement_PersistenciaEnExcel(t *testing.T) {
	repo, path := excelRepoDePrueba(t)
	svc := NewInventoryService(repo)

	pasos := []models.StockOperation{
		op(models.MovementStockIn, 8),      // 12 -> 20
		op(models.MovementStockOut, 5),     // 20 -> 15
		op(models.MovementAdjustment, 14),  // 15 -> 14
		op(models.MovementStockUpdate, 14), // sin cambio
		{Type: models.MovementTransfer, DestinationLocation: "Depósito"},
	}
	for _, p := range pasos {
		if _, err := svc.ApplyMovement(ctx, 3, p); err != nil {
			t.Fatalf("%+v: %v", p, err)
		}
	}
	_, err := svc.ApplyMovement(ctx, 3, op(models.MovementStockOut, 15))
	assertErr(t, err, ErrInsufficientStock)

	releido, err := repository.NewExcelRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	item, err := releido.GetByID(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if item.Quantity != 14 || item.Location != "Depósito" || item.Brand != "Logitech" {
		t.Errorf("ítem guardado: %d en %q (%s); se esperaba 14 en Depósito", item.Quantity, item.Location, item.Brand)
	}
	movs, err := releido.GetMovementsByItemID(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	nuevos := movs[1:] // el primero es del fixture
	wantTipos := []models.MovementType{models.MovementStockIn, models.MovementStockOut, models.MovementAdjustment,
		models.MovementStockUpdate, models.MovementTransfer}
	if len(nuevos) != len(wantTipos) {
		t.Fatalf("%d movimientos nuevos; se esperaban %d (la salida rechazada no debe registrarse)", len(nuevos), len(wantTipos))
	}
	stock := 12
	for i, m := range nuevos {
		if m.MovementType != wantTipos[i] || m.PreviousQuantity != stock || m.CreatedAt.IsZero() {
			t.Errorf("movimiento %d = %+v; se esperaba %s desde %d", i, m, wantTipos[i], stock)
		}
		stock = m.NewQuantity
	}
	if stock != item.Quantity {
		t.Errorf("el historial termina en %d y el inventario tiene %d", stock, item.Quantity)
	}
	if last := nuevos[len(nuevos)-1]; last.OriginLocation != "Sistemas" || last.DestinationLocation != "Depósito" || last.Quantity != 14 {
		t.Errorf("transferencia = %+v", last)
	}
	if !nuevos[len(nuevos)-1].CreatedAt.Equal(item.UpdatedAt) {
		t.Error("la fecha del último movimiento debe ser la de la última escritura del ítem")
	}
}
