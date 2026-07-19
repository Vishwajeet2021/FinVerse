// Package migrations embeds Auth Service's SQL migration files into the
// compiled binary so the container image doesn't need a separate COPY step
// for them and the migration runner (internal/migrate) always ships in sync
// with the code that depends on the schema, per docs/08-database-design.md §15.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
