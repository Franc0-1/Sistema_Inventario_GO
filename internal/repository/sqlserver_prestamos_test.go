package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"inventario/internal/models"
)

func TestSQLServer_Prestamos(t *testing.T) {
	repo := testModeloNuevo(t)
	ctx := context.Background()
	sistemas, _, persona, pc, _ := datosEquipos(t, repo)
	nb, err := repo.CreateEquipo(ctx, models.Equipo{NumeroInventario: "N-1", TipoID: pc, Marca: "Lenovo",
		Estado: models.StatusOperational, OficinaID: sistemas}, 1, "")
	must(t, err)

	ahora := time.Now()
	p, err := repo.CreatePrestamo(ctx, models.Prestamo{EquipoID: nb.ID, PersonaID: persona, FechaSalida: ahora,
		DevolucionPrevista: ahora.Add(2 * time.Hour), Observacion: "Reunión"}, 1)
	must(t, err)
	if p.Equipo != "N-1" || p.Persona != "María Gómez" || p.Oficina != "Piso EP / Prensa" || p.Vencido || p.FechaDevolucion != nil {
		t.Fatalf("préstamo = %+v", p)
	}

	// El equipo muestra a quién está prestado y no se puede volver a prestar.
	e, err := repo.GetEquipo(ctx, nb.ID)
	must(t, err)
	if e.PrestamoID != p.ID || e.PrestadoA != "María Gómez" {
		t.Errorf("equipo prestado = %+v", e)
	}
	if _, err := repo.CreatePrestamo(ctx, models.Prestamo{EquipoID: nb.ID, PersonaID: persona, FechaSalida: ahora,
		DevolucionPrevista: ahora.Add(time.Hour)}, 1); !errors.Is(err, ErrEnUso) || !contiene(err, "María Gómez") {
		t.Errorf("segundo préstamo: err = %v", err)
	}
	// Ni darlo de baja, ni desactivar a la persona.
	e.Estado, e.MotivoBaja = models.StatusRetired, "Rota"
	if _, err := repo.UpdateEquipo(ctx, e, 1, ""); !errors.Is(err, ErrEnUso) {
		t.Errorf("baja de un equipo prestado: err = %v", err)
	}
	per, err := repo.GetPersona(ctx, persona)
	must(t, err)
	per.Activo = false
	if _, err := repo.UpdatePersona(ctx, per, 1); !errors.Is(err, ErrEnUso) {
		t.Errorf("desactivar a quien tiene un préstamo: err = %v", err)
	}

	p, err = repo.DevolverPrestamo(ctx, p.ID, "Sin cargador", 1)
	must(t, err)
	if p.FechaDevolucion == nil || p.Observacion != "Reunión | Devolución: Sin cargador" {
		t.Errorf("devuelto = %+v", p)
	}
	if _, err := repo.DevolverPrestamo(ctx, p.ID, "", 1); !errors.Is(err, ErrEstadoInvalido) {
		t.Errorf("devolver dos veces: err = %v", err)
	}
	if _, err := repo.DevolverPrestamo(ctx, 999, "", 1); !errors.Is(err, ErrNoEncontrado) {
		t.Errorf("préstamo inexistente: err = %v", err)
	}

	// Un préstamo vencido (la devolución prevista ya pasó).
	_, err = repo.db.ExecContext(ctx, `INSERT INTO dbo.Prestamos (EquipoID, PersonaID, FechaSalida, DevolucionPrevista, UsuarioID)
		VALUES (@p1, @p2, DATEADD(HOUR, -5, SYSDATETIMEOFFSET()), DATEADD(HOUR, -1, SYSDATETIMEOFFSET()), 1)`, nb.ID, persona)
	must(t, err)
	for _, c := range []struct {
		f    models.FiltroPrestamos
		want int
	}{
		{models.FiltroPrestamos{Limite: 10}, 2},
		{models.FiltroPrestamos{Abiertos: true, Limite: 10}, 1},
		{models.FiltroPrestamos{Vencidos: true, Limite: 10}, 1},
		{models.FiltroPrestamos{PersonaID: persona + 1, Limite: 10}, 0},
	} {
		got, err := repo.ListPrestamos(ctx, c.f)
		must(t, err)
		if len(got) != c.want {
			t.Errorf("%+v: %d préstamos, se esperaban %d", c.f, len(got), c.want)
		}
		if c.f.Vencidos && len(got) == 1 && !got[0].Vencido {
			t.Errorf("el préstamo vencido no figura como vencido: %+v", got[0])
		}
	}

	// Un equipo de baja no se presta.
	_, err = repo.db.ExecContext(ctx, `UPDATE dbo.Prestamos SET FechaDevolucion = SYSDATETIMEOFFSET() WHERE FechaDevolucion IS NULL`)
	must(t, err)
	e, err = repo.GetEquipo(ctx, nb.ID)
	must(t, err)
	e.Estado, e.MotivoBaja = models.StatusRetired, "Rota"
	_, err = repo.UpdateEquipo(ctx, e, 1, "")
	must(t, err)
	if _, err := repo.CreatePrestamo(ctx, models.Prestamo{EquipoID: nb.ID, PersonaID: persona, FechaSalida: ahora,
		DevolucionPrevista: ahora.Add(time.Hour)}, 1); !errors.Is(err, ErrEstadoInvalido) {
		t.Errorf("préstamo de un equipo de baja: err = %v", err)
	}
}
