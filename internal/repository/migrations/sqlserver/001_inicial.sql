-- Esquema inicial: refleja las hojas Inventario, Movimientos y Categorias del
-- Excel, con las mismas reglas de integridad como restricciones de la base.
-- La base usa la intercalación Modern_Spanish_CI_AI: las comparaciones y los
-- índices únicos no distinguen mayúsculas ni tildes, igual que la aplicación.

CREATE TABLE dbo.Items (
    ID              INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Items PRIMARY KEY,  -- nunca reutiliza IDs
    InventoryNumber NVARCHAR(100)     NOT NULL CONSTRAINT DF_Items_InventoryNumber DEFAULT N'',
    HasInventory    BIT               NOT NULL,
    DeviceType      NVARCHAR(100)     NOT NULL,
    Brand           NVARCHAR(100)     NOT NULL,
    Model           NVARCHAR(100)     NOT NULL,
    SerialNumber    NVARCHAR(100)     NOT NULL CONSTRAINT DF_Items_SerialNumber DEFAULT N'',
    Quantity        INT               NOT NULL,
    Location        NVARCHAR(150)     NOT NULL,
    Status          VARCHAR(20)       NOT NULL,
    Notes           NVARCHAR(1000)    NOT NULL CONSTRAINT DF_Items_Notes DEFAULT N'',
    CreatedAt       DATETIMEOFFSET(7) NOT NULL,
    UpdatedAt       DATETIMEOFFSET(7) NOT NULL,
    Availability    VARCHAR(12)       NOT NULL CONSTRAINT DF_Items_Availability DEFAULT 'DISPONIBLE',
    AssignedTo      NVARCHAR(150)     NOT NULL CONSTRAINT DF_Items_AssignedTo DEFAULT N'',
    LoanedAt        DATETIMEOFFSET(7) NULL,
    EquipmentID     INT               NULL CONSTRAINT FK_Items_Equipment REFERENCES dbo.Items (ID),
    RowVersion      ROWVERSION        NOT NULL,  -- versión para la concurrencia optimista

    CONSTRAINT CK_Items_Quantity        CHECK (Quantity >= 0),
    CONSTRAINT CK_Items_Status          CHECK (Status IN ('OPERATIVO', 'EN_REPARACION', 'FUERA_DE_SERVICIO', 'BAJA')),
    CONSTRAINT CK_Items_Availability    CHECK (Availability IN ('DISPONIBLE', 'PRESTADO')),
    CONSTRAINT CK_Items_InventoryNumber CHECK ((HasInventory = 1 AND InventoryNumber <> N'')
                                            OR (HasInventory = 0 AND InventoryNumber = N'')),
    CONSTRAINT CK_Items_Equipment       CHECK (EquipmentID IS NULL OR EquipmentID <> ID)
);

-- Números de inventario y de serie únicos cuando no están vacíos.
CREATE UNIQUE INDEX UX_Items_InventoryNumber ON dbo.Items (InventoryNumber) WHERE InventoryNumber <> N'';
CREATE UNIQUE INDEX UX_Items_SerialNumber    ON dbo.Items (SerialNumber)    WHERE SerialNumber <> N'';
CREATE INDEX IX_Items_EquipmentID ON dbo.Items (EquipmentID) WHERE EquipmentID IS NOT NULL;
CREATE INDEX IX_Items_Location    ON dbo.Items (Location);

-- Historial: solo crece. ItemID no tiene clave foránea a propósito: los
-- movimientos se conservan aunque el ítem se elimine.
CREATE TABLE dbo.Movements (
    ID                  INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Movements PRIMARY KEY,
    ItemID              INT               NOT NULL,
    MovementType        VARCHAR(20)       NOT NULL,
    Quantity            INT               NOT NULL,
    PreviousQuantity    INT               NOT NULL,
    NewQuantity         INT               NOT NULL,
    OriginLocation      NVARCHAR(150)     NOT NULL CONSTRAINT DF_Movements_Origin DEFAULT N'',
    DestinationLocation NVARCHAR(150)     NOT NULL CONSTRAINT DF_Movements_Destination DEFAULT N'',
    Notes               NVARCHAR(1000)    NOT NULL CONSTRAINT DF_Movements_Notes DEFAULT N'',
    CreatedAt           DATETIMEOFFSET(7) NOT NULL,

    CONSTRAINT CK_Movements_ItemID     CHECK (ItemID > 0),
    CONSTRAINT CK_Movements_Type       CHECK (MovementType IN ('stock_in', 'stock_out', 'stock_update', 'transfer', 'adjustment')),
    CONSTRAINT CK_Movements_Quantities CHECK (Quantity >= 0 AND PreviousQuantity >= 0 AND NewQuantity >= 0)
);

CREATE INDEX IX_Movements_ItemID ON dbo.Movements (ItemID, ID);

CREATE TABLE dbo.Categories (
    ID       INT IDENTITY(1, 1) NOT NULL CONSTRAINT PK_Categories PRIMARY KEY,
    Name     NVARCHAR(100)      NOT NULL CONSTRAINT UX_Categories_Name UNIQUE,
    Active   BIT                NOT NULL CONSTRAINT DF_Categories_Active DEFAULT 1,
    Loanable BIT                NOT NULL CONSTRAINT DF_Categories_Loanable DEFAULT 0
);

-- Mismas categorías iniciales que crea el libro de Excel nuevo.
INSERT INTO dbo.Categories (Name, Loanable) VALUES
    (N'PC', 0), (N'Notebook', 1), (N'Celular', 1), (N'Monitor', 1), (N'Impresora', 0),
    (N'Teclado', 1), (N'Mouse', 1), (N'Accesorio', 1), (N'Switch', 0), (N'Router', 0),
    (N'Access Point', 0), (N'UPS', 0), (N'Teléfono IP', 0), (N'Aire acondicionado', 0), (N'Otro', 1);
