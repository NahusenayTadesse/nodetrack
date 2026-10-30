// Package store persists apps, metric samples and lifecycle events in SQLite
// (WAL mode). Raw samples are rolled up into 1-minute and 1-hour tables and
// pruned, so the database stays small no matter how long it runs.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

const schema = `
CREATE TABLE IF NOT EXISTS apps (
	id          INTEGER PRIMARY KEY,
	key         TEXT NOT NULL UNIQUE,
	name        TEXT NOT NULL,
	runtime     TEXT NOT NULL,
	source      TEXT NOT NULL,
	unit        TEXT NOT NULL DEFAULT '',
	cwd         TEXT NOT NULL DEFAULT '',
	command     TEXT NOT NULL DEFAULT '',
	pkg_version TEXT NOT NULL DEFAULT '',
	ports       TEXT NOT NULL DEFAULT '[]',
	sockets     TEXT NOT NULL DEFAULT '[]',
	domains     TEXT NOT NULL DEFAULT '[]',
	root_pid    INTEGER NOT NULL DEFAULT 0,
	root_start  INTEGER NOT NULL DEFAULT 0,
	first_seen  INTEGER NOT NULL,
	last_seen   INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS samples (
	app_id INTEGER NOT NULL,
	ts     INTEGER NOT NULL,
	cpu    REAL NOT NULL,
	mem    INTEGER NOT NULL,
	PRIMARY KEY (app_id, ts)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS samples_1m (
	app_id  INTEGER NOT NULL,
	ts      INTEGER NOT NULL,
	cpu     REAL NOT NULL,
	cpu_max REAL NOT NULL,
	mem     INTEGER NOT NULL,
	mem_max INTEGER NOT NULL,
	PRIMARY KEY (app_id, ts)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS samples_1h (
	app_id  INTEGER NOT NULL,
	ts      INTEGER NOT NULL,
	cpu     REAL NOT NULL,
	cpu_max REAL NOT NULL,
	mem     INTEGER NOT NULL,
	mem_max INTEGER NOT NULL,
	PRIMARY KEY (app_id, ts)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS events (
	id     INTEGER PRIMARY KEY,
	app_id INTEGER NOT NULL,
	ts     INTEGER NOT NULL,
	kind   TEXT NOT NULL,
	detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS events_app_ts ON events (app_id, ts);
CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL);
`

// Retention per table, in seconds.
const (
	keepRaw    = 6 * 3600
	keep1m     = 8 * 86400
	keep1h     = 400 * 86400
	keepEvents = 90 * 86400
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open(driverName, dsn(path))
	if err != nil {
		return nil, err
	}
	// One connection: one page cache, and the collector is the only writer.
	// It is never closed while idle, so per-connection pragmas stick.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA journal_size_limit = 4194304; PRAGMA temp_store = MEMORY;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("pragmas: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Columns added after v0.1, applied to existing databases in place.
var addedColumns = []struct{ table, column, decl string }{
	{"apps", "exe", "TEXT NOT NULL DEFAULT ''"},
	{"apps", "pkg_dir", "TEXT NOT NULL DEFAULT ''"},
	{"apps", "entry", "TEXT NOT NULL DEFAULT ''"},
	{"apps", "uid", "INTEGER NOT NULL DEFAULT -1"},
	{"apps", "pm2_home", "TEXT NOT NULL DEFAULT ''"},
}

func migrate(db *sql.DB) error {
	for _, c := range addedColumns {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`,
			c.table, c.column).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE ` + c.table + ` ADD COLUMN ` + c.column + ` ` + c.decl); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

type App struct {
	ID         int64
	Key        string
	Name       string
	Runtime    string
	Source     string
	Unit       string
	Cwd        string
	Command    string
	PkgVersion string
	Ports      string // JSON
	Sockets    string // JSON
	Domains    string // JSON
	RootPID    int
	RootStart  int64
	FirstSeen  int64
	LastSeen   int64
	Exe        string
	PkgDir     string
	Entry      string
	UID        int
	PM2Home    string
}

const appCols = `id, key, name, runtime, source, unit, cwd, command, pkg_version,
	ports, sockets, domains, root_pid, root_start, first_seen, last_seen,
	exe, pkg_dir, entry, uid, pm2_home`

func (s *Store) Apps() ([]App, error) {
	rows, err := s.db.Query(`SELECT ` + appCols + ` FROM apps`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []App
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.Key, &a.Name, &a.Runtime, &a.Source, &a.Unit,
			&a.Cwd, &a.Command, &a.PkgVersion, &a.Ports, &a.Sockets, &a.Domains,
			&a.RootPID, &a.RootStart, &a.FirstSeen, &a.LastSeen,
			&a.Exe, &a.PkgDir, &a.Entry, &a.UID, &a.PM2Home); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) InsertApp(a *App) error {
	res, err := s.db.Exec(`INSERT INTO apps (key, name, runtime, source, unit, cwd,
		command, pkg_version, ports, sockets, domains, root_pid, root_start, first_seen, last_seen,
		exe, pkg_dir, entry, uid, pm2_home)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Key, a.Name, a.Runtime, a.Source, a.Unit, a.Cwd, a.Command, a.PkgVersion,
		a.Ports, a.Sockets, a.Domains, a.RootPID, a.RootStart, a.FirstSeen, a.LastSeen,
		a.Exe, a.PkgDir, a.Entry, a.UID, a.PM2Home)
	if err != nil {
		return err
	}
	a.ID, err = res.LastInsertId()
	return err
}

type Sample struct {
	AppID int64
	CPU   float64
	Mem   int64
}

type Event struct {
	AppID  int64  `json:"-"`
	TS     int64  `json:"ts"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// WriteTick stores one collection tick atomically: refreshed app rows,
// a sample per running app, and any lifecycle events.
func (s *Store) WriteTick(ts int64, apps []*App, samples []Sample, events []Event) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(apps) > 0 {
		st, err := tx.Prepare(`UPDATE apps SET name=?, source=?, unit=?, cwd=?, command=?,
			pkg_version=?, ports=?, sockets=?, domains=?, root_pid=?, root_start=?, last_seen=?,
			exe=?, pkg_dir=?, entry=?, uid=?, pm2_home=?
			WHERE id=?`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, a := range apps {
			if _, err := st.Exec(a.Name, a.Source, a.Unit, a.Cwd, a.Command, a.PkgVersion,
				a.Ports, a.Sockets, a.Domains, a.RootPID, a.RootStart, a.LastSeen,
				a.Exe, a.PkgDir, a.Entry, a.UID, a.PM2Home, a.ID); err != nil {
				return err
			}
		}
	}
	if len(samples) > 0 {
		st, err := tx.Prepare(`INSERT OR REPLACE INTO samples (app_id, ts, cpu, mem) VALUES (?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, sm := range samples {
			if _, err := st.Exec(sm.AppID, ts, sm.CPU, sm.Mem); err != nil {
				return err
			}
		}
	}
	for _, e := range events {
		if _, err := tx.Exec(`INSERT INTO events (app_id, ts, kind, detail) VALUES (?, ?, ?, ?)`,
			e.AppID, e.TS, e.Kind, e.Detail); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddEvent records a single event outside a collection tick (user actions).
func (s *Store) AddEvent(e Event) error {
	_, err := s.db.Exec(`INSERT INTO events (app_id, ts, kind, detail) VALUES (?, ?, ?, ?)`,
		e.AppID, e.TS, e.Kind, e.Detail)
	return err
}

// Maintain rolls completed minutes/hours up and prunes expired rows.
func (s *Store) Maintain(now int64) error {
	if err := s.rollup("rollup_1m", "samples", "samples_1m", 60, now,
		`avg(cpu), max(cpu), CAST(avg(mem) AS INTEGER), max(mem)`); err != nil {
		return err
	}
	if err := s.rollup("rollup_1h", "samples_1m", "samples_1h", 3600, now,
		`avg(cpu), max(cpu_max), CAST(avg(mem) AS INTEGER), max(mem_max)`); err != nil {
		return err
	}
	for _, q := range []struct {
		table string
		keep  int64
	}{{"samples", keepRaw}, {"samples_1m", keep1m}, {"samples_1h", keep1h}, {"events", keepEvents}} {
		if _, err := s.db.Exec(`DELETE FROM `+q.table+` WHERE ts < ?`, now-q.keep); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) rollup(mark, from, to string, step, now int64, aggs string) error {
	var since int64
	var v string
	err := s.db.QueryRow(`SELECT v FROM meta WHERE k = ?`, mark).Scan(&v)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	since, _ = strconv.ParseInt(v, 10, 64)
	until := now / step * step // only buckets that are complete
	if until <= since {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(`INSERT OR REPLACE INTO %s (app_id, ts, cpu, cpu_max, mem, mem_max)
		SELECT app_id, ts / %d * %d, %s FROM %s WHERE ts >= ? AND ts < ?
		GROUP BY app_id, ts / %d`, to, step, step, aggs, from, step), since, until); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO meta (k, v) VALUES (?, ?)`, mark, strconv.FormatInt(until, 10)); err != nil {
		return err
	}
	return tx.Commit()
}

// Restarts counts start/restart events per app since ts.
func (s *Store) Restarts(since int64) (map[int64]int, error) {
	rows, err := s.db.Query(`SELECT app_id, count(*) FROM events
		WHERE ts >= ? AND kind IN ('start', 'restart') GROUP BY app_id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (s *Store) Events(appID int64, limit int) ([]Event, error) {
	rows, err := s.db.Query(`SELECT ts, kind, detail FROM events WHERE app_id = ?
		ORDER BY ts DESC, id DESC LIMIT ?`, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.TS, &e.Kind, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type History struct {
	Range  string    `json:"range"`
	Step   int64     `json:"step"`
	TS     []int64   `json:"ts"`
	CPU    []float64 `json:"cpu"`
	CPUMax []float64 `json:"cpuMax"`
	Mem    []int64   `json:"mem"`
	MemMax []int64   `json:"memMax"`
}

var ranges = map[string]struct {
	span  int64
	table string
	step  int64
}{
	"1h":  {3600, "samples", 0},
	"6h":  {6 * 3600, "samples_1m", 60},
	"24h": {86400, "samples_1m", 60},
	"7d":  {7 * 86400, "samples_1h", 3600},
	"30d": {30 * 86400, "samples_1h", 3600},
}

var ErrBadRange = errors.New("unknown range")

func (s *Store) History(appID int64, rng string, now, rawStep int64) (*History, error) {
	r, ok := ranges[rng]
	if !ok {
		return nil, ErrBadRange
	}
	h := &History{Range: rng, Step: r.step,
		TS: []int64{}, CPU: []float64{}, CPUMax: []float64{}, Mem: []int64{}, MemMax: []int64{}}
	q := `SELECT ts, cpu, cpu_max, mem, mem_max FROM ` + r.table + ` WHERE app_id = ? AND ts >= ? ORDER BY ts`
	if r.table == "samples" {
		h.Step = rawStep
		q = `SELECT ts, cpu, cpu, mem, mem FROM samples WHERE app_id = ? AND ts >= ? ORDER BY ts`
	}
	rows, err := s.db.Query(q, appID, now-r.span)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ts, mem, memMax int64
		var cpu, cpuMax float64
		if err := rows.Scan(&ts, &cpu, &cpuMax, &mem, &memMax); err != nil {
			return nil, err
		}
		h.TS = append(h.TS, ts)
		h.CPU = append(h.CPU, cpu)
		h.CPUMax = append(h.CPUMax, cpuMax)
		h.Mem = append(h.Mem, mem)
		h.MemMax = append(h.MemMax, memMax)
	}
	return h, rows.Err()
}

func (s *Store) DeleteApp(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"samples", "samples_1m", "samples_1h", "events"} {
		if _, err := tx.Exec(`DELETE FROM `+t+` WHERE app_id = ?`, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM apps WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
