ALTER TABLE servers
    ADD COLUMN location_country TEXT,
    ADD COLUMN location_region TEXT,
    ADD COLUMN location_city TEXT,
    ADD COLUMN location_latitude DOUBLE PRECISION,
    ADD COLUMN location_longitude DOUBLE PRECISION,
    ADD COLUMN location_source TEXT;

ALTER TABLE servers
    ADD CONSTRAINT servers_location_latitude_check CHECK (
        location_latitude IS NULL OR (location_latitude >= -90 AND location_latitude <= 90)
    ),
    ADD CONSTRAINT servers_location_longitude_check CHECK (
        location_longitude IS NULL OR (location_longitude >= -180 AND location_longitude <= 180)
    ),
    ADD CONSTRAINT servers_location_source_check CHECK (
        location_source IS NULL OR location_source IN ('manual','auto_detected')
    ),
    ADD CONSTRAINT servers_location_coordinates_pair_check CHECK (
        (location_latitude IS NULL AND location_longitude IS NULL)
        OR (location_latitude IS NOT NULL AND location_longitude IS NOT NULL)
    );

CREATE INDEX idx_servers_location_country
    ON servers (location_country)
    WHERE location_country IS NOT NULL;
