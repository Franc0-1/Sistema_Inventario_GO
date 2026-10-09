package services

import (
	"context"
	"errors"
	"testing"

	"inventario/internal/models"
)

// fakeEquipos guarda equipos y componentes en memoria y registra el usuario y
// la nota de la última escritura.
type fakeEquipos struct {
	equipos     map[int]models.Equipo
	componentes map[int]models.Componente
	usuario     int
	nota        string
}

func (f *fakeEquipos) ListEquipos(context.Context, models.FiltroEquipos) ([]models.Equipo, error) {
	return nil, nil
}

func (f *fakeEquipos) GetEquipo(_ context.Context, id int) (models.Equipo, error) {
	e, ok := f.equipos[id]
	if !ok {
		return e, noEncontrado("equipo", id)
	}
	return e, nil
}

func (f *fakeEquipos) CreateEquipo(_ context.Context, e models.Equipo, u int, nota string) (models.Equipo, error) {
	e.ID, f.usuario, f.nota = len(f.equipos)+1, u, nota
	f.equipos[e.ID] = e
	return e, nil
}

func (f *fakeEquipos) UpdateEquipo(_ context.Context, e models.Equipo, u int, nota string) (models.Equipo, error) {
	f.equipos[e.ID], f.usuario, f.nota = e, u, nota
	return e, nil
}

func (f *fakeEquipos) HistorialEquipo(context.Context, int) ([]models.HistorialEquipo, error) {
	return nil, nil
}

func (f *fakeEquipos) ListComponentes(_ context.Context, filtro models.FiltroComponentes) ([]models.Componente, error) {
	var out []models.Componente
	for _, c := range f.componentes {
		if c.EquipoID == filtro.EquipoID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeEquipos) GetComponente(_ context.Context, id int) (models.Componente, error) {
	c, ok := f.componentes[id]
	if !ok {
		return c, noEncontrado("componente", id)
	}
	return c, nil
}

func (f *fakeEquipos) CreateComponente(_ context.Context, c models.Componente, u int, nota string) (models.Componente, error) {
	c.ID, f.usuario, f.nota = len(f.componentes)+1, u, nota
	f.componentes[c.ID] = c
	return c, nil
}

func (f *fakeEquipos) UpdateComponente(_ context.Context, c models.Componente, u int, nota string) (models.Componente, error) {
	f.componentes[c.ID], f.usuario, f.nota = c, u, nota
	return c, nil
}

func (f *fakeEquipos) HistorialComponente(context.Context, int) ([]models.HistorialComponente, error) {
	return nil, nil
}

// Tipos: 1 PC (equipo), 2 RAM (componente), 3 Mouse (insumo), 4 Fax (equipo inactivo).
// Equipos: 1 PC 1001 en Sistemas, 2 PC 1002 de baja. Persona 1 (Ana) está en la oficina 2 (inactiva).
func equiposDePrueba() (EquipoService, *fakeEquipos, *fakeCatalogos) {
	cat := newFakeCatalogos()
	cat.tipos = map[int]models.Tipo{
		1: {ID: 1, Nombre: "PC", Clase: models.ClaseEquipo, Activo: true},
		2: {ID: 2, Nombre: "RAM", Clase: models.ClaseComponente, Activo: true},
		3: {ID: 3, Nombre: "Mouse", Clase: models.ClaseInsumo, Activo: true},
		4: {ID: 4, Nombre: "Fax", Clase: models.ClaseEquipo, Activo: false},
	}
	cat.personas[3] = models.Persona{ID: 3, Nombre: "Luis", OficinaID: 1, Activo: true}
	repo := &fakeEquipos{
		equipos: map[int]models.Equipo{
			1: {ID: 1, NumeroInventario: "1001", TipoID: 1, OficinaID: 1, Estado: models.StatusOperational, Version: "v"},
			2: {ID: 2, NumeroInventario: "1002", TipoID: 1, OficinaID: 1, Estado: models.StatusRetired, Version: "v"},
		},
		componentes: map[int]models.Componente{
			1: {ID: 1, NumeroInventario: "1002", TipoID: 2, EquipoID: 2, Estado: models.StatusOperational, Version: "v"},
		},
	}
	return NewEquipoService(repo, cat), repo, cat
}

func TestEquipos_AltaConPersonaUsaSuOficina(t *testing.T) {
	s, repo, _ := equiposDePrueba()
	ctx := ConUsuario(context.Background(), 1)
	e, err := s.CrearEquipo(ctx, models.Equipo{NumeroInventario: " 2001 ", TipoID: 1, PersonaID: 3, Estado: "operativo",
		MotivoBaja: "no corresponde"}, "  Entrega inicial ")
	if err != nil {
		t.Fatal(err)
	}
	if e.OficinaID != 1 || e.NumeroInventario != "2001" || e.Estado != models.StatusOperational || e.MotivoBaja != "" {
		t.Errorf("equipo = %+v", e)
	}
	if repo.usuario != 1 || repo.nota != "Entrega inicial" {
		t.Errorf("usuario %d, nota %q", repo.usuario, repo.nota)
	}
}

func TestEquipos_Rechazos(t *testing.T) {
	s, _, _ := equiposDePrueba()
	ctx := ConUsuario(context.Background(), 1)
	for _, c := range []struct {
		nombre string
		err    error
		hacer  func() error
	}{
		{"sin usuario", ErrUsuarioInvalido, func() error {
			_, err := s.CrearEquipo(context.Background(), models.Equipo{TipoID: 1, OficinaID: 1}, "")
			return err
		}},
		{"sin tipo", ErrDatoInvalido, func() error {
			_, err := s.CrearEquipo(ctx, models.Equipo{OficinaID: 1}, "")
			return err
		}},
		{"tipo de componente", ErrDatoInvalido, func() error {
			_, err := s.CrearEquipo(ctx, models.Equipo{TipoID: 2, OficinaID: 1}, "")
			return err
		}},
		{"tipo inactivo", ErrDatoInvalido, func() error {
			_, err := s.CrearEquipo(ctx, models.Equipo{TipoID: 4, OficinaID: 1}, "")
			return err
		}},
		{"sin oficina ni persona", ErrDatoInvalido, func() error {
			_, err := s.CrearEquipo(ctx, models.Equipo{TipoID: 1}, "")
			return err
		}},
		{"oficina inactiva", ErrDatoInvalido, func() error {
			_, err := s.CrearEquipo(ctx, models.Equipo{TipoID: 1, OficinaID: 2}, "")
			return err
		}},
		{"persona inexistente", ErrDatoInvalido, func() error {
			_, err := s.CrearEquipo(ctx, models.Equipo{TipoID: 1, PersonaID: 9}, "")
			return err
		}},
		{"baja sin motivo", ErrDatoInvalido, func() error {
			_, err := s.CrearEquipo(ctx, models.Equipo{TipoID: 1, OficinaID: 1, Estado: models.StatusRetired}, "")
			return err
		}},
		{"estado desconocido", ErrDatoInvalido, func() error {
			_, err := s.CrearEquipo(ctx, models.Equipo{TipoID: 1, OficinaID: 1, Estado: "ROTO"}, "")
			return err
		}},
		{"componente con tipo de insumo", ErrDatoInvalido, func() error {
			_, err := s.CrearComponente(ctx, models.Componente{TipoID: 3, OficinaID: 1}, "")
			return err
		}},
		{"componente sin ubicación", ErrDatoInvalido, func() error {
			_, err := s.CrearComponente(ctx, models.Componente{TipoID: 2}, "")
			return err
		}},
		{"componente en un equipo de baja", ErrDatoInvalido, func() error {
			_, err := s.CrearComponente(ctx, models.Componente{TipoID: 2, EquipoID: 2}, "")
			return err
		}},
		{"componente en un equipo inexistente", ErrDatoInvalido, func() error {
			_, err := s.CrearComponente(ctx, models.Componente{TipoID: 2, EquipoID: 9}, "")
			return err
		}},
		{"editar sin versión", ErrDatoInvalido, func() error {
			_, err := s.ActualizarEquipo(ctx, models.Equipo{ID: 1, TipoID: 1, OficinaID: 1}, "")
			return err
		}},
	} {
		if err := c.hacer(); !errors.Is(err, c.err) {
			t.Errorf("%s: err = %v, se esperaba %v", c.nombre, err, c.err)
		}
	}
}

func TestEquipos_ComponenteTomaElNumeroDelEquipo(t *testing.T) {
	s, _, _ := equiposDePrueba()
	ctx := ConUsuario(context.Background(), 1)
	c, err := s.CrearComponente(ctx, models.Componente{TipoID: 2, EquipoID: 1, OficinaID: 5}, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.NumeroInventario != "1001" || c.OficinaID != 0 {
		t.Errorf("componente = %+v", c)
	}
	propio, err := s.CrearComponente(ctx, models.Componente{NumeroInventario: "3001", TipoID: 2, EquipoID: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if propio.NumeroInventario != "3001" {
		t.Errorf("número propio = %q", propio.NumeroInventario)
	}
}

// Editar un componente que sigue en un equipo dado de baja no exige moverlo;
// sacarlo a una oficina sí funciona.
func TestEquipos_ComponenteEnEquipoDeBaja(t *testing.T) {
	s, repo, _ := equiposDePrueba()
	ctx := ConUsuario(context.Background(), 1)
	c := repo.componentes[1]
	c.Observacion = "Revisada"
	if _, err := s.ActualizarComponente(ctx, c, ""); err != nil {
		t.Fatalf("misma PC de baja: %v", err)
	}
	c.EquipoID, c.OficinaID = 0, 1
	movido, err := s.ActualizarComponente(ctx, c, "Se retira de la PC")
	if err != nil {
		t.Fatal(err)
	}
	if movido.EquipoID != 0 || movido.OficinaID != 1 || repo.nota != "Se retira de la PC" {
		t.Errorf("componente = %+v, nota %q", movido, repo.nota)
	}
}

func TestEquipos_DetalleConComponentes(t *testing.T) {
	s, _, _ := equiposDePrueba()
	e, err := s.ObtenerEquipo(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Componentes) != 1 {
		t.Errorf("componentes = %+v", e.Componentes)
	}
}
