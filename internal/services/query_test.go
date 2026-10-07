package services

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"inventario/internal/models"
)

func queryIDs(page models.ItemPage) []int {
	out := make([]int, len(page.Items))
	for i, it := range page.Items {
		out[i] = it.ID
	}
	return out
}

// numbered genera n ítems con IDs 1..n.
func numbered(n int) []models.Item {
	items := make([]models.Item, n)
	for i := range items {
		items[i] = models.Item{ID: i + 1, DeviceType: "Mouse", Brand: "Logitech", Model: "M280",
			Quantity: i, Location: "Sistemas", Status: models.StatusOperational}
	}
	return items
}

// ============================================================
// Validación (listas blancas y rangos)
// ============================================================

func TestQuery_RechazaValoresFueraDeLaListaBlanca(t *testing.T) {
	tests := map[string]models.ItemQuery{
		"campo de orden inexistente":         {Sort: "password"},
		"campo del modelo que no se ordena":  {Sort: "notes"},
		"nombre JSON en lugar del de la API": {Sort: "numero_inventario"},
		"sentido de orden":                   {Order: "up"},
		"estado desconocido":                 {Filter: models.ItemFilter{Status: "Disponible"}},
		"disponibilidad desconocida":         {Filter: models.ItemFilter{Availability: "LIBRE"}},
		"tamaño de página mayor al máximo":   {Page: 1, PageSize: models.MaxPageSize + 1},
		"página negativa":                    {Page: -1},
		"tamaño negativo":                    {PageSize: -5},
	}
	for name, q := range tests {
		t.Run(name, func(t *testing.T) {
			svc, repo := newTestService(seed()...)
			_, err := svc.Query(ctx, q)
			assertErr(t, err, ErrInvalidQuery)
			if len(repo.calls) > 0 {
				t.Errorf("con una consulta inválida no debería leer el repositorio; llamó a %v", repo.calls)
			}
		})
	}
}

func TestQuery_ElMensajeListaLosValoresValidos(t *testing.T) {
	svc, _ := newTestService(seed()...)
	_, err := svc.Query(ctx, models.ItemQuery{Sort: "password"})
	want := `consulta inválida: campo de orden "password" desconocido (válidos: id, inventoryNumber, deviceType, ` +
		`brand, model, quantity, location, status, createdAt, updatedAt)`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v\nse esperaba %s", err, want)
	}
}

// Los nombres de la lista blanca se aceptan sin distinguir mayúsculas y
// llegan al repositorio con su valor canónico.
func TestQuery_NormalizaEstadoYDisponibilidad(t *testing.T) {
	svc, repo := newTestService(seed()...)
	_, err := svc.Query(ctx, models.ItemQuery{
		Filter: models.ItemFilter{Status: " en_reparacion ", Availability: "disponible", Brand: "Dell"},
		Sort:   "BRAND", Order: "DESC",
	})
	if err != nil {
		t.Fatal(err)
	}
	f := repo.lastFilter
	if f.Status != models.StatusInRepair || f.Availability != models.Available || f.Brand != "Dell" {
		t.Errorf("filtro recibido por el repositorio = %+v", f)
	}
}

// ============================================================
// Paginación
// ============================================================

func TestQuery_SinPaginacionDevuelveTodo(t *testing.T) {
	svc, _ := newTestService(numbered(45)...)
	page, err := svc.Query(ctx, models.ItemQuery{})
	if err != nil {
		t.Fatal(err)
	}
	want := models.Pagination{Page: 1, PageSize: 45, TotalItems: 45, TotalPages: 1}
	if len(page.Items) != 45 || page.Pagination != want {
		t.Errorf("%d ítems, %+v; se esperaba 45 y %+v", len(page.Items), page.Pagination, want)
	}
}

func TestQuery_Paginas(t *testing.T) {
	tests := []struct {
		name           string
		page, pageSize int
		wantIDs        []int
		wantPagination models.Pagination
	}{
		{"primera", 1, 20, idRange(1, 20), models.Pagination{Page: 1, PageSize: 20, TotalItems: 45, TotalPages: 3}},
		{"intermedia", 2, 20, idRange(21, 40), models.Pagination{Page: 2, PageSize: 20, TotalItems: 45, TotalPages: 3}},
		{"última incompleta", 3, 20, idRange(41, 45), models.Pagination{Page: 3, PageSize: 20, TotalItems: 45, TotalPages: 3}},
		{"posterior a la última: vacía, no error", 4, 20, []int{}, models.Pagination{Page: 4, PageSize: 20, TotalItems: 45, TotalPages: 3}},
		{"solo page: tamaño por defecto", 2, 0, idRange(21, 40), models.Pagination{Page: 2, PageSize: models.DefaultPageSize, TotalItems: 45, TotalPages: 3}},
		{"solo pageSize: primera página", 0, 10, idRange(1, 10), models.Pagination{Page: 1, PageSize: 10, TotalItems: 45, TotalPages: 5}},
		{"tamaño máximo", 1, models.MaxPageSize, idRange(1, 45), models.Pagination{Page: 1, PageSize: models.MaxPageSize, TotalItems: 45, TotalPages: 1}},
		{"exacta", 3, 15, idRange(31, 45), models.Pagination{Page: 3, PageSize: 15, TotalItems: 45, TotalPages: 3}},
		{"página enorme no desborda", math.MaxInt, 100, []int{}, models.Pagination{Page: math.MaxInt, PageSize: 100, TotalItems: 45, TotalPages: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestService(numbered(45)...)
			page, err := svc.Query(ctx, models.ItemQuery{Page: tt.page, PageSize: tt.pageSize})
			if err != nil {
				t.Fatal(err)
			}
			if got := queryIDs(page); !slices.Equal(got, tt.wantIDs) {
				t.Errorf("IDs = %v; se esperaba %v", got, tt.wantIDs)
			}
			if page.Pagination != tt.wantPagination {
				t.Errorf("pagination = %+v; se esperaba %+v", page.Pagination, tt.wantPagination)
			}
		})
	}
}

func TestQuery_SinResultados(t *testing.T) {
	for _, q := range []models.ItemQuery{{}, {Page: 1, PageSize: 20}} {
		svc, _ := newTestService()
		page, err := svc.Query(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 || page.Pagination.TotalItems != 0 || page.Pagination.TotalPages != 0 {
			t.Errorf("Query(%+v) = %+v; se esperaba sin ítems y 0 páginas", q, page)
		}
	}
}

// Al paginar, cada ítem aparece en exactamente una página, también cuando
// hay valores repetidos en el campo de orden (el ID desempata).
func TestQuery_LasPaginasNoRepitenNiPierdenItems(t *testing.T) {
	items := numbered(23)
	for i := range items {
		items[i].Brand = []string{"HP", "Dell", "Lenovo"}[i%3]
	}
	svc, _ := newTestService(items...)
	var seen []int
	for p := 1; p <= 5; p++ {
		page, err := svc.Query(ctx, models.ItemQuery{Sort: models.SortByBrand, Page: p, PageSize: 5})
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, queryIDs(page)...)
	}
	slices.Sort(seen)
	if !slices.Equal(seen, idRange(1, 23)) {
		t.Errorf("IDs recorridos = %v", seen)
	}
}

func idRange(from, to int) []int {
	out := []int{}
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

// ============================================================
// Ordenamiento
// ============================================================

func sortFixture() []models.Item {
	day := func(d int) time.Time { return time.Date(2026, 9, d, 10, 0, 0, 0, time.UTC) }
	return []models.Item{
		{ID: 1, InventoryNumber: "B-2", DeviceType: "Notebook", Brand: "lenovo", Model: "T14", Quantity: 1,
			Location: "Sistemas", Status: models.StatusRetired, CreatedAt: day(3), UpdatedAt: day(9)},
		{ID: 2, InventoryNumber: "", DeviceType: "Mouse", Brand: "Logitech", Model: "M280", Quantity: 12,
			Location: "Administración", Status: models.StatusOperational, CreatedAt: day(1), UpdatedAt: day(5)},
		{ID: 3, InventoryNumber: "A-1", DeviceType: "Aire acondicionado", Brand: "Dell", Model: "", Quantity: 1,
			Location: "Recepción", Status: models.StatusInRepair, CreatedAt: day(2), UpdatedAt: day(2)},
		{ID: 4, InventoryNumber: "a-3", DeviceType: "Monitor", Brand: "Ácer", Model: "V22", Quantity: 0,
			Location: "", Status: models.StatusOutOfService, CreatedAt: day(4), UpdatedAt: day(1)},
	}
}

func TestQuery_Ordenamiento(t *testing.T) {
	tests := []struct {
		sort      models.SortField
		asc, desc []int
	}{
		{models.SortByID, []int{1, 2, 3, 4}, []int{4, 3, 2, 1}},
		// Sin distinguir mayúsculas ("a-3" entre "A-1" y "B-2"); el vacío siempre al final.
		{models.SortByInventoryNumber, []int{3, 4, 1, 2}, []int{1, 4, 3, 2}},
		{models.SortByDeviceType, []int{3, 4, 2, 1}, []int{1, 2, 4, 3}},
		// "Ácer" se ordena como "acer": sin tildes.
		{models.SortByBrand, []int{4, 3, 1, 2}, []int{2, 1, 3, 4}},
		{models.SortByModel, []int{2, 1, 4, 3}, []int{4, 1, 2, 3}},
		// Empate en 1 (ítems 1 y 3): desempata el ID, en ambos sentidos.
		{models.SortByQuantity, []int{4, 1, 3, 2}, []int{2, 1, 3, 4}},
		{models.SortByLocation, []int{2, 3, 1, 4}, []int{1, 3, 2, 4}},
		// Por gravedad: OPERATIVO, EN_REPARACION, FUERA_DE_SERVICIO, BAJA.
		{models.SortByStatus, []int{2, 3, 4, 1}, []int{1, 4, 3, 2}},
		{models.SortByCreatedAt, []int{2, 3, 1, 4}, []int{4, 1, 3, 2}},
		{models.SortByUpdatedAt, []int{4, 3, 2, 1}, []int{1, 2, 3, 4}},
	}
	if len(tests) != len(models.SortFields) {
		t.Fatalf("hay %d campos de orden y %d casos de prueba", len(models.SortFields), len(tests))
	}
	for _, tt := range tests {
		for order, want := range map[models.SortOrder][]int{models.Ascending: tt.asc, models.Descending: tt.desc} {
			t.Run(fmt.Sprintf("%s %s", tt.sort, order), func(t *testing.T) {
				svc, _ := newTestService(sortFixture()...)
				page, err := svc.Query(ctx, models.ItemQuery{Sort: tt.sort, Order: order})
				if err != nil {
					t.Fatal(err)
				}
				if got := queryIDs(page); !slices.Equal(got, want) {
					t.Errorf("IDs = %v; se esperaba %v", got, want)
				}
			})
		}
	}
}

func TestQuery_OrdenPorDefectoEsIDAscendente(t *testing.T) {
	items := sortFixture()
	slices.Reverse(items) // el repositorio los devuelve desordenados
	svc, _ := newTestService(items...)
	page, err := svc.Query(ctx, models.ItemQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if got := queryIDs(page); !slices.Equal(got, []int{1, 2, 3, 4}) {
		t.Errorf("IDs = %v; se esperaba [1 2 3 4]", got)
	}
}

// Se ordena antes de paginar: la página 2 por marca descendente contiene los
// dos últimos de ese orden, no los de la hoja.
func TestQuery_OrdenaAntesDePaginar(t *testing.T) {
	svc, _ := newTestService(sortFixture()...)
	page, err := svc.Query(ctx, models.ItemQuery{Sort: models.SortByBrand, Order: models.Descending, Page: 2, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := queryIDs(page); !slices.Equal(got, []int{3, 4}) {
		t.Errorf("IDs = %v; se esperaba [3 4]", got)
	}
}

func TestQuery_ErrorDelRepositorio(t *testing.T) {
	svc, repo := newTestService(seed()...)
	repo.failWith = fmt.Errorf("falla de lectura")
	if _, err := svc.Query(ctx, models.ItemQuery{}); err == nil {
		t.Error("debería propagar el error del repositorio")
	}
}
