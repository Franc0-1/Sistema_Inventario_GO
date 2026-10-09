package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"inventario/internal/models"
)

// fakeInsumos guarda el último insumo, movimiento y filtro que recibió. El
// cálculo del stock se prueba contra SQL Server en el paquete repository.
type fakeInsumos struct {
	insumo     models.Insumo
	movimiento models.MovimientoInsumo
	filtro     models.FiltroMovimientosInsumo
	usuario    int
}

func (f *fakeInsumos) ListInsumos(context.Context, models.FiltroInsumos) ([]models.Insumo, error) {
	return nil, nil
}

func (f *fakeInsumos) GetInsumo(_ context.Context, id int) (models.Insumo, error) {
	return models.Insumo{ID: id, TipoID: 3, Activo: true}, nil
}

func (f *fakeInsumos) CreateInsumo(_ context.Context, i models.Insumo, u int) (models.Insumo, error) {
	f.insumo, f.usuario = i, u
	return i, nil
}

func (f *fakeInsumos) UpdateInsumo(_ context.Context, i models.Insumo, u int) (models.Insumo, error) {
	f.insumo, f.usuario = i, u
	return i, nil
}

func (f *fakeInsumos) RegistrarMovimiento(_ context.Context, m models.MovimientoInsumo, u int) (models.MovimientoInsumo, models.Insumo, error) {
	f.movimiento, f.usuario = m, u
	return m, models.Insumo{}, nil
}

func (f *fakeInsumos) ListMovimientosInsumo(_ context.Context, filtro models.FiltroMovimientosInsumo) ([]models.MovimientoInsumo, error) {
	f.filtro = filtro
	return nil, nil
}

func insumosDePrueba() (InsumoService, *fakeInsumos) {
	_, _, cat := equiposDePrueba() // tipos 1 PC, 2 RAM, 3 Mouse; personas 1 (oficina 2 inactiva) y 3 (oficina 1)
	cat.personas[4] = models.Persona{ID: 4, Nombre: "Eva", OficinaID: 1, Activo: false}
	repo := &fakeInsumos{}
	return NewInsumoService(repo, cat), repo
}

func TestInsumos_EntregaAPersonaRegistraSuOficina(t *testing.T) {
	s, repo := insumosDePrueba()
	ctx := ConUsuario(context.Background(), 1)
	if _, _, err := s.RegistrarMovimiento(ctx, models.MovimientoInsumo{InsumoID: 1, Tipo: "salida", Cantidad: 2, PersonaID: 3}); err != nil {
		t.Fatal(err)
	}
	if repo.movimiento.Tipo != models.MovimientoSalida || repo.movimiento.OficinaID != 1 || repo.usuario != 1 {
		t.Errorf("movimiento = %+v, usuario %d", repo.movimiento, repo.usuario)
	}
	// El ajuste con stock contado 0 es válido (se terminó todo).
	if _, _, err := s.RegistrarMovimiento(ctx, models.MovimientoInsumo{InsumoID: 1, Tipo: models.MovimientoAjuste}); err != nil {
		t.Errorf("ajuste a 0: %v", err)
	}
}

func TestInsumos_Rechazos(t *testing.T) {
	s, _ := insumosDePrueba()
	ctx := ConUsuario(context.Background(), 1)
	mov := func(m models.MovimientoInsumo) func() error {
		return func() error { _, _, err := s.RegistrarMovimiento(ctx, m); return err }
	}
	for _, c := range []struct {
		nombre string
		err    error
		hacer  func() error
	}{
		{"alta sin usuario", ErrUsuarioInvalido, func() error {
			_, err := s.CrearInsumo(context.Background(), models.Insumo{TipoID: 3})
			return err
		}},
		{"alta con tipo de equipo", ErrDatoInvalido, func() error {
			_, err := s.CrearInsumo(ctx, models.Insumo{TipoID: 1})
			return err
		}},
		{"stock inicial negativo", ErrDatoInvalido, func() error {
			_, err := s.CrearInsumo(ctx, models.Insumo{TipoID: 3, Stock: -1})
			return err
		}},
		{"stock mínimo negativo", ErrDatoInvalido, func() error {
			_, err := s.CrearInsumo(ctx, models.Insumo{TipoID: 3, StockMinimo: -1})
			return err
		}},
		{"tipo de movimiento desconocido", ErrDatoInvalido, mov(models.MovimientoInsumo{InsumoID: 1, Tipo: "PRESTAMO", Cantidad: 1})},
		{"entrada de 0", ErrDatoInvalido, mov(models.MovimientoInsumo{InsumoID: 1, Tipo: models.MovimientoEntrada})},
		{"ajuste negativo", ErrDatoInvalido, mov(models.MovimientoInsumo{InsumoID: 1, Tipo: models.MovimientoAjuste, Cantidad: -3})},
		{"salida sin destino", ErrDatoInvalido, mov(models.MovimientoInsumo{InsumoID: 1, Tipo: models.MovimientoSalida, Cantidad: 1})},
		{"salida a una persona inactiva", ErrDatoInvalido, mov(models.MovimientoInsumo{InsumoID: 1, Tipo: models.MovimientoSalida, Cantidad: 1, PersonaID: 4})},
		{"salida a una oficina inactiva", ErrDatoInvalido, mov(models.MovimientoInsumo{InsumoID: 1, Tipo: models.MovimientoSalida, Cantidad: 1, OficinaID: 2})},
		{"insumo inválido", ErrInvalidID, mov(models.MovimientoInsumo{Tipo: models.MovimientoEntrada, Cantidad: 1})},
		{"límite excesivo", ErrDatoInvalido, func() error {
			_, err := s.ListarMovimientos(ctx, models.FiltroMovimientosInsumo{Limite: 5000})
			return err
		}},
		{"fechas invertidas", ErrDatoInvalido, func() error {
			hoy := time.Now()
			_, err := s.ListarMovimientos(ctx, models.FiltroMovimientosInsumo{Desde: hoy, Hasta: hoy.AddDate(0, 0, -1)})
			return err
		}},
	} {
		if err := c.hacer(); !errors.Is(err, c.err) {
			t.Errorf("%s: err = %v, se esperaba %v", c.nombre, err, c.err)
		}
	}
}

func TestInsumos_LimitePorDefecto(t *testing.T) {
	s, repo := insumosDePrueba()
	if _, err := s.ListarMovimientos(context.Background(), models.FiltroMovimientosInsumo{Tipo: "entrada"}); err != nil {
		t.Fatal(err)
	}
	if repo.filtro.Limite != LimiteMovimientosPorDefecto || repo.filtro.Tipo != models.MovimientoEntrada {
		t.Errorf("filtro = %+v", repo.filtro)
	}
}
