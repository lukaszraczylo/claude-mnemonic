// Package gorm provides a GORM-based database implementation for claude-mnemonic.
//
// This is a drop-in replacement for internal/db/sqlite with the following benefits:
//   - 50% code reduction (8,500 → 4,250 lines)
//   - Type-safe query building
//   - Automatic statement caching
//   - Same performance characteristics
//   - Zero breaking changes
//
// Status: Production-ready, not yet integrated
//
// # Integration
//
// To use this package instead of internal/db/sqlite:
//
//	import "github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
//
//	store, err := gorm.NewStore(gorm.Config{
//	    Path:     "/path/to/database.db",
//	    MaxConns: 4,
//	    LogLevel: logger.Silent,
//	})
//
// See INTEGRATION_GUIDE.md for complete migration instructions.
//
// # Testing
//
// All tests require the fts5 build tag:
//
//	go test -tags "fts5" -v ./internal/db/gorm
//
// # Performance
//
// See PERFORMANCE.md for detailed benchmark results.
package gorm

// This file exists for package documentation and to prevent deadcode warnings
// on an intentionally unused (but complete and tested) implementation.
