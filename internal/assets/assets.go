package assets

import "embed"

// Files contains the configuration, data fixtures, schemas, and SQLite migrations.
//
//go:embed config fixtures migrations schemas.json
var Files embed.FS
