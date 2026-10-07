package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"inventario/internal/models"
	"inventario/internal/repository"
)

var (
	ctx       = context.Background()
	createdAt = time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	loanedAt  = time.Date(2026, 9, 15, 9, 30, 0, 0, time.UTC)
)

// seed: dos equipos individuales (el 1 prestado) y un consumible.
func seed() []models.Item {
	return []models.Item{
		{ID: 1, InventoryNumber: "INV-001", HasInventory: true, DeviceType: "Notebook", Brand: "Dell",
			Model: "Latitude 5420", SerialNumber: "SN-AAA", Quantity: 1, Location: "Administración",
			Status: models.StatusOperational, CreatedAt: createdAt, UpdatedAt: createdAt,
			Availability: models.Loaned, AssignedTo: "Ana", LoanedAt: &loanedAt},
		{ID: 2, InventoryNumber: "INV-002", HasInventory: true, DeviceType: "Monitor", Brand: "Samsung",
			Model: "S24R350", SerialNumber: "SN-BBB", Quantity: 1, Location: "Sistemas",
			Status: models.StatusInRepair, CreatedAt: createdAt, UpdatedAt: createdAt, Availability: models.Available},
		{ID: 3, DeviceType: "Mouse", Brand: "Logitech", Model: "M280", Quantity: 10, Location: "Sistemas",
			Status: models.StatusOperational, CreatedAt: createdAt, UpdatedAt: createdAt, Availability: models.Available},
	}
}

func newTestService(items ...models.Item) (InventoryService, *fakeRepository) {
	repo := newFakeRepository(items...)
	return NewInventoryService(repo), repo
}

// validItem es un alta correcta; cada caso de prueba modifica un campo.
func validItem() models.Item {
	return models.Item{
		InventoryNumber: "INV-100", HasInventory: true, DeviceType: "Notebook", Brand: "Lenovo",
		Model: "ThinkPad T14", SerialNumber: "SN-NEW", Quantity: 1, Location: "Sistemas",
		Status: models.StatusOperational,
	}
}

func assertErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v; se esperaba %v", err, want)
	}
}

// ============================================================
// Create
// ============================================================

func TestCreate_ItemValido(t *testing.T) {
	svc, repo := newTestService(seed()...)
	input := validItem()
	input.ID, input.CreatedAt = 99, createdAt // se ignoran
	input.Availability, input.AssignedTo = models.Loaned, "Alguien"

	created, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != 4 {
		t.Errorf("ID = %d; se esperaba 4 (lo asigna el repositorio, no el cliente)", created.ID)
	}
	if created.CreatedAt.Equal(createdAt) || created.CreatedAt.IsZero() {
		t.Errorf("CreatedAt = %v; debería asignarlo el repositorio", created.CreatedAt)
	}
	if created.Availability != models.Available || created.AssignedTo != "" || created.LoanedAt != nil {
		t.Errorf("un ítem nuevo no debería nacer prestado: %+v", created)
	}
	if created.Brand != "Lenovo" || created.SerialNumber != "SN-NEW" || created.Location != "Sistemas" {
		t.Errorf("no conservó los datos: %+v", created)
	}
	if len(repo.items) != 4 {
		t.Errorf("el repositorio tiene %d ítems; se esperaban 4", len(repo.items))
	}
}

func TestCreate_Rechaza(t *testing.T) {
	tests := []struct {
		name   string
		change func(*models.Item)
		want   error
	}{
		{"DeviceType vacío", func(it *models.Item) { it.DeviceType = "" }, ErrInvalidItem},
		{"Brand solo con espacios", func(it *models.Item) { it.Brand = "   " }, ErrInvalidItem},
		{"Model vacío", func(it *models.Item) { it.Model = "" }, ErrInvalidItem},
		{"Location vacío", func(it *models.Item) { it.Location = "" }, ErrInvalidItem},
		{"Status vacío", func(it *models.Item) { it.Status = "" }, ErrInvalidItem},
		{"Status desconocido", func(it *models.Item) { it.Status = "ROTO" }, ErrInvalidItem},
		{"Quantity negativa", func(it *models.Item) { it.Quantity = -1 }, ErrInvalidQuantity},
		{"HasInventory sin InventoryNumber", func(it *models.Item) { it.InventoryNumber = " " }, ErrInventoryNumberRequired},
		{"InventoryNumber sin HasInventory", func(it *models.Item) { it.HasInventory = false }, ErrInvalidItem},
		{"InventoryNumber existente", func(it *models.Item) { it.InventoryNumber = "INV-001" }, ErrInventoryNumberExists},
		{"InventoryNumber existente en otro caso", func(it *models.Item) { it.InventoryNumber = " inv-001 " }, ErrInventoryNumberExists},
		{"SerialNumber duplicado", func(it *models.Item) { it.SerialNumber = "sn-bbb" }, ErrSerialNumberExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTestService(seed()...)
			item := validItem()
			tt.change(&item)

			_, err := svc.Create(ctx, item)
			assertErr(t, err, tt.want)
			if repo.called("Create") {
				t.Error("no debería llamar a repository.Create con datos inválidos")
			}
		})
	}
}

func TestCreate_ErroresDeCantidadYNumeroSonDeItemInvalido(t *testing.T) {
	// Los handlers pueden responder 400 con un único errors.Is(err, ErrInvalidItem).
	for _, err := range []error{ErrInvalidQuantity, ErrInventoryNumberRequired} {
		if !errors.Is(err, ErrInvalidItem) {
			t.Errorf("%v debería envolver ErrInvalidItem", err)
		}
	}
}

func TestCreate_NormalizaEspacios(t *testing.T) {
	svc, _ := newTestService(seed()...)
	item := validItem()
	item.Brand, item.Model, item.Location = "   Lenovo   ", "  ThinkPad  T14 ", "\tSistemas\n"
	item.InventoryNumber, item.SerialNumber, item.Notes = " INV-100 ", " SN-NEW ", "  Nuevo  "

	created, err := svc.Create(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Lenovo", "ThinkPad  T14", "Sistemas", "INV-100", "SN-NEW", "Nuevo"}
	got := []string{created.Brand, created.Model, created.Location, created.InventoryNumber, created.SerialNumber, created.Notes}
	if !slices.Equal(got, want) {
		t.Errorf("normalizado = %q; se esperaba %q (espacios internos y mayúsculas se conservan)", got, want)
	}
}

func TestCreate_ConsumiblesSinNumerosNoChocan(t *testing.T) {
	svc, _ := newTestService(seed()...) // el ítem 3 ya es un consumible sin N° ni serie
	item := validItem()
	item.HasInventory, item.InventoryNumber, item.SerialNumber, item.Quantity = false, "", "", 25

	if _, err := svc.Create(ctx, item); err != nil {
		t.Errorf("dos ítems con número de inventario y serie vacíos no deberían chocar: %v", err)
	}
}

func TestCreate_ConcurrenteConMismoNumero(t *testing.T) {
	svc, repo := newTestService(seed()...)
	repo.getAllWait = 5 * time.Millisecond // amplía la ventana entre verificar y guardar

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item := validItem()
			item.SerialNumber = fmt.Sprintf("SN-%d", i)
			_, errs[i] = svc.Create(ctx, item)
		}()
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrInventoryNumberExists):
			t.Errorf("error inesperado: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("se crearon %d ítems con el mismo número de inventario; se esperaba 1", ok)
	}
}

// ============================================================
// Update
// ============================================================

func TestUpdate_ItemValido(t *testing.T) {
	svc, repo := newTestService(seed()...)
	changes := repo.items[1] // ítem 2
	changes.ID = 0           // el ID lo define el parámetro
	changes.Brand, changes.Location, changes.Status = " LG ", "Dirección", models.StatusOperational
	changes.CreatedAt = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC) // se ignora

	updated, err := svc.Update(ctx, 2, changes)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != 2 {
		t.Errorf("ID = %d; se esperaba 2", updated.ID)
	}
	if !updated.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt = %v; se esperaba conservar %v", updated.CreatedAt, createdAt)
	}
	if !updated.UpdatedAt.After(createdAt) {
		t.Errorf("UpdatedAt = %v; debería avanzar", updated.UpdatedAt)
	}
	if updated.Brand != "LG" || updated.Location != "Dirección" || updated.Status != models.StatusOperational {
		t.Errorf("no aplicó los cambios: %+v", updated)
	}
}

func TestUpdate_ConservaDatosDePrestamo(t *testing.T) {
	svc, repo := newTestService(seed()...)
	changes := repo.items[0] // ítem 1, prestado a Ana
	changes.Notes = "Batería nueva"
	changes.Availability, changes.AssignedTo, changes.LoanedAt = "", "", nil // como llegaría del formulario

	updated, err := svc.Update(ctx, 1, changes)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Availability != models.Loaned || updated.AssignedTo != "Ana" || updated.LoanedAt == nil {
		t.Errorf("editar no debería borrar el préstamo: %+v", updated)
	}
}

func TestUpdate_Rechaza(t *testing.T) {
	tests := []struct {
		name   string
		id     int
		change func(*models.Item)
		want   error
	}{
		{"ID cero", 0, nil, ErrInvalidID},
		{"ID negativo", -4, nil, ErrInvalidID},
		{"cambio de ID", 2, func(it *models.Item) { it.ID = 5 }, ErrInvalidID},
		{"ID inexistente", 99, nil, repository.ErrItemNotFound},
		{"InventoryNumber de otro ítem", 2, func(it *models.Item) { it.InventoryNumber = "inv-001" }, ErrInventoryNumberExists},
		{"SerialNumber de otro ítem", 2, func(it *models.Item) { it.SerialNumber = "SN-aaa" }, ErrSerialNumberExists},
		{"Brand vacío", 2, func(it *models.Item) { it.Brand = "" }, ErrInvalidItem},
		{"Quantity negativa", 2, func(it *models.Item) { it.Quantity = -3 }, ErrInvalidQuantity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTestService(seed()...)
			item := repo.items[1]
			item.ID = 0 // como llega del formulario: el ID va en la ruta
			if tt.change != nil {
				tt.change(&item)
			}
			_, err := svc.Update(ctx, tt.id, item)
			assertErr(t, err, tt.want)
			if repo.called("Update") {
				t.Error("no debería llamar a repository.Update")
			}
		})
	}
}

func TestUpdate_PermiteConservarSusPropiosNumeros(t *testing.T) {
	svc, repo := newTestService(seed()...)
	item := repo.items[1]
	item.InventoryNumber, item.SerialNumber = "inv-002", "sn-bbb" // los suyos, en otro caso
	item.Notes = "Sin cambios de identificación"

	if _, err := svc.Update(ctx, 2, item); err != nil {
		t.Errorf("un ítem debería poder conservar su propio número de inventario y serie: %v", err)
	}
}

// ============================================================
// Delete
// ============================================================

func TestDelete(t *testing.T) {
	svc, repo := newTestService(seed()...)
	if err := svc.Delete(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetByID(ctx, 2); !errors.Is(err, repository.ErrItemNotFound) {
		t.Errorf("el ítem 2 debería haberse eliminado; error = %v", err)
	}
	if len(repo.items) != 2 {
		t.Errorf("quedan %d ítems; se esperaban 2", len(repo.items))
	}
}

func TestDelete_Rechaza(t *testing.T) {
	for _, tt := range []struct {
		id   int
		want error
	}{{0, ErrInvalidID}, {-1, ErrInvalidID}, {99, repository.ErrItemNotFound}} {
		t.Run(fmt.Sprint(tt.id), func(t *testing.T) {
			svc, repo := newTestService(seed()...)
			assertErr(t, svc.Delete(ctx, tt.id), tt.want)
			if repo.called("Delete") {
				t.Error("no debería llamar a repository.Delete")
			}
		})
	}
}

// ============================================================
// UpdateStock
// ============================================================

func TestUpdateStock(t *testing.T) {
	svc, repo := newTestService(seed()...)
	before := repo.items[2]

	updated, err := svc.UpdateStock(ctx, 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Quantity != 4 {
		t.Errorf("Quantity = %d; se esperaba 4", updated.Quantity)
	}
	want := before
	want.Quantity, want.UpdatedAt = 4, updated.UpdatedAt
	if updated != want {
		t.Errorf("solo debería cambiar Quantity y UpdatedAt:\n%+v\n%+v", updated, want)
	}
}

func TestUpdateStock_PermiteCero(t *testing.T) {
	svc, _ := newTestService(seed()...)
	if _, err := svc.UpdateStock(ctx, 3, 0); err != nil {
		t.Errorf("stock 0 es válido: %v", err)
	}
}

func TestUpdateStock_Rechaza(t *testing.T) {
	for _, tt := range []struct {
		name         string
		id, quantity int
		want         error
	}{
		{"cantidad negativa", 3, -1, ErrInvalidQuantity},
		{"ID inválido", 0, 5, ErrInvalidID},
		{"ID inexistente", 99, 5, repository.ErrItemNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTestService(seed()...)
			_, err := svc.UpdateStock(ctx, tt.id, tt.quantity)
			assertErr(t, err, tt.want)
			if repo.called("UpdateStock") {
				t.Error("no debería llamar a repository.UpdateStock")
			}
		})
	}
}

// ============================================================
// Consultas
// ============================================================

func TestGetAll_Delega(t *testing.T) {
	svc, _ := newTestService(seed()...)
	items, err := svc.GetAll(ctx)
	if err != nil || len(items) != 3 {
		t.Errorf("GetAll = %d ítems, %v; se esperaban 3", len(items), err)
	}
}

func TestGetByID_Delega(t *testing.T) {
	svc, _ := newTestService(seed()...)
	item, err := svc.GetByID(ctx, 2)
	if err != nil || item.Brand != "Samsung" {
		t.Errorf("GetByID(2) = %q, %v", item.Brand, err)
	}
	_, err = svc.GetByID(ctx, 0)
	assertErr(t, err, ErrInvalidID)
}

func TestSearch_Delega(t *testing.T) {
	svc, repo := newTestService(seed()...)
	filter := models.ItemFilter{Text: "dell", Location: "Sistemas", IncludeRetired: true}

	items, err := svc.Search(ctx, filter)
	if err != nil || len(items) != 3 {
		t.Fatalf("Search = %d ítems, %v", len(items), err)
	}
	if repo.lastFilter != filter {
		t.Errorf("el filtro llegó al repositorio como %+v; se esperaba %+v", repo.lastFilter, filter)
	}
}

func TestCheckHealth_Delega(t *testing.T) {
	svc, repo := newTestService(seed()...)
	if err := svc.CheckHealth(ctx); err != nil || !repo.called("Check") {
		t.Errorf("CheckHealth = %v; debería delegar en repository.Check", err)
	}
	repo.failWith = fmt.Errorf("%w: \"Movimientos\"", repository.ErrSheetMissing)
	assertErr(t, svc.CheckHealth(ctx), repository.ErrSheetMissing)
}

func TestErroresDelRepositorioSeConservan(t *testing.T) {
	svc, repo := newTestService(seed()...)
	repo.failWith = fmt.Errorf("%w: faltan hojas", repository.ErrInvalidWorkbook)

	_, err := svc.GetAll(ctx)
	assertErr(t, err, repository.ErrInvalidWorkbook)
	_, err = svc.Create(ctx, validItem())
	assertErr(t, err, repository.ErrInvalidWorkbook)
}
