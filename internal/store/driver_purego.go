//go:build !cgo_sqlite

package store

// Default driver: pure Go, so the binary cross-compiles with CGO_ENABLED=0.
import _ "modernc.org/sqlite"

const driverName = "sqlite"

func dsn(path string) string {
	return path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=cache_size(-1024)" + // 1 MiB page cache
		"&_txlock=immediate"
}
