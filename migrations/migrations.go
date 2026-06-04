// Package migrations embeds the SQL schema files so the binary can apply them
// without shipping the .sql files alongside it.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
