-- Modelo nuevo (DER acordado el 2026-10-09): separa Equipos, Componentes e
-- Insumos; Oficinas, Puestos y Personas pasan a ser entidades; préstamos e
-- historiales con el usuario que registró cada cambio.
--
-- BORRADOR: esta carpeta no se aplica sola. Pasa a migrations/sqlserver/ cuando
-- el backend use estas tablas. Convive con Items, Movements y Categories hasta
-- que la migración de datos (etapa 2) las reemplace.
--
-- Reglas comunes:
--   - No se borra nada: bajas con fecha y motivo; personas, oficinas y tipos inactivos.
--     Las claves foráneas no tienen cascada.
--   - Tablas editables: CreadoEn, ActualizadoEn, ActualizadoPor y RowVersion
--     (concurrencia optimista). Historiales: solo crecen, con Fecha y UsuarioID.
--   - El tipo de un equipo, componente o insumo tiene que ser de su clase: la
--     columna calculada Clase + la FK compuesta (TipoID, Clase) lo garantiza, y
--     además impide cambiar la clase de un tipo que ya tiene ítems.

CREATE TABLE dbo.Usuarios (
    ID             INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Usuarios PRIMARY KEY,
    Nombre         NVARCHAR(100)      NOT NULL CONSTRAINT UX_Usuarios_Nombre UNIQUE,
    Activo         BIT                NOT NULL CONSTRAINT DF_Usuarios_Activo DEFAULT 1,
    CreadoEn       DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Usuarios_CreadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoEn  DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Usuarios_ActualizadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoPor INT                NULL CONSTRAINT FK_Usuarios_Usuario REFERENCES dbo.Usuarios (ID),
    RowVersion     ROWVERSION         NOT NULL,
    CONSTRAINT CK_Usuarios_Nombre CHECK (Nombre <> N'')
);

-- Usuario 1: cambios hechos por el sistema (migración, procesos automáticos).
INSERT INTO dbo.Usuarios (Nombre) VALUES (N'Sistema');

CREATE TABLE dbo.Oficinas (
    ID             INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Oficinas PRIMARY KEY,
    Nombre         NVARCHAR(150)      NOT NULL CONSTRAINT UX_Oficinas_Nombre UNIQUE,
    Activo         BIT                NOT NULL CONSTRAINT DF_Oficinas_Activo DEFAULT 1,
    CreadoEn       DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Oficinas_CreadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoEn  DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Oficinas_ActualizadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoPor INT                NULL CONSTRAINT FK_Oficinas_Usuario REFERENCES dbo.Usuarios (ID),
    RowVersion     ROWVERSION         NOT NULL,
    CONSTRAINT CK_Oficinas_Nombre CHECK (Nombre <> N'')
);

-- Puesto = cargo de la persona (Contador, Técnico…).
CREATE TABLE dbo.Puestos (
    ID             INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Puestos PRIMARY KEY,
    Nombre         NVARCHAR(100)      NOT NULL CONSTRAINT UX_Puestos_Nombre UNIQUE,
    Activo         BIT                NOT NULL CONSTRAINT DF_Puestos_Activo DEFAULT 1,
    CreadoEn       DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Puestos_CreadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoEn  DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Puestos_ActualizadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoPor INT                NULL CONSTRAINT FK_Puestos_Usuario REFERENCES dbo.Usuarios (ID),
    RowVersion     ROWVERSION         NOT NULL,
    CONSTRAINT CK_Puestos_Nombre CHECK (Nombre <> N'')
);

-- Puesto y oficina pueden faltar en las personas que vienen del texto libre
-- "Asignado a" del sistema anterior; la interfaz los pide al cargar o editar.
CREATE TABLE dbo.Personas (
    ID             INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Personas PRIMARY KEY,
    Nombre         NVARCHAR(100)      NOT NULL,
    Apellido       NVARCHAR(100)      NOT NULL CONSTRAINT DF_Personas_Apellido DEFAULT N'',
    PuestoID       INT                NULL CONSTRAINT FK_Personas_Puesto  REFERENCES dbo.Puestos (ID),
    OficinaID      INT                NULL CONSTRAINT FK_Personas_Oficina REFERENCES dbo.Oficinas (ID),
    Activo         BIT                NOT NULL CONSTRAINT DF_Personas_Activo DEFAULT 1,
    CreadoEn       DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Personas_CreadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoEn  DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Personas_ActualizadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoPor INT                NULL CONSTRAINT FK_Personas_Usuario REFERENCES dbo.Usuarios (ID),
    RowVersion     ROWVERSION         NOT NULL,
    CONSTRAINT CK_Personas_Nombre CHECK (Nombre <> N'')
);
CREATE INDEX IX_Personas_OficinaID ON dbo.Personas (OficinaID);

CREATE TABLE dbo.Tipos (
    ID             INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Tipos PRIMARY KEY,
    Nombre         NVARCHAR(100)      NOT NULL CONSTRAINT UX_Tipos_Nombre UNIQUE,
    Clase          VARCHAR(12)        NOT NULL,
    Prestable      BIT                NOT NULL CONSTRAINT DF_Tipos_Prestable DEFAULT 0,
    Activo         BIT                NOT NULL CONSTRAINT DF_Tipos_Activo DEFAULT 1,
    CreadoEn       DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Tipos_CreadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoEn  DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Tipos_ActualizadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoPor INT                NULL CONSTRAINT FK_Tipos_Usuario REFERENCES dbo.Usuarios (ID),
    RowVersion     ROWVERSION         NOT NULL,
    CONSTRAINT CK_Tipos_Nombre    CHECK (Nombre <> N''),
    CONSTRAINT CK_Tipos_Clase     CHECK (Clase IN ('EQUIPO', 'COMPONENTE', 'INSUMO')),
    CONSTRAINT CK_Tipos_Prestable CHECK (Prestable = 0 OR Clase = 'EQUIPO'),  -- solo se prestan equipos
    CONSTRAINT UX_Tipos_ID_Clase  UNIQUE (ID, Clase)                          -- destino de las FK compuestas
);

CREATE TABLE dbo.Equipos (
    ID               INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Equipos PRIMARY KEY,
    IDAnterior       INT                NULL,  -- ID en Items, para verificar la migración
    NumeroInventario NVARCHAR(100)      NOT NULL CONSTRAINT DF_Equipos_NumeroInventario DEFAULT N'',  -- vacío = pendiente de numerar
    TipoID           INT                NOT NULL,
    Clase            AS CONVERT(VARCHAR(12), 'EQUIPO') PERSISTED NOT NULL,
    Marca            NVARCHAR(100)      NOT NULL CONSTRAINT DF_Equipos_Marca DEFAULT N'',
    Modelo           NVARCHAR(100)      NOT NULL CONSTRAINT DF_Equipos_Modelo DEFAULT N'',
    NumeroSerie      NVARCHAR(100)      NOT NULL CONSTRAINT DF_Equipos_NumeroSerie DEFAULT N'',
    Estado           VARCHAR(20)        NOT NULL CONSTRAINT DF_Equipos_Estado DEFAULT 'OPERATIVO',
    OficinaID        INT                NOT NULL CONSTRAINT FK_Equipos_Oficina REFERENCES dbo.Oficinas (ID),
    PersonaID        INT                NULL CONSTRAINT FK_Equipos_Persona REFERENCES dbo.Personas (ID),
    Observacion      NVARCHAR(1000)     NOT NULL CONSTRAINT DF_Equipos_Observacion DEFAULT N'',
    FechaBaja        DATETIMEOFFSET(7)  NULL,
    MotivoBaja       NVARCHAR(500)      NOT NULL CONSTRAINT DF_Equipos_MotivoBaja DEFAULT N'',
    CreadoEn         DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Equipos_CreadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoEn    DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Equipos_ActualizadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoPor   INT                NULL CONSTRAINT FK_Equipos_Usuario REFERENCES dbo.Usuarios (ID),
    RowVersion       ROWVERSION         NOT NULL,
    CONSTRAINT FK_Equipos_Tipo  FOREIGN KEY (TipoID, Clase) REFERENCES dbo.Tipos (ID, Clase),
    CONSTRAINT CK_Equipos_Estado CHECK (Estado IN ('OPERATIVO', 'EN_REPARACION', 'FUERA_DE_SERVICIO', 'BAJA')),
    CONSTRAINT CK_Equipos_Baja   CHECK ((Estado = 'BAJA' AND FechaBaja IS NOT NULL AND MotivoBaja <> N'')
                                     OR (Estado <> 'BAJA' AND FechaBaja IS NULL AND MotivoBaja = N''))
);
CREATE UNIQUE INDEX UX_Equipos_NumeroInventario ON dbo.Equipos (NumeroInventario) WHERE NumeroInventario <> N'';
CREATE UNIQUE INDEX UX_Equipos_NumeroSerie ON dbo.Equipos (NumeroSerie) WHERE NumeroSerie <> N'';
CREATE UNIQUE INDEX UX_Equipos_IDAnterior  ON dbo.Equipos (IDAnterior)  WHERE IDAnterior IS NOT NULL;
CREATE INDEX IX_Equipos_OficinaID ON dbo.Equipos (OficinaID);
CREATE INDEX IX_Equipos_PersonaID ON dbo.Equipos (PersonaID) WHERE PersonaID IS NOT NULL;
CREATE INDEX IX_Equipos_TipoID    ON dbo.Equipos (TipoID);

-- El N° de inventario del componente es fijo (al instalarse suele tomar el de
-- la PC) y puede repetirse. Está dentro de un equipo o en una oficina, nunca
-- en los dos ni en ninguno.
CREATE TABLE dbo.Componentes (
    ID               INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Componentes PRIMARY KEY,
    IDAnterior       INT                NULL,
    NumeroInventario NVARCHAR(100)      NOT NULL CONSTRAINT DF_Componentes_NumeroInventario DEFAULT N'',
    TipoID           INT                NOT NULL,
    Clase            AS CONVERT(VARCHAR(12), 'COMPONENTE') PERSISTED NOT NULL,
    Marca            NVARCHAR(100)      NOT NULL CONSTRAINT DF_Componentes_Marca DEFAULT N'',
    Modelo           NVARCHAR(100)      NOT NULL CONSTRAINT DF_Componentes_Modelo DEFAULT N'',
    NumeroSerie      NVARCHAR(100)      NOT NULL CONSTRAINT DF_Componentes_NumeroSerie DEFAULT N'',
    Estado           VARCHAR(20)        NOT NULL CONSTRAINT DF_Componentes_Estado DEFAULT 'OPERATIVO',
    EquipoID         INT                NULL CONSTRAINT FK_Componentes_Equipo  REFERENCES dbo.Equipos (ID),
    OficinaID        INT                NULL CONSTRAINT FK_Componentes_Oficina REFERENCES dbo.Oficinas (ID),
    Observacion      NVARCHAR(1000)     NOT NULL CONSTRAINT DF_Componentes_Observacion DEFAULT N'',
    FechaBaja        DATETIMEOFFSET(7)  NULL,
    MotivoBaja       NVARCHAR(500)      NOT NULL CONSTRAINT DF_Componentes_MotivoBaja DEFAULT N'',
    CreadoEn         DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Componentes_CreadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoEn    DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Componentes_ActualizadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoPor   INT                NULL CONSTRAINT FK_Componentes_Usuario REFERENCES dbo.Usuarios (ID),
    RowVersion       ROWVERSION         NOT NULL,
    CONSTRAINT FK_Componentes_Tipo  FOREIGN KEY (TipoID, Clase) REFERENCES dbo.Tipos (ID, Clase),
    CONSTRAINT CK_Componentes_Ubicacion CHECK ((EquipoID IS NOT NULL AND OficinaID IS NULL)
                                            OR (EquipoID IS NULL AND OficinaID IS NOT NULL)),
    CONSTRAINT CK_Componentes_Estado CHECK (Estado IN ('OPERATIVO', 'EN_REPARACION', 'FUERA_DE_SERVICIO', 'BAJA')),
    CONSTRAINT CK_Componentes_Baja   CHECK ((Estado = 'BAJA' AND FechaBaja IS NOT NULL AND MotivoBaja <> N'')
                                         OR (Estado <> 'BAJA' AND FechaBaja IS NULL AND MotivoBaja = N''))
);
CREATE UNIQUE INDEX UX_Componentes_NumeroSerie ON dbo.Componentes (NumeroSerie) WHERE NumeroSerie <> N'';
CREATE UNIQUE INDEX UX_Componentes_IDAnterior  ON dbo.Componentes (IDAnterior)  WHERE IDAnterior IS NOT NULL;
CREATE INDEX IX_Componentes_EquipoID         ON dbo.Componentes (EquipoID)  WHERE EquipoID IS NOT NULL;
CREATE INDEX IX_Componentes_OficinaID        ON dbo.Componentes (OficinaID) WHERE OficinaID IS NOT NULL;
CREATE INDEX IX_Componentes_NumeroInventario ON dbo.Componentes (NumeroInventario) WHERE NumeroInventario <> N'';

-- Stock de nuestra oficina. Las entregas a otras quedan en MovimientosInsumo.
-- Marca y modelo comparan sin mayúsculas ni tildes (intercalación de la base).
CREATE TABLE dbo.Insumos (
    ID             INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Insumos PRIMARY KEY,
    IDAnterior     INT                NULL,
    TipoID         INT                NOT NULL,
    Clase          AS CONVERT(VARCHAR(12), 'INSUMO') PERSISTED NOT NULL,
    Marca          NVARCHAR(100)      NOT NULL CONSTRAINT DF_Insumos_Marca DEFAULT N'',
    Modelo         NVARCHAR(100)      NOT NULL CONSTRAINT DF_Insumos_Modelo DEFAULT N'',
    Stock          INT                NOT NULL CONSTRAINT DF_Insumos_Stock DEFAULT 0,
    StockMinimo    INT                NOT NULL CONSTRAINT DF_Insumos_StockMinimo DEFAULT 0,
    Observacion    NVARCHAR(1000)     NOT NULL CONSTRAINT DF_Insumos_Observacion DEFAULT N'',
    Activo         BIT                NOT NULL CONSTRAINT DF_Insumos_Activo DEFAULT 1,
    CreadoEn       DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Insumos_CreadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoEn  DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Insumos_ActualizadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoPor INT                NULL CONSTRAINT FK_Insumos_Usuario REFERENCES dbo.Usuarios (ID),
    RowVersion     ROWVERSION         NOT NULL,
    CONSTRAINT FK_Insumos_Tipo  FOREIGN KEY (TipoID, Clase) REFERENCES dbo.Tipos (ID, Clase),
    CONSTRAINT UX_Insumos_TipoMarcaModelo UNIQUE (TipoID, Marca, Modelo),
    CONSTRAINT CK_Insumos_Stock CHECK (Stock >= 0 AND StockMinimo >= 0)
);
CREATE UNIQUE INDEX UX_Insumos_IDAnterior ON dbo.Insumos (IDAnterior) WHERE IDAnterior IS NOT NULL;

-- Préstamo en el día. No cambia la persona ni la oficina asignadas al equipo.
-- Que el tipo sea prestable y el equipo no esté de baja lo controla el servicio.
CREATE TABLE dbo.Prestamos (
    ID                 INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Prestamos PRIMARY KEY,
    EquipoID           INT                NOT NULL CONSTRAINT FK_Prestamos_Equipo  REFERENCES dbo.Equipos (ID),
    PersonaID          INT                NOT NULL CONSTRAINT FK_Prestamos_Persona REFERENCES dbo.Personas (ID),
    FechaSalida        DATETIMEOFFSET(7)  NOT NULL,
    DevolucionPrevista DATETIMEOFFSET(7)  NOT NULL,
    FechaDevolucion    DATETIMEOFFSET(7)  NULL,  -- NULL = todavía no se devolvió
    Observacion        NVARCHAR(1000)     NOT NULL CONSTRAINT DF_Prestamos_Observacion DEFAULT N'',
    UsuarioID          INT                NOT NULL CONSTRAINT FK_Prestamos_Usuario REFERENCES dbo.Usuarios (ID),
    CreadoEn           DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Prestamos_CreadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoEn      DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_Prestamos_ActualizadoEn DEFAULT SYSDATETIMEOFFSET(),
    ActualizadoPor     INT                NULL CONSTRAINT FK_Prestamos_ActualizadoPor REFERENCES dbo.Usuarios (ID),
    RowVersion         ROWVERSION         NOT NULL,
    CONSTRAINT CK_Prestamos_Fechas CHECK (DevolucionPrevista >= FechaSalida
                                      AND (FechaDevolucion IS NULL OR FechaDevolucion >= FechaSalida))
);
-- Un equipo no puede tener dos préstamos abiertos.
CREATE UNIQUE INDEX UX_Prestamos_Abierto ON dbo.Prestamos (EquipoID) WHERE FechaDevolucion IS NULL;
CREATE INDEX IX_Prestamos_PersonaID ON dbo.Prestamos (PersonaID);

-- Cantidad: unidades que entran o salen (> 0); en AJUSTE, la diferencia con
-- signo respecto del stock anterior (≠ 0). Toda SALIDA dice a quién o a dónde.
CREATE TABLE dbo.MovimientosInsumo (
    ID          INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_MovimientosInsumo PRIMARY KEY,
    InsumoID    INT                NOT NULL CONSTRAINT FK_MovimientosInsumo_Insumo  REFERENCES dbo.Insumos (ID),
    Tipo        VARCHAR(10)        NOT NULL,
    Cantidad    INT                NOT NULL,
    PersonaID   INT                NULL CONSTRAINT FK_MovimientosInsumo_Persona REFERENCES dbo.Personas (ID),
    OficinaID   INT                NULL CONSTRAINT FK_MovimientosInsumo_Oficina REFERENCES dbo.Oficinas (ID),
    Observacion NVARCHAR(1000)     NOT NULL CONSTRAINT DF_MovimientosInsumo_Observacion DEFAULT N'',
    Fecha       DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_MovimientosInsumo_Fecha DEFAULT SYSDATETIMEOFFSET(),
    UsuarioID   INT                NOT NULL CONSTRAINT FK_MovimientosInsumo_Usuario REFERENCES dbo.Usuarios (ID),
    CONSTRAINT CK_MovimientosInsumo_Tipo     CHECK (Tipo IN ('ENTRADA', 'SALIDA', 'AJUSTE')),
    CONSTRAINT CK_MovimientosInsumo_Cantidad CHECK ((Tipo IN ('ENTRADA', 'SALIDA') AND Cantidad > 0)
                                                 OR (Tipo = 'AJUSTE' AND Cantidad <> 0)),
    CONSTRAINT CK_MovimientosInsumo_Destino  CHECK (Tipo <> 'SALIDA' OR PersonaID IS NOT NULL OR OficinaID IS NOT NULL)
);
CREATE INDEX IX_MovimientosInsumo_InsumoID ON dbo.MovimientosInsumo (InsumoID, ID);

-- Historiales: solo crecen; los escribe el servicio en la misma transacción
-- que el cambio. Anterior NULL = alta; Estado = estado después del cambio.
CREATE TABLE dbo.HistorialEquipos (
    ID                INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_HistorialEquipos PRIMARY KEY,
    EquipoID          INT                NOT NULL CONSTRAINT FK_HistorialEquipos_Equipo REFERENCES dbo.Equipos (ID),
    OficinaAnteriorID INT                NULL CONSTRAINT FK_HistorialEquipos_OficinaAnterior REFERENCES dbo.Oficinas (ID),
    OficinaNuevaID    INT                NULL CONSTRAINT FK_HistorialEquipos_OficinaNueva    REFERENCES dbo.Oficinas (ID),
    PersonaAnteriorID INT                NULL CONSTRAINT FK_HistorialEquipos_PersonaAnterior REFERENCES dbo.Personas (ID),
    PersonaNuevaID    INT                NULL CONSTRAINT FK_HistorialEquipos_PersonaNueva    REFERENCES dbo.Personas (ID),
    Estado            VARCHAR(20)        NOT NULL,
    Observacion       NVARCHAR(1000)     NOT NULL CONSTRAINT DF_HistorialEquipos_Observacion DEFAULT N'',
    Fecha             DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_HistorialEquipos_Fecha DEFAULT SYSDATETIMEOFFSET(),
    UsuarioID         INT                NOT NULL CONSTRAINT FK_HistorialEquipos_Usuario REFERENCES dbo.Usuarios (ID),
    CONSTRAINT CK_HistorialEquipos_Estado CHECK (Estado IN ('OPERATIVO', 'EN_REPARACION', 'FUERA_DE_SERVICIO', 'BAJA'))
);
CREATE INDEX IX_HistorialEquipos_EquipoID ON dbo.HistorialEquipos (EquipoID, ID);

-- EquipoAnterior/EquipoNuevo NULL = estaba / quedó fuera de un equipo (en una oficina).
CREATE TABLE dbo.HistorialComponentes (
    ID               INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_HistorialComponentes PRIMARY KEY,
    ComponenteID     INT                NOT NULL CONSTRAINT FK_HistorialComponentes_Componente REFERENCES dbo.Componentes (ID),
    EquipoAnteriorID INT                NULL CONSTRAINT FK_HistorialComponentes_EquipoAnterior REFERENCES dbo.Equipos (ID),
    EquipoNuevoID    INT                NULL CONSTRAINT FK_HistorialComponentes_EquipoNuevo    REFERENCES dbo.Equipos (ID),
    Observacion      NVARCHAR(1000)     NOT NULL CONSTRAINT DF_HistorialComponentes_Observacion DEFAULT N'',
    Fecha            DATETIMEOFFSET(7)  NOT NULL CONSTRAINT DF_HistorialComponentes_Fecha DEFAULT SYSDATETIMEOFFSET(),
    UsuarioID        INT                NOT NULL CONSTRAINT FK_HistorialComponentes_Usuario REFERENCES dbo.Usuarios (ID)
);
CREATE INDEX IX_HistorialComponentes_ComponenteID ON dbo.HistorialComponentes (ComponenteID, ID);
