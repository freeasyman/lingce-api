-- Baseline marker for the versioned schema migration system.
--
-- Historical schema compatibility has been handled by the legacy compatibility
-- migration path. New schema changes must be added as subsequent migration
-- files in this directory.
SELECT 1;
