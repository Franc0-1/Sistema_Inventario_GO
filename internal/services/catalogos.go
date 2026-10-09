package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"inventario/internal/models"
	"inventario/internal/repository"
)

var (
	// ErrDatoInvalido: un dato del modelo nuevo no cumple las reglas.
	ErrDatoInvalido = errors.New("dato inválido")
	// ErrUsuarioInvalido: el usuario que hace el cambio no existe o está inactivo.
	ErrUsuarioInvalido = errors.New("usuario inválido")
)

// Usuario que hace los cambios. Lo agrega el handler y el servicio lo guarda
// en ActualizadoPor y en los historiales.
type claveUsuario struct{}

func ConUsuario(ctx context.Context, id int) context.Context {
	return context.WithValue(ctx, claveUsuario{}, id)
}

func UsuarioDe(ctx context.Context) int {
	id, _ := ctx.Value(claveUsuario{}).(int)
	return id
}

// CatalogoService administra oficinas, puestos, usuarios, personas y tipos.
// Nada se elimina: se desactiva. Las actualizaciones reemplazan el registro
// completo y exigen la Version leída (si otro lo cambió antes, ErrConflict).
type CatalogoService interface {
	ListarCatalogo(ctx context.Context, t models.TablaCatalogo, soloActivos bool) ([]models.Catalogo, error)
	ObtenerCatalogo(ctx context.Context, t models.TablaCatalogo, id int) (models.Catalogo, error)
	CrearCatalogo(ctx context.Context, t models.TablaCatalogo, c models.Catalogo) (models.Catalogo, error)
	ActualizarCatalogo(ctx context.Context, t models.TablaCatalogo, c models.Catalogo) (models.Catalogo, error)

	ListarPersonas(ctx context.Context, f models.FiltroPersonas) ([]models.Persona, error)
	ObtenerPersona(ctx context.Context, id int) (models.Persona, error)
	CrearPersona(ctx context.Context, p models.Persona) (models.Persona, error)
	ActualizarPersona(ctx context.Context, p models.Persona) (models.Persona, error)

	ListarTipos(ctx context.Context, clase string, soloActivos bool) ([]models.Tipo, error)
	ObtenerTipo(ctx context.Context, id int) (models.Tipo, error)
	CrearTipo(ctx context.Context, t models.Tipo) (models.Tipo, error)
	ActualizarTipo(ctx context.Context, t models.Tipo) (models.Tipo, error)
}

type catalogoService struct {
	repo repository.CatalogoRepository
}

func NewCatalogoService(repo repository.CatalogoRepository) CatalogoService {
	return &catalogoService{repo: repo}
}

// texto colapsa los espacios y controla el largo máximo de la columna.
func texto(valor, campo string, max int, obligatorio bool) (string, error) {
	v := strings.Join(strings.Fields(valor), " ")
	switch {
	case obligatorio && v == "":
		return v, fmt.Errorf("%w: %s es obligatorio", ErrDatoInvalido, campo)
	case utf8.RuneCountInString(v) > max:
		return v, fmt.Errorf("%w: %s admite hasta %d caracteres", ErrDatoInvalido, campo, max)
	}
	return v, nil
}

func validarID(id int) error {
	if id <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidID, id)
	}
	return nil
}

func validarEdicion(id int, version string) error {
	if err := validarID(id); err != nil {
		return err
	}
	if version == "" {
		return fmt.Errorf("%w: falta la versión del registro; recargá los datos", ErrDatoInvalido)
	}
	return nil
}

// actorDe devuelve el usuario del contexto verificando que exista y esté
// activo. Sin usuario devuelve 0, salvo que sea obligatorio (los cambios que
// dejan historial siempre dicen quién los hizo).
func actorDe(ctx context.Context, repo repository.CatalogoRepository, obligatorio bool) (int, error) {
	id := UsuarioDe(ctx)
	if id == 0 {
		if obligatorio {
			return 0, fmt.Errorf("%w: indicá quién hace el cambio (X-Usuario-ID)", ErrUsuarioInvalido)
		}
		return 0, nil
	}
	u, err := repo.GetCatalogo(ctx, models.CatalogoUsuarios, id)
	if errors.Is(err, repository.ErrNoEncontrado) || (err == nil && !u.Activo) {
		return 0, fmt.Errorf("%w: %d", ErrUsuarioInvalido, id)
	}
	return id, err
}

func (s *catalogoService) actor(ctx context.Context) (int, error) {
	return actorDe(ctx, s.repo, false)
}

// ---- Oficinas, puestos y usuarios ----

func (s *catalogoService) ListarCatalogo(ctx context.Context, t models.TablaCatalogo, soloActivos bool) ([]models.Catalogo, error) {
	return s.repo.ListCatalogo(ctx, t, soloActivos)
}

func (s *catalogoService) ObtenerCatalogo(ctx context.Context, t models.TablaCatalogo, id int) (models.Catalogo, error) {
	if err := validarID(id); err != nil {
		return models.Catalogo{}, err
	}
	return s.repo.GetCatalogo(ctx, t, id)
}

func normalizarCatalogo(t models.TablaCatalogo, c models.Catalogo) (models.Catalogo, error) {
	max := 100
	if t == models.CatalogoOficinas {
		max = 150
	}
	var err error
	c.Nombre, err = texto(c.Nombre, "el nombre", max, true)
	return c, err
}

func (s *catalogoService) CrearCatalogo(ctx context.Context, t models.TablaCatalogo, c models.Catalogo) (models.Catalogo, error) {
	c, err := normalizarCatalogo(t, c)
	if err != nil {
		return c, err
	}
	usuario, err := s.actor(ctx)
	if err != nil {
		return c, err
	}
	return s.repo.CreateCatalogo(ctx, t, c, usuario)
}

func (s *catalogoService) ActualizarCatalogo(ctx context.Context, t models.TablaCatalogo, c models.Catalogo) (models.Catalogo, error) {
	if err := validarEdicion(c.ID, c.Version); err != nil {
		return c, err
	}
	c, err := normalizarCatalogo(t, c)
	if err != nil {
		return c, err
	}
	usuario, err := s.actor(ctx)
	if err != nil {
		return c, err
	}
	return s.repo.UpdateCatalogo(ctx, t, c, usuario)
}

// ---- Personas ----

func (s *catalogoService) ListarPersonas(ctx context.Context, f models.FiltroPersonas) ([]models.Persona, error) {
	return s.repo.ListPersonas(ctx, f)
}

func (s *catalogoService) ObtenerPersona(ctx context.Context, id int) (models.Persona, error) {
	if err := validarID(id); err != nil {
		return models.Persona{}, err
	}
	return s.repo.GetPersona(ctx, id)
}

// referenciaActiva verifica que la oficina o el puesto elegido exista y esté activo.
func (s *catalogoService) referenciaActiva(ctx context.Context, t models.TablaCatalogo, que, inactivo string, id int) error {
	c, err := s.repo.GetCatalogo(ctx, t, id)
	switch {
	case errors.Is(err, repository.ErrNoEncontrado):
		return fmt.Errorf("%w: %s %d no existe", ErrDatoInvalido, que, id)
	case err != nil:
		return err
	case !c.Activo:
		return fmt.Errorf("%w: %s %q está %s", ErrDatoInvalido, que, c.Nombre, inactivo)
	}
	return nil
}

// validarPersona normaliza los textos y controla oficina y puesto. Solo se
// exige que estén activos si cambian: editar a alguien que ya estaba en una
// oficina desactivada no obliga a moverlo.
func (s *catalogoService) validarPersona(ctx context.Context, p, anterior models.Persona) (models.Persona, error) {
	var err error
	if p.Nombre, err = texto(p.Nombre, "el nombre", 100, true); err != nil {
		return p, err
	}
	if p.Apellido, err = texto(p.Apellido, "el apellido", 100, false); err != nil {
		return p, err
	}
	if p.OficinaID <= 0 {
		return p, fmt.Errorf("%w: la oficina es obligatoria", ErrDatoInvalido)
	}
	if p.PuestoID < 0 {
		return p, fmt.Errorf("%w: puesto %d inválido", ErrDatoInvalido, p.PuestoID)
	}
	if p.OficinaID != anterior.OficinaID {
		if err := s.referenciaActiva(ctx, models.CatalogoOficinas, "la oficina", "inactiva", p.OficinaID); err != nil {
			return p, err
		}
	}
	if p.PuestoID != 0 && p.PuestoID != anterior.PuestoID {
		if err := s.referenciaActiva(ctx, models.CatalogoPuestos, "el puesto", "inactivo", p.PuestoID); err != nil {
			return p, err
		}
	}
	return p, nil
}

func (s *catalogoService) CrearPersona(ctx context.Context, p models.Persona) (models.Persona, error) {
	p, err := s.validarPersona(ctx, p, models.Persona{})
	if err != nil {
		return p, err
	}
	usuario, err := s.actor(ctx)
	if err != nil {
		return p, err
	}
	return s.repo.CreatePersona(ctx, p, usuario)
}

func (s *catalogoService) ActualizarPersona(ctx context.Context, p models.Persona) (models.Persona, error) {
	if err := validarEdicion(p.ID, p.Version); err != nil {
		return p, err
	}
	anterior, err := s.repo.GetPersona(ctx, p.ID)
	if err != nil {
		return p, err
	}
	if p, err = s.validarPersona(ctx, p, anterior); err != nil {
		return p, err
	}
	usuario, err := s.actor(ctx)
	if err != nil {
		return p, err
	}
	return s.repo.UpdatePersona(ctx, p, usuario)
}

// ---- Tipos ----

func parseClase(v string) (models.Clase, error) {
	c, ok := oneOf(v, models.ClasesValidas)
	if !ok {
		return c, fmt.Errorf("%w: clase %q desconocida (válidas: EQUIPO, COMPONENTE, INSUMO)", ErrDatoInvalido, v)
	}
	return c, nil
}

func (s *catalogoService) ListarTipos(ctx context.Context, clase string, soloActivos bool) ([]models.Tipo, error) {
	var c models.Clase
	if strings.TrimSpace(clase) != "" {
		var err error
		if c, err = parseClase(clase); err != nil {
			return nil, err
		}
	}
	return s.repo.ListTipos(ctx, c, soloActivos)
}

func (s *catalogoService) ObtenerTipo(ctx context.Context, id int) (models.Tipo, error) {
	if err := validarID(id); err != nil {
		return models.Tipo{}, err
	}
	return s.repo.GetTipo(ctx, id)
}

func validarTipo(t models.Tipo) (models.Tipo, error) {
	var err error
	if t.Nombre, err = texto(t.Nombre, "el nombre", 100, true); err != nil {
		return t, err
	}
	if t.Clase, err = parseClase(string(t.Clase)); err != nil {
		return t, err
	}
	if t.Prestable && t.Clase != models.ClaseEquipo {
		return t, fmt.Errorf("%w: solo los tipos de equipo pueden prestarse", ErrDatoInvalido)
	}
	return t, nil
}

func (s *catalogoService) CrearTipo(ctx context.Context, t models.Tipo) (models.Tipo, error) {
	t, err := validarTipo(t)
	if err != nil {
		return t, err
	}
	usuario, err := s.actor(ctx)
	if err != nil {
		return t, err
	}
	return s.repo.CreateTipo(ctx, t, usuario)
}

func (s *catalogoService) ActualizarTipo(ctx context.Context, t models.Tipo) (models.Tipo, error) {
	if err := validarEdicion(t.ID, t.Version); err != nil {
		return t, err
	}
	t, err := validarTipo(t)
	if err != nil {
		return t, err
	}
	usuario, err := s.actor(ctx)
	if err != nil {
		return t, err
	}
	return s.repo.UpdateTipo(ctx, t, usuario)
}
