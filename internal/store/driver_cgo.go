//go:build cgo_sqlite

package store

// C SQLite via cgo: a much smaller binary and resident footprint, but it
// needs a C toolchain and links against the build host's glibc.
import _ "github.com/mattn/go-sqlite3"

const driverName = "sqlite3"

func dsn(path string) string {
	return path +
		"?_journal_mode=WAL" +
		"&_synchronous=NORMAL" +
		"&_busy_timeout=5000" +
		"&_cache_size=-1024" + // 1 MiB page cache
		"&_txlock=immediate"
}
