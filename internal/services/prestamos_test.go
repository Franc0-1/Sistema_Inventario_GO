package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"inventario/internal/models"
)

type fakePrestamos struct {
	prestamo models.Prestamo
	usuario  int
	nota     string
}

func (f *fakePrestamos) ListPrestamos(context.Context, models.FiltroPrestamos) ([]models.Prestamo, error) {
	return nil, nil
}

func (f *fakePrestamos) GetPrestamo(_ context.Context, id int) (models.Prestamo, error) {
	return models.Prestamo{ID: id}, nil
}

func (f *fakePrestamos) CreatePrestamo(_ context.Context, p models.Prestamo, u int) (models.Prestamo, error) {
	f.prestamo, f.usuario = p, u
	return p, nil
}

func (f *fakePrestamos) DevolverPrestamo(_ context.Context, id int, nota string, u int) (models.Prestamo, error) {
	f.nota, f.usuario = nota, u
	return models.Prestamo{ID: id}, nil
}

// prestamosDePrueba: son las 10:00 y la hora de salida es 13:00. Equipos de
// equiposDePrueba más la 3 (Notebook prestable). Personas: 1 activa, 4 inactiva.
func prestamosDePrueba(hora int) (*prestamoService, *fakePrestamos) {
	_, equipos, cat := equiposDePrueba()
	cat.tipos[5] = models.Tipo{ID: 5, Nombre: "Notebook", Clase: models.ClaseEquipo, Prestable: true, Activo: true}
	equipos.equipos[3] = models.Equipo{ID: 3, NumeroInventario: "1003", TipoID: 5, OficinaID: 1, Estado: models.StatusOperational}
	equipos.equipos[4] = models.Equipo{ID: 4, NumeroInventario: "1004", TipoID: 5, OficinaID: 1, Estado: models.StatusRetired}
	cat.personas[4] = models.Persona{ID: 4, Nombre: "Eva", OficinaID: 1, Activo: false}
	repo := &fakePrestamos{}
	s := NewPrestamoService(repo, equipos, cat, 13*time.Hour).(*prestamoService)
	s.ahora = func() time.Time { return time.Date(2026, 10, 9, hora, 0, 0, 0, time.Local) }
	return s, repo
}

func TestPrestamos_VuelveHoyALaHoraDeSalida(t *testing.T) {
	s, repo := prestamosDePrueba(10)
	ctx := ConUsuario(context.Background(), 1)
	if _, err := s.Prestar(ctx, models.Prestamo{EquipoID: 3, PersonaID: 1, Observacion: " Reunión "}); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 9, 13, 0, 0, 0, time.Local)
	if !repo.prestamo.DevolucionPrevista.Equal(want) || repo.prestamo.Observacion != "Reunión" || repo.usuario != 1 {
		t.Errorf("préstamo = %+v, usuario %d", repo.prestamo, repo.usuario)
	}
}

func TestPrestamos_Rechazos(t *testing.T) {
	s, _ := prestamosDePrueba(10)
	tarde, _ := prestamosDePrueba(15)
	ctx := ConUsuario(context.Background(), 1)
	for _, c := range []struct {
		nombre string
		err    error
		hacer  func() error
	}{
		{"sin usuario", ErrUsuarioInvalido, func() error {
			_, err := s.Prestar(context.Background(), models.Prestamo{EquipoID: 3, PersonaID: 1})
			return err
		}},
		{"tipo no prestable", ErrDatoInvalido, func() error {
			_, err := s.Prestar(ctx, models.Prestamo{EquipoID: 1, PersonaID: 1})
			return err
		}},
		{"equipo de baja", ErrDatoInvalido, func() error {
			_, err := s.Prestar(ctx, models.Prestamo{EquipoID: 4, PersonaID: 1})
			return err
		}},
		{"equipo inexistente", ErrDatoInvalido, func() error {
			_, err := s.Prestar(ctx, models.Prestamo{EquipoID: 9, PersonaID: 1})
			return err
		}},
		{"sin persona", ErrDatoInvalido, func() error {
			_, err := s.Prestar(ctx, models.Prestamo{EquipoID: 3})
			return err
		}},
		{"persona inactiva", ErrDatoInvalido, func() error {
			_, err := s.Prestar(ctx, models.Prestamo{EquipoID: 3, PersonaID: 4})
			return err
		}},
		{"devolución en el pasado", ErrDatoInvalido, func() error {
			_, err := s.Prestar(ctx, models.Prestamo{EquipoID: 3, PersonaID: 1,
				DevolucionPrevista: time.Date(2026, 10, 9, 9, 0, 0, 0, time.Local)})
			return err
		}},
		{"después de la hora de salida sin fecha", ErrDatoInvalido, func() error {
			_, err := tarde.Prestar(ctx, models.Prestamo{EquipoID: 3, PersonaID: 1})
			return err
		}},
		{"devolver sin usuario", ErrUsuarioInvalido, func() error {
			_, err := s.Devolver(context.Background(), 1, "")
			return err
		}},
	} {
		if err := c.hacer(); !errors.Is(err, c.err) {
			t.Errorf("%s: err = %v, se esperaba %v", c.nombre, err, c.err)
		}
	}
	// Después de la hora de salida, con fecha explícita, sí se presta.
	if _, err := tarde.Prestar(ctx, models.Prestamo{EquipoID: 3, PersonaID: 1,
		DevolucionPrevista: time.Date(2026, 10, 9, 18, 0, 0, 0, time.Local)}); err != nil {
		t.Errorf("con fecha explícita: %v", err)
	}
}
