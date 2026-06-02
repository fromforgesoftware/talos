-- Bootstrap that runs before the versioned migrations (and before
-- golang-migrate creates its schema_migrations tracking table), so a
-- fresh database has the uuid-ossp extension and the talos schema in
-- place. Idempotent — re-runs harmlessly on every migrate.
CREATE EXTENSION IF NOT EXISTS "uuid-ossp" SCHEMA public;
CREATE SCHEMA IF NOT EXISTS talos;
