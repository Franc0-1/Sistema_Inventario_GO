package services

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"inventario/internal/models"
	"inventario/internal/repository"
)

// fakeRepository implementa repository.InventoryRepository en memoria, con
// la misma semántica observable que el repositorio de Excel: asigna ID y
// fechas, controla la versión y devuelve los mismos errores.
type fakeRepository struct {
	mu        sync.Mutex
	items     []models.Item
	movements []models.Movement
	lastID    int
	clock     time.Time

	calls       []string          // métodos llamados, en orden
	lastFilter  models.ItemFilter // último filtro recibido por Search
	failWith    error             // si no es nil, toda llamada lo devuelve
	getAllWait  time.Duration     // pausa en GetAll para exponer carreras
	beforeWrite func()            // si no es nil, corre (con el lock) antes de Update y UpdateStock
}

var _ Repository = (*fakeRepository)(nil)

func newFakeRepository(items ...models.Item) *fakeRepository {
	f := &fakeRepository{clock: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)}
	for _, it := range items {
		f.items = append(f.items, it)
		f.lastID = max(f.lastID, it.ID)
	}
	return f
}

func (f *fakeRepository) record(call string) error {
	f.calls = append(f.calls, call)
	return f.failWith
}

func (f *fakeRepository) tick() time.Time {
	f.clock = f.clock.Add(time.Minute)
	return f.clock
}

func (f *fakeRepository) index(id int) (int, error) {
	i := slices.IndexFunc(f.items, func(it models.Item) bool { return it.ID == id })
	if i < 0 {
		return 0, fmt.Errorf("%w: id %d", repository.ErrItemNotFound, id)
	}
	return i, nil
}

func (f *fakeRepository) checkVersion(i int, expected time.Time) error {
	if !f.items[i].UpdatedAt.Equal(expected) {
		return repository.ErrConflict
	}
	return nil
}

func (f *fakeRepository) GetAll(context.Context) ([]models.Item, error) {
	f.mu.Lock()
	err := f.record("GetAll")
	items := slices.Clone(f.items)
	f.mu.Unlock()
	time.Sleep(f.getAllWait)
	return items, err
}

func (f *fakeRepository) GetByID(_ context.Context, id int) (models.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("GetByID"); err != nil {
		return models.Item{}, err
	}
	i, err := f.index(id)
	if err != nil {
		return models.Item{}, err
	}
	return f.items[i], nil
}

func (f *fakeRepository) Search(_ context.Context, filter models.ItemFilter) ([]models.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastFilter = filter
	return slices.Clone(f.items), f.record("Search")
}

func (f *fakeRepository) Create(_ context.Context, item models.Item) (models.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("Create"); err != nil {
		return models.Item{}, err
	}
	f.lastID++
	item.ID = f.lastID
	item.CreatedAt = f.tick()
	item.UpdatedAt = item.CreatedAt
	f.items = append(f.items, item)
	return item, nil
}

func (f *fakeRepository) Update(_ context.Context, item models.Item, expected time.Time, mov *models.Movement) (models.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beforeWrite != nil {
		f.beforeWrite()
	}
	if err := f.record("Update"); err != nil {
		return models.Item{}, err
	}
	i, err := f.index(item.ID)
	if err != nil {
		return models.Item{}, err
	}
	if err := f.checkVersion(i, expected); err != nil {
		return models.Item{}, err
	}
	before := f.items[i]
	item.CreatedAt, item.UpdatedAt = before.CreatedAt, f.tick()
	f.items[i] = item
	if mov != nil {
		f.appendStockMovement(*mov, before, item)
	}
	return item, nil
}

func (f *fakeRepository) Delete(_ context.Context, id int, expected time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("Delete"); err != nil {
		return err
	}
	i, err := f.index(id)
	if err != nil {
		return err
	}
	if err := f.checkVersion(i, expected); err != nil {
		return err
	}
	f.items = slices.Delete(f.items, i, i+1)
	return nil
}

func (f *fakeRepository) UpdateStock(_ context.Context, id, quantity int, expected time.Time, mov models.Movement) (models.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beforeWrite != nil {
		f.beforeWrite()
	}
	if err := f.record("UpdateStock"); err != nil {
		return models.Item{}, err
	}
	i, err := f.index(id)
	if err != nil {
		return models.Item{}, err
	}
	if err := f.checkVersion(i, expected); err != nil {
		return models.Item{}, err
	}
	before := f.items[i]
	f.items[i].Quantity, f.items[i].UpdatedAt = quantity, f.tick()
	f.appendStockMovement(mov, before, f.items[i])
	return f.items[i], nil
}

// ---- Movimientos: misma semántica que el repositorio de Excel ----

// appendStockMovement completa el movimiento con lo realmente escrito (como
// el repositorio real). Se llama con f.mu tomado.
func (f *fakeRepository) appendStockMovement(mov models.Movement, before, after models.Item) {
	mov.ItemID, mov.PreviousQuantity, mov.NewQuantity, mov.CreatedAt = after.ID, before.Quantity, after.Quantity, after.UpdatedAt
	mov.ID = len(f.movements) + 1
	f.movements = append(f.movements, mov)
}

func (f *fakeRepository) GetMovements(context.Context) ([]models.Movement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.movements), f.record("GetMovements")
}

func (f *fakeRepository) GetMovementsByItemID(_ context.Context, itemID int) ([]models.Movement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("GetMovementsByItemID"); err != nil {
		return nil, err
	}
	out := []models.Movement{}
	for _, m := range f.movements {
		if m.ItemID == itemID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeRepository) CreateMovement(_ context.Context, mov models.Movement) (models.Movement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("CreateMovement"); err != nil {
		return models.Movement{}, err
	}
	mov.ID, mov.CreatedAt = len(f.movements)+1, f.tick()
	f.movements = append(f.movements, mov)
	return mov, nil
}

// lastMovement devuelve el último movimiento registrado (o false si no hay).
func (f *fakeRepository) lastMovement() (models.Movement, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.movements) == 0 {
		return models.Movement{}, false
	}
	return f.movements[len(f.movements)-1], true
}

func (f *fakeRepository) Check(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.record("Check")
}

func (f *fakeRepository) ExportInventory(context.Context) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("ExportInventory"); err != nil {
		return nil, err
	}
	return []byte("xlsx"), nil
}

func (f *fakeRepository) ImportInventory(_ context.Context, _ io.Reader) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("ImportInventory"); err != nil {
		return 0, err
	}
	return len(f.items), nil
}

// called informa si el método se llamó al menos una vez.
func (f *fakeRepository) called(method string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.calls, method)
}
