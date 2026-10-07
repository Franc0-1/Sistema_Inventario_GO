package services

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"inventario/internal/models"
	"inventario/internal/repository"
)

func TestSummary_TotalesDistribucionesYMovimientos(t *testing.T) {
	items := seed()
	items = append(items, models.Item{ID: 4, Status: models.StatusRetired, Quantity: 7, HasInventory: true, Availability: models.Loaned})
	svc, repo := newTestService(items...)
	for i := 1; i <= 12; i++ {
		repo.movements = append(repo.movements, models.Movement{ID: i, CreatedAt: createdAt.Add(time.Duration(i/2) * time.Hour)})
	}
	got, err := svc.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveRecords != 3 || got.RetiredRecords != 1 || got.ActiveUnits != 12 || got.AvailableEquipment != 1 || got.LoanedEquipment != 1 || got.Records != 4 || got.Units != 19 {
		t.Fatalf("totales: %+v", got)
	}
	if len(got.RecentMovements) != 10 || got.RecentMovements[0].ID != 12 || got.RecentMovements[1].ID != 11 || got.RecentMovements[2].ID != 10 {
		t.Fatalf("movimientos: %+v", got.RecentMovements)
	}
	if !reflect.DeepEqual(repo.calls, []string{"GetAll", "GetMovements"}) {
		t.Fatal(repo.calls)
	}
	for _, groups := range [][]models.InventoryGroup{got.Locations, got.DeviceTypes, got.Statuses} {
		var records, units int
		for _, group := range groups {
			records += group.Records
			units += group.Units
		}
		if records != got.Records || units != got.Units {
			t.Fatal("distribuciones inconsistentes")
		}
	}
}

func TestReport_FiltrosCombinadosYBajas(t *testing.T) {
	repo, err := repository.NewExcelRepository(filepath.Join(t.TempDir(), "inventario.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	for _, item := range []models.Item{
		{Quantity: 5, Location: "Sistemas", DeviceType: "Mouse", Status: models.StatusOperational, Availability: models.Available},
		{Quantity: 2, Location: "Sistemas", DeviceType: "Mouse", Status: models.StatusRetired, Availability: models.Available},
		{Quantity: 9, Location: "Otra", DeviceType: "Mouse", Status: models.StatusOperational, Availability: models.Available},
		{Quantity: 3, Location: "Sistemas", DeviceType: "Monitor", Status: models.StatusOperational, Availability: models.Available},
	} {
		if _, err := repo.Create(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewInventoryService(repo)
	for _, retired := range []bool{false, true} {
		got, err := svc.Report(ctx, models.ItemFilter{Location: "sistemas", DeviceType: "mouse", IncludeRetired: retired})
		if err != nil {
			t.Fatal(err)
		}
		wantRecords, wantUnits := 1, 5
		if retired {
			wantRecords, wantUnits = 2, 7
		}
		if got.Records != wantRecords || got.Units != wantUnits || len(got.Locations) != 1 || len(got.DeviceTypes) != 1 {
			t.Fatalf("reporte: %+v", got)
		}
	}
	got, err := svc.Report(ctx, models.ItemFilter{Location: "No existe"})
	if err != nil || got.Records != 0 || got.Units != 0 || got.Locations == nil || len(got.Locations) != 0 {
		t.Fatalf("vacío: %+v %v", got, err)
	}
}

func TestAggregateInventory_OrdenNormalizacionYCero(t *testing.T) {
	items := []models.Item{{Location: "z", Quantity: 0}, {Location: "Área", Quantity: 2}, {Location: " area ", Quantity: 3}, {Location: "", Quantity: 1}}
	got := aggregateInventory(items)
	if len(got.Locations) != 3 || got.Locations[0].Value != "" || got.Locations[1].Records != 2 || got.Locations[1].Units != 5 || got.Locations[2].Value != "z" {
		t.Fatal(got.Locations)
	}
	items[0], items[3] = items[3], items[0]
	if !reflect.DeepEqual(got, aggregateInventory(items)) {
		t.Fatal("orden depende de la entrada")
	}
}

func TestAnalytics_VacioYErrores(t *testing.T) {
	svc, repo := newTestService()
	got, err := svc.Summary(context.Background())
	if err != nil || got.RecentMovements == nil || got.Locations == nil || got.Records != 0 {
		t.Fatalf("vacío: %+v %v", got, err)
	}
	repo.failWith = errors.New("detalle interno")
	if _, err := svc.Summary(ctx); !errors.Is(err, repo.failWith) {
		t.Fatal(err)
	}
	if _, err := svc.Report(ctx, models.ItemFilter{}); !errors.Is(err, repo.failWith) {
		t.Fatal(err)
	}
	if _, err := svc.Report(ctx, models.ItemFilter{Status: "INVALIDO"}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatal(err)
	}
}
