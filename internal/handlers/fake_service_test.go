package handlers

import (
	"context"
	"io"

	"inventario/internal/models"
	"inventario/internal/services"
)

// fakeService implementa services.InventoryService: registra con qué
// argumentos lo llamaron y devuelve lo configurado.
type fakeService struct {
	items      []models.Item     // resultado de GetAll, Search y Query
	pagination models.Pagination // paginación que devuelve Query
	movements  []models.Movement // resultado de GetMovements y GetMovementsByItemID
	item       models.Item       // resultado de GetByID, Create, Update y UpdateStock
	err        error             // si no es nil, todas las operaciones lo devuelven
	panic      bool              // si es true, Query entra en panic

	calls        []string
	gotID        int
	gotItem      models.Item
	gotFilter    models.ItemFilter
	gotQuery     models.ItemQuery
	gotQuantity  int
	gotOperation models.StockOperation
	exportBytes  []byte
	imported     int
}

var _ services.InventoryService = (*fakeService)(nil)

func (f *fakeService) Summary(context.Context) (models.InventorySummary, error) {
	f.calls = append(f.calls, "Summary")
	return models.InventorySummary{}, f.err
}

func (f *fakeService) Report(_ context.Context, filter models.ItemFilter) (models.InventoryReport, error) {
	f.calls, f.gotFilter = append(f.calls, "Report"), filter
	return models.InventoryReport{}, f.err
}

func (f *fakeService) called(method string) bool {
	for _, c := range f.calls {
		if c == method {
			return true
		}
	}
	return false
}

func (f *fakeService) GetAll(context.Context) ([]models.Item, error) {
	f.calls = append(f.calls, "GetAll")
	return f.items, f.err
}

func (f *fakeService) GetByID(_ context.Context, id int) (models.Item, error) {
	f.calls, f.gotID = append(f.calls, "GetByID"), id
	return f.item, f.err
}

func (f *fakeService) Search(_ context.Context, filter models.ItemFilter) ([]models.Item, error) {
	f.calls, f.gotFilter = append(f.calls, "Search"), filter
	return f.items, f.err
}

func (f *fakeService) Create(_ context.Context, item models.Item) (models.Item, error) {
	f.calls, f.gotItem = append(f.calls, "Create"), item
	return f.item, f.err
}

func (f *fakeService) Update(_ context.Context, id int, item models.Item) (models.Item, error) {
	f.calls, f.gotID, f.gotItem = append(f.calls, "Update"), id, item
	return f.item, f.err
}

func (f *fakeService) Delete(_ context.Context, id int) error {
	f.calls, f.gotID = append(f.calls, "Delete"), id
	return f.err
}

func (f *fakeService) UpdateStock(_ context.Context, id, quantity int) (models.Item, error) {
	f.calls, f.gotID, f.gotQuantity = append(f.calls, "UpdateStock"), id, quantity
	return f.item, f.err
}

func (f *fakeService) CheckHealth(context.Context) error {
	f.calls = append(f.calls, "CheckHealth")
	return f.err
}

func (f *fakeService) GetMovements(context.Context) ([]models.Movement, error) {
	f.calls = append(f.calls, "GetMovements")
	return f.movements, f.err
}

func (f *fakeService) GetMovementsByItemID(_ context.Context, itemID int) ([]models.Movement, error) {
	f.calls, f.gotID = append(f.calls, "GetMovementsByItemID"), itemID
	return f.movements, f.err
}

func (f *fakeService) Query(_ context.Context, q models.ItemQuery) (models.ItemPage, error) {
	f.calls, f.gotQuery = append(f.calls, "Query"), q
	if f.panic {
		panic("falla inesperada")
	}
	return models.ItemPage{Items: f.items, Pagination: f.pagination}, f.err
}

func (f *fakeService) ApplyMovement(_ context.Context, id int, op models.StockOperation) (models.Item, error) {
	f.calls, f.gotID, f.gotOperation = append(f.calls, "ApplyMovement"), id, op
	return f.item, f.err
}

func (f *fakeService) ExportInventory(context.Context) ([]byte, error) {
	f.calls = append(f.calls, "ExportInventory")
	return f.exportBytes, f.err
}

func (f *fakeService) ImportInventory(_ context.Context, _ io.Reader) (int, error) {
	f.calls = append(f.calls, "ImportInventory")
	return f.imported, f.err
}
