// Package db embeds the SQL migrations so a single binary can bring an empty
// database up to date without shipping the .sql files alongside it.
package db

import "embed"

// Migrations holds every goose migration, applied by cmd/api on boot.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// MigrationsDir is the path of the migrations inside Migrations.
const MigrationsDir = "migrations"
