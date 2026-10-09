package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"inventario/internal/models"
	"inventario/internal/repository"
)

// fakeCatalogos guarda los catálogos en memoria y registra el usuario de la
// última escritura. Las reglas de la base (únicos, en uso) se prueban contra
// SQL Server en el paquete repository.
type fakeCatalogos struct {
	catalogos map[models.TablaCatalogo]map[int]models.Catalogo
	personas  map[int]models.Persona
	tipos     map[int]models.Tipo
	usuario   int
}

func newFakeCatalogos() *fakeCatalogos {
	return &fakeCatalogos{
		catalogos: map[models.TablaCatalogo]map[int]models.Catalogo{
			models.CatalogoOficinas: {
				1: {ID: 1, Nombre: "Piso 6 / Sistemas", Activo: true, Version: "v"},
				2: {ID: 2, Nombre: "Piso 4 / FIA", Activo: false, Version: "v"},
			},
			models.CatalogoPuestos:  {1: {ID: 1, Nombre: "Técnico", Activo: true, Version: "v"}},
			models.CatalogoUsuarios: {1: {ID: 1, Nombre: "Sistema", Activo: true}, 2: {ID: 2, Nombre: "Baja", Activo: false}},
		},
		personas: map[int]models.Persona{1: {ID: 1, Nombre: "Ana", OficinaID: 2, Activo: true, Version: "v"}},
		tipos:    map[int]models.Tipo{},
	}
}

func noEncontrado(que string, id int) error {
	return fmt.Errorf("%w: %s %d", repository.ErrNoEncontrado, que, id)
}

func (f *fakeCatalogos) ListCatalogo(_ context.Context, t models.TablaCatalogo, _ bool) ([]models.Catalogo, error) {
	var out []models.Catalogo
	for _, c := range f.catalogos[t] {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeCatalogos) GetCatalogo(_ context.Context, t models.TablaCatalogo, id int) (models.Catalogo, error) {
	c, ok := f.catalogos[t][id]
	if !ok {
		return c, noEncontrado(string(t), id)
	}
	return c, nil
}

func (f *fakeCatalogos) CreateCatalogo(_ context.Context, t models.TablaCatalogo, c models.Catalogo, u int) (models.Catalogo, error) {
	c.ID, f.usuario = len(f.catalogos[t])+1, u
	f.catalogos[t][c.ID] = c
	return c, nil
}

func (f *fakeCatalogos) UpdateCatalogo(_ context.Context, t models.TablaCatalogo, c models.Catalogo, u int) (models.Catalogo, error) {
	f.catalogos[t][c.ID], f.usuario = c, u
	return c, nil
}

func (f *fakeCatalogos) ListPersonas(context.Context, models.FiltroPersonas) ([]models.Persona, error) {
	return nil, nil
}

func (f *fakeCatalogos) GetPersona(_ context.Context, id int) (models.Persona, error) {
	p, ok := f.personas[id]
	if !ok {
		return p, noEncontrado("persona", id)
	}
	return p, nil
}

func (f *fakeCatalogos) CreatePersona(_ context.Context, p models.Persona, u int) (models.Persona, error) {
	p.ID, f.usuario = len(f.personas)+1, u
	f.personas[p.ID] = p
	return p, nil
}

func (f *fakeCatalogos) UpdatePersona(_ context.Context, p models.Persona, u int) (models.Persona, error) {
	f.personas[p.ID], f.usuario = p, u
	return p, nil
}

func (f *fakeCatalogos) ListTipos(context.Context, models.Clase, bool) ([]models.Tipo, error) {
	return nil, nil
}

func (f *fakeCatalogos) GetTipo(_ context.Context, id int) (models.Tipo, error) {
	t, ok := f.tipos[id]
	if !ok {
		return t, noEncontrado("tipo", id)
	}
	return t, nil
}

func (f *fakeCatalogos) CreateTipo(_ context.Context, t models.Tipo, u int) (models.Tipo, error) {
	t.ID, f.usuario = len(f.tipos)+1, u
	f.tipos[t.ID] = t
	return t, nil
}

func (f *fakeCatalogos) UpdateTipo(_ context.Context, t models.Tipo, u int) (models.Tipo, error) {
	f.tipos[t.ID], f.usuario = t, u
	return t, nil
}

func TestCatalogos_NombreNormalizadoYUsuario(t *testing.T) {
	repo := newFakeCatalogos()
	s := NewCatalogoService(repo)
	ctx := ConUsuario(context.Background(), 1)

	c, err := s.CrearCatalogo(ctx, models.CatalogoOficinas, models.Catalogo{Nombre: "  Piso PB   /  Municipios ", Activo: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.Nombre != "Piso PB / Municipios" {
		t.Errorf("nombre = %q", c.Nombre)
	}
	if repo.usuario != 1 {
		t.Errorf("usuario guardado = %d, se esperaba 1", repo.usuario)
	}
}

func TestCatalogos_Rechazos(t *testing.T) {
	s := NewCatalogoService(newFakeCatalogos())
	bg := context.Background()
	casos := []struct {
		nombre string
		err    error
		hacer  func() error
	}{
		{"nombre vacío", ErrDatoInvalido, func() error {
			_, err := s.CrearCatalogo(bg, models.CatalogoPuestos, models.Catalogo{Nombre: "   "})
			return err
		}},
		{"nombre demasiado largo", ErrDatoInvalido, func() error {
			_, err := s.CrearCatalogo(bg, models.CatalogoOficinas, models.Catalogo{Nombre: fmt.Sprintf("%0151d", 0)})
			return err
		}},
		{"actualizar sin versión", ErrDatoInvalido, func() error {
			_, err := s.ActualizarCatalogo(bg, models.CatalogoPuestos, models.Catalogo{ID: 1, Nombre: "X"})
			return err
		}},
		{"ID inválido", ErrInvalidID, func() error {
			_, err := s.ObtenerCatalogo(bg, models.CatalogoPuestos, 0)
			return err
		}},
		{"usuario inexistente", ErrUsuarioInvalido, func() error {
			_, err := s.CrearCatalogo(ConUsuario(bg, 99), models.CatalogoPuestos, models.Catalogo{Nombre: "X"})
			return err
		}},
		{"usuario inactivo", ErrUsuarioInvalido, func() error {
			_, err := s.CrearCatalogo(ConUsuario(bg, 2), models.CatalogoPuestos, models.Catalogo{Nombre: "X"})
			return err
		}},
		{"persona sin oficina", ErrDatoInvalido, func() error {
			_, err := s.CrearPersona(bg, models.Persona{Nombre: "Juan"})
			return err
		}},
		{"persona en oficina inexistente", ErrDatoInvalido, func() error {
			_, err := s.CrearPersona(bg, models.Persona{Nombre: "Juan", OficinaID: 9})
			return err
		}},
		{"persona en oficina inactiva", ErrDatoInvalido, func() error {
			_, err := s.CrearPersona(bg, models.Persona{Nombre: "Juan", OficinaID: 2})
			return err
		}},
		{"persona con puesto inexistente", ErrDatoInvalido, func() error {
			_, err := s.CrearPersona(bg, models.Persona{Nombre: "Juan", OficinaID: 1, PuestoID: 9})
			return err
		}},
		{"clase desconocida", ErrDatoInvalido, func() error {
			_, err := s.CrearTipo(bg, models.Tipo{Nombre: "Silla", Clase: "MUEBLE"})
			return err
		}},
		{"insumo prestable", ErrDatoInvalido, func() error {
			_, err := s.CrearTipo(bg, models.Tipo{Nombre: "Mouse", Clase: models.ClaseInsumo, Prestable: true})
			return err
		}},
		{"filtro de clase desconocida", ErrDatoInvalido, func() error {
			_, err := s.ListarTipos(bg, "mueble", false)
			return err
		}},
	}
	for _, c := range casos {
		if err := c.hacer(); !errors.Is(err, c.err) {
			t.Errorf("%s: err = %v, se esperaba %v", c.nombre, err, c.err)
		}
	}
}

// Editar a una persona que ya estaba en una oficina desactivada no obliga a
// moverla; cambiarla a otra oficina inactiva sí se rechaza.
func TestCatalogos_PersonaEnOficinaInactiva(t *testing.T) {
	s := NewCatalogoService(newFakeCatalogos())
	bg := context.Background()
	p, err := s.ActualizarPersona(bg, models.Persona{ID: 1, Nombre: "Ana  María", OficinaID: 2, Activo: true, Version: "v"})
	if err != nil {
		t.Fatalf("misma oficina inactiva: %v", err)
	}
	if p.Nombre != "Ana María" {
		t.Errorf("nombre = %q", p.Nombre)
	}
	if _, err := s.ActualizarPersona(bg, models.Persona{ID: 1, Nombre: "Ana", OficinaID: 1, PuestoID: 1, Version: "v"}); err != nil {
		t.Errorf("a una oficina activa con puesto: %v", err)
	}
	if _, err := s.ActualizarPersona(bg, models.Persona{ID: 1, Nombre: "Ana", OficinaID: 2, Version: "v"}); !errors.Is(err, ErrDatoInvalido) {
		t.Errorf("volver a una oficina inactiva: err = %v", err)
	}
}

func TestCatalogos_ClaseSinDistinguirMayusculas(t *testing.T) {
	s := NewCatalogoService(newFakeCatalogos())
	tipo, err := s.CrearTipo(context.Background(), models.Tipo{Nombre: "Notebook", Clase: " equipo ", Prestable: true})
	if err != nil {
		t.Fatal(err)
	}
	if tipo.Clase != models.ClaseEquipo {
		t.Errorf("clase = %q", tipo.Clase)
	}
}
