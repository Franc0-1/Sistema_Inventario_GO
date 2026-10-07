package services

import (
	"errors"
	"slices"
	"testing"

	"inventario/internal/models"
	"inventario/internal/repository"
)

// ============================================================
// UpdateStock registra el movimiento correcto
// ============================================================

func TestUpdateStock_RegistraMovimiento(t *testing.T) {
	svc, repo := newTestService(seed()...) // el ítem 3 (consumible) tiene 10 unidades

	pasos := []struct {
		nombre   string
		nueva    int
		tipo     models.MovementType
		magnitud int
		anterior int
	}{
		{"entrada 10 -> 15", 15, models.MovementStockIn, 5, 10},
		{"salida 15 -> 10", 10, models.MovementStockOut, 5, 15},
		{"sin cambio 10 -> 10", 10, models.MovementStockUpdate, 0, 10},
	}
	for _, p := range pasos {
		t.Run(p.nombre, func(t *testing.T) {
			item, err := svc.UpdateStock(ctx, 3, p.nueva)
			if err != nil {
				t.Fatal(err)
			}
			mov, ok := repo.lastMovement()
			if !ok {
				t.Fatal("no se registró ningún movimiento")
			}
			want := models.Movement{ID: mov.ID, ItemID: 3, MovementType: p.tipo, Quantity: p.magnitud,
				PreviousQuantity: p.anterior, NewQuantity: p.nueva, CreatedAt: item.UpdatedAt}
			if mov != want {
				t.Errorf("movimiento =\n%+v\nse esperaba\n%+v", mov, want)
			}
			if item.Quantity != p.nueva {
				t.Errorf("Quantity = %d; se esperaba %d", item.Quantity, p.nueva)
			}
		})
	}
}

func TestUpdateStock_RechazosNoRegistranMovimiento(t *testing.T) {
	tests := []struct {
		name         string
		id, cantidad int
		want         error
	}{
		{"stock negativo", 3, -1, ErrInvalidQuantity},
		{"ítem inexistente", 99, 5, repository.ErrItemNotFound},
		{"ID inválido", 0, 5, ErrInvalidID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTestService(seed()...)
			_, err := svc.UpdateStock(ctx, tt.id, tt.cantidad)
			assertErr(t, err, tt.want)
			if _, ok := repo.lastMovement(); ok || repo.called("UpdateStock") {
				t.Error("no debería modificar el stock ni registrar movimientos")
			}
		})
	}
}

// ============================================================
// Update: un cambio de cantidad también queda en el historial
// ============================================================

func TestUpdate_CambioDeCantidadRegistraAjuste(t *testing.T) {
	svc, repo := newTestService(seed()...)
	item := repo.items[2] // ítem 3, 10 unidades
	item.ID, item.Quantity = 0, 7

	updated, err := svc.Update(ctx, 3, item)
	if err != nil {
		t.Fatal(err)
	}
	mov, ok := repo.lastMovement()
	want := models.Movement{ID: mov.ID, ItemID: 3, MovementType: models.MovementAdjustment, Quantity: 3,
		PreviousQuantity: 10, NewQuantity: 7, CreatedAt: updated.UpdatedAt}
	if !ok || mov != want {
		t.Errorf("movimiento =\n%+v\nse esperaba\n%+v", mov, want)
	}

	// Editar sin cambiar la cantidad no genera historial.
	updated.ID, updated.Notes = 0, "Solo cambia la nota"
	if _, err := svc.Update(ctx, 3, updated); err != nil {
		t.Fatal(err)
	}
	if movs, _ := repo.GetMovements(ctx); len(movs) != 1 {
		t.Errorf("hay %d movimientos; se esperaba 1", len(movs))
	}
}

// ============================================================
// Consultas del historial
// ============================================================

func TestGetMovements(t *testing.T) {
	svc, repo := newTestService(seed()...)
	repo.movements = []models.Movement{
		{ID: 1, ItemID: 3, MovementType: models.MovementStockOut, Quantity: 2, PreviousQuantity: 12, NewQuantity: 10},
		{ID: 2, ItemID: 1, MovementType: models.MovementTransfer, Quantity: 1, PreviousQuantity: 1, NewQuantity: 1},
	}

	movs, err := svc.GetMovements(ctx)
	if err != nil || len(movs) != 2 || movs[1].MovementType != models.MovementTransfer {
		t.Errorf("GetMovements = %+v, %v", movs, err)
	}
}

func TestGetMovementsByItemID(t *testing.T) {
	svc, repo := newTestService(seed()...)
	repo.movements = []models.Movement{
		{ID: 1, ItemID: 3, MovementType: models.MovementStockOut, Quantity: 2, PreviousQuantity: 12, NewQuantity: 10},
		{ID: 2, ItemID: 50, MovementType: models.MovementStockIn, Quantity: 4, PreviousQuantity: 0, NewQuantity: 4}, // ítem ya eliminado
		{ID: 3, ItemID: 3, MovementType: models.MovementStockIn, Quantity: 1, PreviousQuantity: 10, NewQuantity: 11},
	}

	tests := []struct {
		name    string
		itemID  int
		wantIDs []int
		wantErr error
	}{
		{"solo los del ítem", 3, []int{1, 3}, nil},
		{"ítem existente sin movimientos: lista vacía", 2, []int{}, nil},
		{"ítem eliminado con historial", 50, []int{2}, nil},
		{"ítem inexistente sin historial", 99, nil, repository.ErrItemNotFound},
		{"ID inválido", 0, nil, ErrInvalidID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movs, err := svc.GetMovementsByItemID(ctx, tt.itemID)
			if tt.wantErr != nil {
				assertErr(t, err, tt.wantErr)
				return
			}
			if err != nil || movs == nil {
				t.Fatalf("GetMovementsByItemID = %v, %v", movs, err)
			}
			got := []int{}
			for _, m := range movs {
				got = append(got, m.ID)
			}
			if !slices.Equal(got, tt.wantIDs) {
				t.Errorf("IDs = %v; se esperaba %v", got, tt.wantIDs)
			}
		})
	}
}

// ============================================================
// Validación de movimientos
// ============================================================

func TestValidateMovement(t *testing.T) {
	base := models.Movement{ItemID: 3, MovementType: models.MovementStockIn, Quantity: 5, PreviousQuantity: 10, NewQuantity: 15}
	con := func(cambio func(*models.Movement)) models.Movement {
		m := base
		cambio(&m)
		return m
	}

	validos := []models.Movement{
		base,
		{ItemID: 3, MovementType: models.MovementStockOut, Quantity: 5, PreviousQuantity: 15, NewQuantity: 10},
		{ItemID: 3, MovementType: models.MovementStockUpdate, PreviousQuantity: 10, NewQuantity: 10},
		{ItemID: 3, MovementType: models.MovementAdjustment, Quantity: 3, PreviousQuantity: 10, NewQuantity: 7},
		{ItemID: 1, MovementType: models.MovementTransfer, Quantity: 1, PreviousQuantity: 1, NewQuantity: 1,
			OriginLocation: "Sistemas", DestinationLocation: "Dirección"},
	}
	for _, m := range validos {
		if err := validateMovement(m); err != nil {
			t.Errorf("%s debería ser válido: %v", m.MovementType, err)
		}
	}

	invalidos := []struct {
		name string
		mov  models.Movement
		want error
	}{
		{"tipo desconocido", con(func(m *models.Movement) { m.MovementType = "robo" }), ErrInvalidMovement},
		{"tipo vacío", con(func(m *models.Movement) { m.MovementType = "" }), ErrInvalidMovement},
		{"ItemID inválido", con(func(m *models.Movement) { m.ItemID = 0 }), ErrInvalidID},
		{"stock nuevo negativo", con(func(m *models.Movement) { m.NewQuantity = -1 }), ErrInvalidQuantity},
		{"stock anterior negativo", con(func(m *models.Movement) { m.PreviousQuantity = -2 }), ErrInvalidQuantity},
		{"cantidad negativa", con(func(m *models.Movement) { m.Quantity = -5 }), ErrInvalidQuantity},
		{"entrada que baja el stock", con(func(m *models.Movement) { m.NewQuantity = 8 }), ErrInvalidMovement},
		{"magnitud que no coincide", con(func(m *models.Movement) { m.Quantity = 4 }), ErrInvalidMovement},
		{"salida que sube el stock", con(func(m *models.Movement) { m.MovementType = models.MovementStockOut }), ErrInvalidMovement},
		{"traslado sin destino", models.Movement{ItemID: 1, MovementType: models.MovementTransfer, OriginLocation: "Sistemas"}, ErrInvalidMovement},
		{"traslado al mismo lugar", models.Movement{ItemID: 1, MovementType: models.MovementTransfer,
			OriginLocation: "Sistemas", DestinationLocation: "sistemas"}, ErrInvalidMovement},
	}
	for _, tt := range invalidos {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateMovement(tt.mov); !errors.Is(err, tt.want) {
				t.Errorf("error = %v; se esperaba %v", err, tt.want)
			}
		})
	}
}
