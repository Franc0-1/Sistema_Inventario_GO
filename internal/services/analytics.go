package services

import (
	"cmp"
	"context"
	"slices"

	"inventario/internal/models"
	"inventario/internal/utils"
)

// Summary uses one inventory read and one history read under the service's
// write lock, so application writes cannot split the snapshot.
func (s *inventoryService) Summary(ctx context.Context) (models.InventorySummary, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	items, err := s.repo.GetAll(ctx)
	if err != nil {
		return models.InventorySummary{}, err
	}
	movements, err := s.repo.GetMovements(ctx)
	if err != nil {
		return models.InventorySummary{}, err
	}
	result := models.InventorySummary{InventoryReport: aggregateInventory(items), RecentMovements: make([]models.Movement, 0)}
	for _, item := range items {
		if item.Status == models.StatusRetired {
			result.RetiredRecords++
			continue
		}
		result.ActiveRecords++
		result.ActiveUnits += item.Quantity
		if item.HasInventory {
			if item.Availability == models.Available {
				result.AvailableEquipment++
			}
			if item.Availability == models.Loaned {
				result.LoanedEquipment++
			}
		}
	}
	movements = slices.Clone(movements)
	slices.SortFunc(movements, func(a, b models.Movement) int {
		if order := b.CreatedAt.Compare(a.CreatedAt); order != 0 {
			return order
		}
		return cmp.Compare(b.ID, a.ID)
	})
	result.RecentMovements = append(result.RecentMovements, movements[:min(10, len(movements))]...)
	return result, nil
}

func (s *inventoryService) Report(ctx context.Context, filter models.ItemFilter) (models.InventoryReport, error) {
	query, err := normalizeQuery(models.ItemQuery{Filter: filter})
	if err != nil {
		return models.InventoryReport{}, err
	}
	items, err := s.repo.Search(ctx, query.Filter)
	if err != nil {
		return models.InventoryReport{}, err
	}
	return aggregateInventory(items), nil
}

func aggregateInventory(items []models.Item) models.InventoryReport {
	result := models.InventoryReport{Records: len(items)}
	for _, item := range items {
		result.Units += item.Quantity
	}
	result.Locations = groupInventory(items, func(it models.Item) string { return it.Location })
	result.DeviceTypes = groupInventory(items, func(it models.Item) string { return it.DeviceType })
	result.Statuses = groupInventory(items, func(it models.Item) string { return string(it.Status) })
	return result
}

func groupInventory(items []models.Item, value func(models.Item) string) []models.InventoryGroup {
	groups := make(map[string]models.InventoryGroup)
	for _, item := range items {
		label := value(item)
		key := utils.FoldText(label)
		group := groups[key]
		if group.Records == 0 || label < group.Value {
			group.Value = label
		}
		group.Records++
		group.Units += item.Quantity
		groups[key] = group
	}
	result := make([]models.InventoryGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, group)
	}
	slices.SortFunc(result, func(a, b models.InventoryGroup) int {
		return cmp.Compare(utils.FoldText(a.Value), utils.FoldText(b.Value))
	})
	return result
}
