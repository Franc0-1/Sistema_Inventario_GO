package services

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"inventario/internal/models"
	"inventario/internal/textnorm"
)

// Query filtra en el repositorio y ordena y pagina en memoria: el Excel se
// lee una sola vez por consulta, bajo el bloqueo de lectura del repositorio.
func (s *inventoryService) Query(ctx context.Context, q models.ItemQuery) (models.ItemPage, error) {
	q, err := normalizeQuery(q)
	if err != nil {
		return models.ItemPage{}, err
	}
	items, err := s.repo.Search(ctx, q.Filter)
	if err != nil {
		return models.ItemPage{}, err
	}
	sortItems(items, q.Sort, q.Order)
	return paginate(items, q.Page, q.PageSize), nil
}

// normalizeQuery valida la consulta contra las listas blancas y completa los
// valores por defecto. Los nombres se aceptan sin distinguir mayúsculas.
func normalizeQuery(q models.ItemQuery) (models.ItemQuery, error) {
	var ok bool
	if q.Filter.Status != "" {
		if q.Filter.Status, ok = oneOf(string(q.Filter.Status), models.ValidStatuses); !ok {
			return q, invalidQuery("estado", string(q.Filter.Status), models.ValidStatuses)
		}
	}
	if q.Filter.Availability != "" {
		valid := []models.Availability{models.Available, models.Loaned}
		if q.Filter.Availability, ok = oneOf(string(q.Filter.Availability), valid); !ok {
			return q, invalidQuery("disponibilidad", string(q.Filter.Availability), valid)
		}
	}

	if q.Sort == "" {
		q.Sort = models.SortByID
	} else if q.Sort, ok = oneOf(string(q.Sort), models.SortFields); !ok {
		return q, invalidQuery("campo de orden", string(q.Sort), models.SortFields)
	}
	if q.Order == "" {
		q.Order = models.Ascending
	} else if q.Order, ok = oneOf(string(q.Order), []models.SortOrder{models.Ascending, models.Descending}); !ok {
		return q, invalidQuery("sentido de orden", string(q.Order), []models.SortOrder{models.Ascending, models.Descending})
	}

	switch {
	case q.Page < 0:
		return q, fmt.Errorf("%w: la página debe ser mayor o igual a 1", ErrInvalidQuery)
	case q.PageSize < 0 || q.PageSize > models.MaxPageSize:
		return q, fmt.Errorf("%w: el tamaño de página debe estar entre 1 y %d", ErrInvalidQuery, models.MaxPageSize)
	case q.Page == 0 && q.PageSize == 0:
		// sin paginar
	case q.Page == 0:
		q.Page = 1
	case q.PageSize == 0:
		q.PageSize = models.DefaultPageSize
	}
	return q, nil
}

// oneOf devuelve el valor canónico de la lista que coincide con value sin
// distinguir mayúsculas ni espacios alrededor.
func oneOf[T ~string](value string, valid []T) (T, bool) {
	value = strings.TrimSpace(value)
	for _, v := range valid {
		if strings.EqualFold(value, string(v)) {
			return v, true
		}
	}
	return T(value), false
}

func invalidQuery[T ~string](what, value string, valid []T) error {
	names := make([]string, len(valid))
	for i, v := range valid {
		names[i] = string(v)
	}
	return fmt.Errorf("%w: %s %q desconocido (válidos: %s)", ErrInvalidQuery, what, value, strings.Join(names, ", "))
}

// sortItems ordena de forma estable. A igual valor decide el ID ascendente,
// así el orden (y por lo tanto cada página) es siempre el mismo. Los textos
// se comparan sin mayúsculas ni tildes y los vacíos van al final en ambos
// sentidos (un consumible sin N° de inventario no ocupa las primeras páginas).
func sortItems(items []models.Item, field models.SortField, order models.SortOrder) {
	compare := itemComparator(field)
	sign := 1
	if order == models.Descending {
		sign = -1
	}
	slices.SortStableFunc(items, func(a, b models.Item) int {
		if c := compare(a, b, sign); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

// itemComparator devuelve la comparación del campo ya multiplicada por sign
// (salvo los vacíos, que siempre van al final).
func itemComparator(field models.SortField) func(a, b models.Item, sign int) int {
	byText := func(get func(models.Item) string) func(a, b models.Item, sign int) int {
		return func(a, b models.Item, sign int) int {
			x, y := textnorm.Fold(get(a)), textnorm.Fold(get(b))
			switch {
			case x == y:
				return 0
			case x == "":
				return 1
			case y == "":
				return -1
			}
			return sign * strings.Compare(x, y)
		}
	}
	byValue := func(c func(a, b models.Item) int) func(a, b models.Item, sign int) int {
		return func(a, b models.Item, sign int) int { return sign * c(a, b) }
	}

	switch field {
	case models.SortByInventoryNumber:
		return byText(func(it models.Item) string { return it.InventoryNumber })
	case models.SortByDeviceType:
		return byText(func(it models.Item) string { return it.DeviceType })
	case models.SortByBrand:
		return byText(func(it models.Item) string { return it.Brand })
	case models.SortByModel:
		return byText(func(it models.Item) string { return it.Model })
	case models.SortByLocation:
		return byText(func(it models.Item) string { return it.Location })
	case models.SortByQuantity:
		return byValue(func(a, b models.Item) int { return cmp.Compare(a.Quantity, b.Quantity) })
	case models.SortByStatus:
		// Orden de gravedad (OPERATIVO … BAJA), no alfabético.
		return byValue(func(a, b models.Item) int { return cmp.Compare(statusRank(a.Status), statusRank(b.Status)) })
	case models.SortByCreatedAt:
		return byValue(func(a, b models.Item) int { return a.CreatedAt.Compare(b.CreatedAt) })
	case models.SortByUpdatedAt:
		return byValue(func(a, b models.Item) int { return a.UpdatedAt.Compare(b.UpdatedAt) })
	default: // SortByID
		return byValue(func(a, b models.Item) int { return cmp.Compare(a.ID, b.ID) })
	}
}

func statusRank(s models.ItemStatus) int {
	if i := slices.Index(models.ValidStatuses, s); i >= 0 {
		return i
	}
	return len(models.ValidStatuses)
}

// paginate recorta la página pedida. Sin paginación (Page 0) devuelve todo
// como una única página.
func paginate(items []models.Item, page, pageSize int) models.ItemPage {
	total := len(items)
	if page == 0 {
		p := models.Pagination{Page: 1, PageSize: total, TotalItems: total}
		if total > 0 {
			p.TotalPages = 1
		}
		return models.ItemPage{Items: items, Pagination: p}
	}
	start := total
	if page-1 <= total/pageSize { // evita desbordar el producto con páginas enormes
		start = (page - 1) * pageSize
	}
	end := min(start+pageSize, total)
	return models.ItemPage{
		Items: items[start:end:end],
		Pagination: models.Pagination{
			Page:       page,
			PageSize:   pageSize,
			TotalItems: total,
			TotalPages: (total + pageSize - 1) / pageSize,
		},
	}
}
