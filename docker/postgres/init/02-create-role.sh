#!/bin/bash
set -e

# Wait for PostgreSQL to be ready
until pg_isready -U "$POSTGRES_USER" -d postgres; do
  echo "Waiting for PostgreSQL to be ready..."
  sleep 2
done

# Create the admin_codular role if it doesn't exist
psql -U "$POSTGRES_USER" -d postgres -c "
DO
\$\$
BEGIN
    IF NOT EXISTS (
        SELECT FROM pg_roles WHERE rolname = '$POSTGRES_ADMIN_USER'
    ) THEN
        CREATE ROLE \"$POSTGRES_ADMIN_USER\" WITH LOGIN PASSWORD '$POSTGRES_ADMIN_PASSWORD';
    END IF;
END
\$\$;
"

# Grant privileges to admin_codular on project_codular_db
psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "
GRANT ALL PRIVILEGES ON DATABASE \"$POSTGRES_DB\" TO \"$POSTGRES_ADMIN_USER\";
GRANT USAGE, CREATE ON SCHEMA public TO \"$POSTGRES_ADMIN_USER\";
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO \"$POSTGRES_ADMIN_USER\";
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO \"$POSTGRES_ADMIN_USER\";
"

echo "Role '$POSTGRES_ADMIN_USER' created and privileges granted."