package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"inventario/internal/models"
)

// datosInsumos carga la oficina 1 y el tipo de insumo 1 (Mouse).
func datosInsumos(t *testing.T, repo *SQLServerRepository) (oficina, mouse int) {
	t.Helper()
	must(t, repo.db.QueryRowContext(context.Background(), `
		INSERT INTO dbo.Oficinas (Nombre) VALUES (N'Piso 6 / Sistemas');
		INSERT INTO dbo.Tipos (Nombre, Clase) VALUES (N'Mouse', 'INSUMO');
		SELECT 1, 1`).Scan(&oficina, &mouse))
	return
}

func TestSQLServer_InsumosYStock(t *testing.T) {
	repo := testModeloNuevo(t)
	ctx := context.Background()
	oficina, mouse := datosInsumos(t, repo)

	i, err := repo.CreateInsumo(ctx, models.Insumo{TipoID: mouse, Marca: "Logitech", Modelo: "M90", Stock: 10, StockMinimo: 5, Activo: true}, 1)
	must(t, err)
	if i.Tipo != "Mouse" || i.Stock != 10 || i.BajoStock {
		t.Fatalf("insumo creado = %+v", i)
	}
	if _, err := repo.CreateInsumo(ctx, models.Insumo{TipoID: mouse, Marca: "logitech", Modelo: "m90", Activo: true}, 1); !errors.Is(err, ErrDuplicado) {
		t.Errorf("insumo repetido: err = %v", err)
	}

	mover := func(tipo models.TipoMovimientoInsumo, cantidad int) (models.MovimientoInsumo, models.Insumo, error) {
		return repo.RegistrarMovimiento(ctx, models.MovimientoInsumo{InsumoID: i.ID, Tipo: tipo, Cantidad: cantidad, OficinaID: oficina}, 1)
	}
	m, i, err := mover(models.MovimientoSalida, 3)
	must(t, err)
	if i.Stock != 7 || m.Oficina != "Piso 6 / Sistemas" || m.Insumo != "Mouse Logitech M90" || m.Usuario != "Sistema" {
		t.Errorf("salida: movimiento %+v, stock %d", m, i.Stock)
	}
	if _, _, err := mover(models.MovimientoSalida, 20); !errors.Is(err, ErrStockInsuficiente) {
		t.Errorf("salida mayor al stock: err = %v", err)
	}
	m, i, err = mover(models.MovimientoAjuste, 4) // se contaron 4
	must(t, err)
	if m.Cantidad != -3 || i.Stock != 4 || !i.BajoStock {
		t.Errorf("ajuste: cantidad %d, insumo %+v", m.Cantidad, i)
	}
	if _, _, err := mover(models.MovimientoAjuste, 4); !errors.Is(err, ErrInvalidMovement) {
		t.Errorf("ajuste sin diferencia: err = %v", err)
	}
	_, i, err = mover(models.MovimientoEntrada, 6)
	must(t, err)
	if i.Stock != 10 {
		t.Errorf("entrada: stock %d", i.Stock)
	}

	// No se desactiva con stock; en cero sí, y después no admite movimientos.
	i.Activo = false
	if _, err := repo.UpdateInsumo(ctx, i, 1); !errors.Is(err, ErrEnUso) {
		t.Errorf("desactivar con stock: err = %v", err)
	}
	_, i, err = mover(models.MovimientoAjuste, 0)
	must(t, err)
	i.Activo, i.Observacion = false, "Discontinuado"
	i, err = repo.UpdateInsumo(ctx, i, 1)
	must(t, err)
	if i.Activo || i.Stock != 0 {
		t.Errorf("insumo desactivado = %+v", i)
	}
	if _, _, err := mover(models.MovimientoEntrada, 1); !errors.Is(err, ErrInvalidMovement) {
		t.Errorf("movimiento de un insumo inactivo: err = %v", err)
	}

	hoy := time.Now()
	for _, c := range []struct {
		f    models.FiltroMovimientosInsumo
		want int
	}{
		{models.FiltroMovimientosInsumo{InsumoID: i.ID, Limite: 100}, 5}, // stock inicial, salida, 2 ajustes, entrada
		{models.FiltroMovimientosInsumo{Tipo: models.MovimientoSalida, Limite: 100}, 1},
		{models.FiltroMovimientosInsumo{OficinaID: oficina, Limite: 100}, 4},
		{models.FiltroMovimientosInsumo{Desde: hoy, Hasta: hoy, Limite: 100}, 5},
		{models.FiltroMovimientosInsumo{Hasta: hoy.AddDate(0, 0, -1), Limite: 100}, 0},
		{models.FiltroMovimientosInsumo{Limite: 2}, 2},
	} {
		got, err := repo.ListMovimientosInsumo(ctx, c.f)
		must(t, err)
		if len(got) != c.want {
			t.Errorf("%+v: %d movimientos, se esperaban %d", c.f, len(got), c.want)
		}
	}
}

// Diez salidas simultáneas de 1 unidad con stock 5: exactamente 5 pasan y el
// stock queda en 0 (la fila del insumo se bloquea en cada movimiento).
func TestSQLServer_InsumosSalidasConcurrentes(t *testing.T) {
	repo := testModeloNuevo(t)
	ctx := context.Background()
	oficina, mouse := datosInsumos(t, repo)
	i, err := repo.CreateInsumo(ctx, models.Insumo{TipoID: mouse, Stock: 5, Activo: true}, 1)
	must(t, err)

	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, sinStock := 0, 0
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := repo.RegistrarMovimiento(ctx, models.MovimientoInsumo{InsumoID: i.ID, Tipo: models.MovimientoSalida,
				Cantidad: 1, OficinaID: oficina}, 1)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrStockInsuficiente):
				sinStock++
			default:
				t.Errorf("salida: %v", err)
			}
		}()
	}
	wg.Wait()
	final, err := repo.GetInsumo(ctx, i.ID)
	must(t, err)
	if ok != 5 || sinStock != 5 || final.Stock != 0 {
		t.Errorf("ok %d, sin stock %d, stock final %d", ok, sinStock, final.Stock)
	}
}
