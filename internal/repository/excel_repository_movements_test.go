package repository

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
)

// stockMovement arma el movimiento que la capa de servicios pasaría a
// UpdateStock: tipo y magnitud. ItemID, cantidades y fecha los completa el repositorio.
func stockMovement(tipo models.MovementType, cantidad int) models.Movement {
	return models.Movement{MovementType: tipo, Quantity: cantidad}
}

func idsDe(movs []models.Movement) []int {
	out := []int{}
	for _, m := range movs {
		out = append(out, m.ID)
	}
	return out
}

func mustMovements(t *testing.T, repo *ExcelRepository, itemID int) []models.Movement {
	t.Helper()
	movs, err := repo.GetMovementsByItemID(context.Background(), itemID)
	must(t, err)
	return movs
}

// ============================================================
// Lectura
// ============================================================

func TestGetMovements(t *testing.T) {
	movs, err := newFixtureRepo(t).GetMovements(context.Background())
	must(t, err)

	if got := idsDe(movs); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("IDs = %v; se esperaba [1 2 3] (ordenados aunque la hoja no lo esté)", got)
	}

	t.Run("interpreta todos los campos", func(t *testing.T) {
		fecha, _ := time.Parse(time.RFC3339, "2026-09-10T10:00:00-03:00")
		want := models.Movement{ID: 1, ItemID: 3, MovementType: models.MovementStockOut, Quantity: 2,
			PreviousQuantity: 14, NewQuantity: 12, Notes: "Entrega a Sistemas", CreatedAt: fecha}
		got := movs[0]
		if !got.CreatedAt.Equal(want.CreatedAt) {
			t.Errorf("CreatedAt = %v; se esperaba %v", got.CreatedAt, want.CreatedAt)
		}
		got.CreatedAt = want.CreatedAt
		if got != want {
			t.Errorf("movimiento =\n%+v\nse esperaba\n%+v", got, want)
		}
	})
	t.Run("ubicaciones de un traslado", func(t *testing.T) {
		if m := movs[2]; m.OriginLocation != "Sistemas" || m.DestinationLocation != "Administración" {
			t.Errorf("origen/destino = %q/%q", m.OriginLocation, m.DestinationLocation)
		}
	})
	t.Run("fecha sin hora", func(t *testing.T) {
		if want := localTime(2026, 9, 12, 0, 0); !movs[1].CreatedAt.Equal(want) {
			t.Errorf("CreatedAt = %v; se esperaba %v", movs[1].CreatedAt, want)
		}
	})
}

func TestGetMovementsByItemID(t *testing.T) {
	repo := newFixtureRepo(t)
	tests := []struct {
		name   string
		itemID int
		want   []int
	}{
		{"solo los del ítem pedido", 3, []int{1}},
		{"historial de un ítem ya eliminado", 7, []int{2}},
		{"ítem sin movimientos devuelve lista vacía", 2, []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movs := mustMovements(t, repo, tt.itemID)
			if movs == nil {
				t.Fatal("devolvió nil; se esperaba una lista (vacía o no)")
			}
			if got := idsDe(movs); !slices.Equal(got, tt.want) {
				t.Errorf("IDs = %v; se esperaba %v", got, tt.want)
			}
		})
	}
}

// ============================================================
// CreateMovement
// ============================================================

func TestCreateMovement(t *testing.T) {
	repo, path := newWriteRepo(t)
	ctx := context.Background()
	inventarioAntes := sheetRows(t, path, SheetInventory)

	input := models.Movement{
		ID: 50, CreatedAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), // se ignoran
		ItemID: 2, MovementType: models.MovementTransfer, Quantity: 1, PreviousQuantity: 1, NewQuantity: 1,
		OriginLocation: "Sistemas", DestinationLocation: "Dirección", Notes: "Préstamo a Dirección",
	}
	created, err := repo.CreateMovement(ctx, input)
	must(t, err)

	t.Run("genera ID y fecha", func(t *testing.T) {
		if created.ID != 4 || !created.CreatedAt.Equal(writeTime) {
			t.Errorf("ID=%d CreatedAt=%v; se esperaba 4 y %v", created.ID, created.CreatedAt, writeTime)
		}
	})
	t.Run("guarda todos los campos", func(t *testing.T) {
		want := input
		want.ID, want.CreatedAt = 4, writeTime
		got := mustMovements(t, repo, 2)
		if len(got) != 1 || !got[0].CreatedAt.Equal(want.CreatedAt) {
			t.Fatalf("movimientos del ítem 2 = %+v", got)
		}
		got[0].CreatedAt = want.CreatedAt
		if got[0] != want {
			t.Errorf("guardado =\n%+v\nse esperaba\n%+v", got[0], want)
		}
	})
	t.Run("IDs consecutivos", func(t *testing.T) {
		otro, err := repo.CreateMovement(ctx, input)
		must(t, err)
		if otro.ID != 5 {
			t.Errorf("ID = %d; se esperaba 5", otro.ID)
		}
	})
	t.Run("no modifica Inventario", func(t *testing.T) {
		if !reflect.DeepEqual(sheetRows(t, path, SheetInventory), inventarioAntes) {
			t.Error("la hoja Inventario cambió")
		}
	})
}

func TestCreateMovement_NoReutilizaIDs(t *testing.T) {
	repo, path := newWriteRepo(t)
	ctx := context.Background()
	mov := models.Movement{ItemID: 3, MovementType: models.MovementStockUpdate}

	created, err := repo.CreateMovement(ctx, mov)
	must(t, err)

	// Alguien borra a mano la última fila de Movimientos (la del ID recién creado).
	f, err := excelize.OpenFile(path)
	must(t, err)
	must(t, f.RemoveRow(SheetMovements, 5))
	must(t, f.Save())
	must(t, f.Close())

	otro, err := repo.CreateMovement(ctx, mov)
	must(t, err)
	if otro.ID != created.ID+1 {
		t.Errorf("ID = %d; se esperaba %d (el %d no debe reutilizarse)", otro.ID, created.ID+1, created.ID)
	}
}

func TestCreateMovement_Invalido(t *testing.T) {
	tests := []struct {
		name string
		mov  models.Movement
	}{
		{"sin ítem", models.Movement{MovementType: models.MovementStockIn}},
		{"sin tipo", models.Movement{ItemID: 3}},
		{"cantidad negativa", models.Movement{ItemID: 3, MovementType: models.MovementStockIn, Quantity: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, _ := newWriteRepo(t)
			if _, err := repo.CreateMovement(context.Background(), tt.mov); !errors.Is(err, ErrInvalidMovement) {
				t.Fatalf("error = %v; se esperaba ErrInvalidMovement", err)
			}
			if movs, _ := repo.GetMovements(context.Background()); len(movs) != 3 {
				t.Errorf("hay %d movimientos; no debería haberse agregado ninguno", len(movs))
			}
		})
	}
}

// ============================================================
// UpdateStock + movimiento (una sola escritura)
// ============================================================

func TestUpdateStock_RegistraMovimiento(t *testing.T) {
	repo, _ := newWriteRepo(t)
	ctx := context.Background()

	// El ítem 3 tiene 12 unidades; primero se lleva a 10 para reproducir los casos de la especificación.
	pasos := []struct {
		nueva    int
		tipo     models.MovementType
		magnitud int
		anterior int
	}{
		{10, models.MovementStockOut, 2, 12},
		{15, models.MovementStockIn, 5, 10},
		{10, models.MovementStockOut, 5, 15},
		{10, models.MovementStockUpdate, 0, 10},
	}
	for _, p := range pasos {
		antes := mustGet(t, repo, 3)
		item, err := repo.UpdateStock(ctx, 3, p.nueva, antes.UpdatedAt, stockMovement(p.tipo, p.magnitud))
		must(t, err)

		movs := mustMovements(t, repo, 3)
		ultimo := movs[len(movs)-1]
		want := models.Movement{ID: ultimo.ID, ItemID: 3, MovementType: p.tipo, Quantity: p.magnitud,
			PreviousQuantity: p.anterior, NewQuantity: p.nueva, CreatedAt: item.UpdatedAt}
		if !ultimo.CreatedAt.Equal(want.CreatedAt) || sinFecha(ultimo) != sinFecha(want) {
			t.Errorf("%d -> %d: movimiento =\n%+v\nse esperaba\n%+v", p.anterior, p.nueva, ultimo, want)
		}
		if got := mustGet(t, repo, 3).Quantity; got != p.nueva {
			t.Errorf("%d -> %d: Quantity en Inventario = %d", p.anterior, p.nueva, got)
		}
	}
	if movs := mustMovements(t, repo, 3); len(movs) != 1+len(pasos) {
		t.Errorf("el ítem 3 tiene %d movimientos; se esperaban %d", len(movs), 1+len(pasos))
	}
}

func sinFecha(m models.Movement) models.Movement {
	m.CreatedAt = time.Time{}
	return m
}

func TestUpdateStock_ConflictoNoDejaNadaAMedias(t *testing.T) {
	repo, path := newWriteRepo(t)
	antes := sheetRows(t, path, SheetMovements)
	viejo := mustGet(t, repo, 3)
	_, err := repo.UpdateStock(context.Background(), 3, 20, viejo.UpdatedAt, stockMovement(models.MovementStockIn, 8))
	must(t, err)
	intermedio := sheetRows(t, path, SheetMovements)

	// Segunda escritura con la versión vieja: se rechaza y no toca ninguna de las dos hojas.
	_, err = repo.UpdateStock(context.Background(), 3, 1, viejo.UpdatedAt, stockMovement(models.MovementStockOut, 11))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("error = %v; se esperaba ErrConflict", err)
	}
	if len(intermedio) != len(antes)+1 || !reflect.DeepEqual(sheetRows(t, path, SheetMovements), intermedio) {
		t.Error("el conflicto no debería agregar movimientos")
	}
	if got := mustGet(t, repo, 3).Quantity; got != 20 {
		t.Errorf("Quantity = %d; debería seguir en 20", got)
	}
}

func TestUpdateStock_MovimientoInvalidoNoModificaInventario(t *testing.T) {
	repo, path := newWriteRepo(t)
	inventarioAntes := sheetRows(t, path, SheetInventory)
	antes := mustGet(t, repo, 3)

	// El Inventario se modifica en memoria antes de agregar el movimiento; si el
	// movimiento falla, el libro no se guarda.
	_, err := repo.UpdateStock(context.Background(), 3, 30, antes.UpdatedAt, models.Movement{})
	if !errors.Is(err, ErrInvalidMovement) {
		t.Fatalf("error = %v; se esperaba ErrInvalidMovement", err)
	}
	if !reflect.DeepEqual(sheetRows(t, path, SheetInventory), inventarioAntes) {
		t.Error("Inventario cambió aunque el movimiento falló")
	}
}

// El movimiento debe describir el cambio que realmente se guarda: si no, se
// rechaza y ninguna de las dos hojas cambia (Inventario ↔ Movimientos).
func TestMovimientoQueNoCoincideConElCambio(t *testing.T) {
	tests := []struct {
		name     string
		cantidad int // el ítem 3 tiene 12
		mov      models.Movement
	}{
		{"entrada con otra magnitud", 15, stockMovement(models.MovementStockIn, 2)},
		{"entrada que en realidad baja", 10, stockMovement(models.MovementStockIn, 2)},
		{"salida que en realidad sube", 14, stockMovement(models.MovementStockOut, 2)},
		{"sin cambio que cambia", 13, stockMovement(models.MovementStockUpdate, 0)},
		{"ajuste con otra magnitud", 20, stockMovement(models.MovementAdjustment, 3)},
		{"transferencia que cambia la cantidad", 11, models.Movement{MovementType: models.MovementTransfer,
			Quantity: 12, OriginLocation: "Sistemas", DestinationLocation: "Depósito"}},
		{"tipo desconocido", 13, stockMovement("robo", 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, path := newWriteRepo(t)
			antes := leerBytes(t, path)
			actual := mustGet(t, repo, 3)
			_, err := repo.UpdateStock(context.Background(), 3, tt.cantidad, actual.UpdatedAt, tt.mov)
			if !errors.Is(err, ErrInvalidMovement) {
				t.Fatalf("error = %v; se esperaba ErrInvalidMovement", err)
			}
			assertIntacto(t, path, antes)
		})
	}
}

// Transferencia: Update cambia la ubicación y registra el movimiento en la misma escritura.
func TestUpdate_Transferencia(t *testing.T) {
	repo, path := newWriteRepo(t)
	antes := mustGet(t, repo, 3)
	movido := antes
	movido.Location = "Depósito"
	mov := models.Movement{MovementType: models.MovementTransfer, Quantity: antes.Quantity,
		OriginLocation: antes.Location, DestinationLocation: "Depósito"}
	updated, err := repo.Update(context.Background(), movido, antes.UpdatedAt, &mov)
	must(t, err)

	releido, err := NewExcelRepository(path)
	must(t, err)
	if it := mustGet(t, releido, 3); it.Location != "Depósito" || it.Quantity != antes.Quantity {
		t.Errorf("ítem guardado = %+v", it)
	}
	movs := mustMovements(t, releido, 3)
	ultimo := movs[len(movs)-1]
	if ultimo.MovementType != models.MovementTransfer || ultimo.PreviousQuantity != 12 || ultimo.NewQuantity != 12 ||
		ultimo.OriginLocation != "Sistemas" || ultimo.DestinationLocation != "Depósito" || !ultimo.CreatedAt.Equal(updated.UpdatedAt) {
		t.Errorf("movimiento = %+v", ultimo)
	}
	assertValido(t, path)
}

// ============================================================
// Update y Delete con respecto al historial
// ============================================================

func TestUpdate_ConMovimiento(t *testing.T) {
	repo, _ := newWriteRepo(t)
	ctx := context.Background()
	antes := mustGet(t, repo, 3)

	cambio := antes
	cambio.Quantity = 18
	mov := models.Movement{MovementType: models.MovementAdjustment, Quantity: 6, Notes: "Recuento"}
	updated, err := repo.Update(ctx, cambio, antes.UpdatedAt, &mov)
	must(t, err)

	movs := mustMovements(t, repo, 3)
	ultimo := movs[len(movs)-1]
	if ultimo.MovementType != models.MovementAdjustment || ultimo.PreviousQuantity != 12 || ultimo.NewQuantity != 18 ||
		ultimo.Quantity != 6 || ultimo.Notes != "Recuento" || !ultimo.CreatedAt.Equal(updated.UpdatedAt) {
		t.Errorf("movimiento = %+v", ultimo)
	}

	// Sin movimiento, Update no agrega nada al historial.
	sinCambio := updated
	sinCambio.Notes = "otra nota"
	_, err = repo.Update(ctx, sinCambio, updated.UpdatedAt, nil)
	must(t, err)
	if len(mustMovements(t, repo, 3)) != len(movs) {
		t.Error("Update sin movimiento no debería registrar historial")
	}
}

func TestDelete_ConservaMovimientos(t *testing.T) {
	repo, _ := newWriteRepo(t)
	ctx := context.Background()
	antes := mustMovements(t, repo, 3)

	must(t, repo.Delete(ctx, 3, mustGet(t, repo, 3).UpdatedAt))

	if _, err := repo.GetByID(ctx, 3); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("el ítem 3 debería haberse eliminado; error = %v", err)
	}
	if despues := mustMovements(t, repo, 3); !reflect.DeepEqual(despues, antes) {
		t.Errorf("movimientos del ítem eliminado = %+v; se esperaba conservar %+v", despues, antes)
	}
}
