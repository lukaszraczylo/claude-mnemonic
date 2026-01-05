//go:build !sqlite_omit_load_extension
// +build !sqlite_omit_load_extension

// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

// This file ensures mattn/go-sqlite3 is built with FTS5 and other extensions enabled.
// The build tag ensures extensions are not omitted.
