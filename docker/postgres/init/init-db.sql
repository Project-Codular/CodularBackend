-- Log start of initialization
\echo 'Starting database initialization...'

-- Create the admin_codular role if it doesn't exist
DO
$do$
BEGIN
    IF NOT EXISTS (
        SELECT FROM pg_roles WHERE rolname = 'admin_codular'
    ) THEN
        CREATE ROLE admin_codular WITH LOGIN PASSWORD 'superAdmin';
    END IF;
END
$do$;

-- Create the database if it doesn't exist
DO
$do$
BEGIN
    IF NOT EXISTS (
        SELECT FROM pg_database WHERE datname = 'project_codular_db'
    ) THEN
        CREATE DATABASE project_codular_db;
    END IF;
END
$do$;

-- Grant privileges to admin_codular on project_codular_db
GRANT ALL PRIVILEGES ON DATABASE project_codular_db TO admin_codular;

-- Connect to project_codular_db
\c project_codular_db postgres

-- Grant schema privileges to admin_codular
GRANT USAGE, CREATE ON SCHEMA public TO admin_codular;

-- Create the skips table if it doesn't exist
CREATE TABLE IF NOT EXISTS skips (
                                     id SERIAL PRIMARY KEY,
                                     user_code TEXT NOT NULL,
                                     skips_number INTEGER NOT NULL,
                                     code_alias TEXT NOT NULL,
                                     created_at TIMESTAMP NOT NULL
);
\echo 'Created tables';

-- Log completion
\echo 'Database initialization completed.'