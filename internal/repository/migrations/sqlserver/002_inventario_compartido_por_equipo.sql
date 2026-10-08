-- Un componente puede compartir el N° de inventario de su gabinete (regla
-- models.ShareEquipment): el número es único entre equipos, no por fila. Esa
-- excepción no se puede expresar con un índice UNIQUE, así que la verifica la
-- aplicación (services.checkUniqueness y la importación), igual que con Excel.
-- El N° de serie sigue siendo único en la base (UX_Items_SerialNumber).
DROP INDEX UX_Items_InventoryNumber ON dbo.Items;
CREATE INDEX IX_Items_InventoryNumber ON dbo.Items (InventoryNumber) WHERE InventoryNumber <> N'';
