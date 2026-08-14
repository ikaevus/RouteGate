DROP INDEX IF EXISTS idx_servers_location_country;
ALTER TABLE servers
    DROP CONSTRAINT IF EXISTS servers_location_coordinates_pair_check,
    DROP CONSTRAINT IF EXISTS servers_location_source_check,
    DROP CONSTRAINT IF EXISTS servers_location_longitude_check,
    DROP CONSTRAINT IF EXISTS servers_location_latitude_check,
    DROP COLUMN IF EXISTS location_source,
    DROP COLUMN IF EXISTS location_longitude,
    DROP COLUMN IF EXISTS location_latitude,
    DROP COLUMN IF EXISTS location_city,
    DROP COLUMN IF EXISTS location_region,
    DROP COLUMN IF EXISTS location_country;
