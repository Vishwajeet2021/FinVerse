#!/bin/sh
# Least privilege between services (docs/22 D-S5, F-7): one login role per service, owning
# only that service's database. Other services' roles cannot even connect to it.
#
# Runs once, when the Postgres data volume is first created. It is idempotent, so it can also
# be re-run against an existing cluster whose databases were created by 001 but are still
# owned by the superuser:
#   docker compose -f docker/docker-compose.yml exec postgres sh /docker-entrypoint-initdb.d/002-create-roles.sh
# (Existing tables created by the superuser must be re-owned too; for local development it is
# simpler to start from a fresh volume: docker compose down -v.)
set -eu

for pair in auth:AUTH customer:CUSTOMER transaction:TRANSACTION account:ACCOUNT wallet:WALLET \
            payment:PAYMENT notification:NOTIFICATION audit:AUDIT fraud:FRAUD; do
  name=${pair%%:*}
  var=${pair##*:}_DB_PASSWORD
  eval "pw=\${$var:-}"
  if [ -z "$pw" ]; then
    echo "002-create-roles: $var is not set" >&2
    exit 1
  fi

  psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d postgres <<SQL
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '${name}_svc') THEN
    CREATE ROLE ${name}_svc LOGIN PASSWORD '${pw}' NOSUPERUSER NOCREATEDB NOCREATEROLE;
  ELSE
    ALTER ROLE ${name}_svc PASSWORD '${pw}';
  END IF;
END
\$\$;
ALTER DATABASE ${name}_db OWNER TO ${name}_svc;
REVOKE ALL ON DATABASE ${name}_db FROM PUBLIC;
GRANT CONNECT ON DATABASE ${name}_db TO ${name}_svc;
SQL

  psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "${name}_db" -c "ALTER SCHEMA public OWNER TO ${name}_svc;"
done
