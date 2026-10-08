package repository

import (
	"strings"

	"inventario/internal/models"
	"inventario/internal/textnorm"
)

// itemMatcher es un ItemFilter con los textos ya normalizados (textnorm.Fold),
// para no repetir ese trabajo en cada fila.
type itemMatcher struct {
	filter            models.ItemFilter
	text              string
	deviceType        string
	excludeDeviceType string
	location          string
	brand             string
	model             string
	inventoryNumber   string
	serialNumber      string
}

func newItemMatcher(f models.ItemFilter) itemMatcher {
	return itemMatcher{
		filter:            f,
		text:              textnorm.Fold(f.Text),
		deviceType:        textnorm.Fold(f.DeviceType),
		excludeDeviceType: textnorm.Fold(f.ExcludeDeviceType),
		location:          textnorm.Fold(f.Location),
		brand:             textnorm.Fold(f.Brand),
		model:             textnorm.Fold(f.Model),
		inventoryNumber:   textnorm.Fold(f.InventoryNumber),
		serialNumber:      textnorm.Fold(f.SerialNumber),
	}
}

// matches evalúa un ítem contra el filtro (ver models.ItemFilter).
func (m itemMatcher) matches(item models.Item) bool {
	f := m.filter
	switch {
	case f.Status == "" && !f.IncludeRetired && item.Status == models.StatusRetired:
		return false
	case f.Status != "" && item.Status != f.Status:
		return false
	case f.Availability != "" && item.Availability != f.Availability:
		return false
	case f.HasInventory != nil && item.HasInventory != *f.HasInventory:
		return false
	case f.OnlyConsumables && item.HasInventory:
		return false
	case !equalsFolded(m.deviceType, item.DeviceType),
		m.excludeDeviceType != "" && textnorm.Fold(item.DeviceType) == m.excludeDeviceType,
		!equalsFolded(m.location, item.Location),
		!containsFolded(m.brand, item.Brand),
		!containsFolded(m.model, item.Model),
		!containsFolded(m.inventoryNumber, item.InventoryNumber),
		!containsFolded(m.serialNumber, item.SerialNumber):
		return false
	}
	return m.text == "" || containsText(item, m.text)
}

// equalsFolded: un criterio vacío no filtra.
func equalsFolded(want, field string) bool {
	return want == "" || textnorm.Fold(field) == want
}

func containsFolded(want, field string) bool {
	return want == "" || strings.Contains(textnorm.Fold(field), want)
}

// containsText busca text (normalizado) en los campos de búsqueda libre.
func containsText(item models.Item, text string) bool {
	for _, field := range []string{
		item.InventoryNumber, item.Brand, item.Model, item.DeviceType, item.SerialNumber,
	} {
		if strings.Contains(textnorm.Fold(field), text) {
			return true
		}
	}
	return false
}
