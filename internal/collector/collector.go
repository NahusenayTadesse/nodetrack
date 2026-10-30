// Package collector discovers node/bun apps from /proc, samples their
// resource usage, tracks their lifecycle and publishes snapshots.
package collector

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"nodetrack/internal/procfs"
	"nodetrack/internal/proxy"
	"nodetrack/internal/store"
)

const (
	Version  = "0.1.0"
	sparkLen = 60
)

type Config struct {
	Interval time.Duration  // sampling period
	MinAge   time.Duration  // a process must live this long before it is adopted
	PortScan time.Duration  // full socket rescan period (also done on new processes)
	Include  *regexp.Regexp // adopt matching processes even without ports
	Exclude  *regexp.Regexp // never adopt matching processes
}

// proc is a cached node/bun (or other) process. Classification happens once
// per process; only stat-derived counters are refreshed on every tick.
type proc struct {
	pid   int
	start uint64 // starttime in ticks; with pid it identifies the process
	comm  string

	candidate  bool
	runtime    string
	argv       []string
	script     string
	cwd        string
	unit       string
	pm2Name    string
	envPort    int
	affinity   string
	pkgName    string
	pkgVersion string
	pkgDir     string

	key, name, source string

	stat      procfs.Stat
	seen      uint64
	prevCPU   uint64
	hasPrev   bool
	cpuPct    float64
	prevRead  uint64
	prevWrite uint64
	hasIO     bool
	readBps   float64
	writeBps  float64
	pss       int64
	fds       int
	listeners []procfs.Listener
	sockets   []string
}

type app struct {
	row       store.App
	up        bool
	restarts  int
	spark     ring
	summary   AppSummary
	processes []Process
	gitDir    string
}

type ring struct {
	cpu  [sparkLen]float64
	mem  [sparkLen]int64
	n, i int
}

func (r *ring) push(c float64, m int64) {
	r.cpu[r.i], r.mem[r.i] = c, m
	r.i = (r.i + 1) % sparkLen
	r.n = min(r.n+1, sparkLen)
}

func (r *ring) slices() ([]float64, []int64) {
	cpu, mem := make([]float64, 0, r.n), make([]int64, 0, r.n)
	for k := range r.n {
		j := (r.i - r.n + k + sparkLen) % sparkLen
		cpu, mem = append(cpu, r.cpu[j]), append(mem, r.mem[j])
	}
	return cpu, mem
}

type forgetReq struct {
	id    int64
	reply chan error
}

var (
	ErrNotFound = errors.New("app not found")
	ErrRunning  = errors.New("app is running")
)

type Collector struct {
	cfg   Config
	store *store.Store
	proxy *proxy.Resolver
	r     *procfs.Reader

	selfNS   string
	bootTime int64
	hostname string

	procs        map[int]*proc
	ppid         map[int]int
	pids         []int
	tcp          map[uint64]procfs.Listener
	unix         map[uint64]string
	tick         uint64
	lastTick     time.Time
	lastPortScan time.Time
	prevHost     procfs.CPUTimes
	host         Host

	apps   map[string]*app // by key; owned by the Run goroutine
	forget chan forgetReq

	mu       sync.RWMutex
	overview *Overview
	details  map[int64][]Process
	gitDirs  map[int64]string
	subs     map[chan []byte]struct{}
}

func New(cfg Config, st *store.Store, px *proxy.Resolver) (*Collector, error) {
	c := &Collector{
		cfg: cfg, store: st, proxy: px, r: procfs.NewReader(),
		selfNS: procfs.SelfPIDNamespace(),
		procs:  map[int]*proc{}, ppid: map[int]int{},
		tcp: map[uint64]procfs.Listener{}, unix: map[uint64]string{},
		apps: map[string]*app{}, forget: make(chan forgetReq),
		subs: map[chan []byte]struct{}{},
	}
	var err error
	if c.bootTime, err = c.r.BootTime(); err != nil {
		return nil, fmt.Errorf("boot time: %w", err)
	}
	c.hostname, _ = os.Hostname()
	rows, err := st.Apps()
	if err != nil {
		return nil, err
	}
	restarts, _ := st.Restarts(time.Now().Unix() - 86400)
	for _, row := range rows {
		a := &app{row: row, restarts: restarts[row.ID]}
		a.summary = downSummary(a)
		c.apps[row.Key] = a
	}
	return c, nil
}

func (c *Collector) Run(ctx context.Context) {
	tick := time.NewTicker(c.cfg.Interval)
	defer tick.Stop()
	maint := time.NewTicker(time.Minute)
	defer maint.Stop()
	c.collect(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			c.collect(now)
		case now := <-maint.C:
			if err := c.store.Maintain(now.Unix()); err != nil {
				slog.Error("maintain", "err", err)
			}
			if r, err := c.store.Restarts(now.Unix() - 86400); err == nil {
				for _, a := range c.apps {
					a.restarts = r[a.row.ID]
				}
			}
		case req := <-c.forget:
			req.reply <- c.doForget(req.id)
		}
	}
}

func (c *Collector) startUnix(p *proc) int64 {
	return c.bootTime + int64(p.start/procfs.ClockTicks)
}

// inspect classifies a process seen for the first time (or after exec).
func (c *Collector) inspect(pid int, st procfs.Stat) *proc {
	p := &proc{pid: pid, start: st.StartTime, comm: st.Comm}
	exe, err := procfs.Exe(pid)
	if err != nil {
		return p // kernel thread, or not ours to read
	}
	if p.runtime = runtimeOf(exe); p.runtime == "" {
		return p
	}
	if ns, err := procfs.PIDNamespace(pid); err == nil && c.selfNS != "" && ns != c.selfNS {
		return p // containerised
	}
	cg, _ := c.r.Cgroup(pid)
	if inContainerCgroup(cg) {
		return p
	}
	argv, _ := c.r.Cmdline(pid)
	cwd, _ := procfs.Cwd(pid)
	ok, script, _ := classify(p.runtime, argv, cwd)
	if !ok {
		return p
	}
	p.candidate = true
	p.argv, p.script, p.cwd = argv, script, cwd
	p.unit = systemdUnit(cg)
	if env, err := c.r.Environ(pid, "pm_id", "name", "PORT"); err == nil {
		if _, ok := env["pm_id"]; ok {
			p.pm2Name = env["name"]
		}
		p.envPort, _ = strconv.Atoi(env["PORT"])
	}
	p.affinity = c.r.CPUAffinity(pid)
	p.pkgName, p.pkgVersion, p.pkgDir = readPackage(cwd, script)
	p.identify()
	return p
}

func (p *proc) identify() {
	switch {
	case p.pm2Name != "":
		p.key, p.source, p.name = "pm2:"+p.pm2Name, "pm2", p.pm2Name
	case p.unit != "":
		p.key, p.source, p.name = "systemd:"+p.unit, "systemd", strings.TrimSuffix(p.unit, ".service")
	default:
		entry := p.script
		if entry == "" {
			entry = strings.Join(p.argv, " ")
		}
		p.key, p.source = "proc:"+p.runtime+":"+p.cwd+":"+entry, "manual"
		p.name = p.pkgName
		if p.name == "" && p.cwd != "/" && p.cwd != "" {
			p.name = filepath.Base(p.cwd)
		}
		if p.name == "" {
			p.name = filepath.Base(entry)
		}
	}
}

func (p *proc) command() string {
	if len(p.argv) == 0 {
		return ""
	}
	args := slices.Clone(p.argv)
	if strings.ContainsRune(args[0], '/') && runtimeOf(args[0]) != "" {
		args[0] = filepath.Base(args[0])
	}
	s := strings.Join(args, " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func (c *Collector) excluded(p *proc) bool {
	return c.cfg.Exclude != nil && c.cfg.Exclude.MatchString(p.cwd+" "+strings.Join(p.argv, " "))
}

func (c *Collector) included(p *proc) bool {
	return c.cfg.Include != nil && c.cfg.Include.MatchString(p.cwd+" "+strings.Join(p.argv, " "))
}

// rootOf walks up the parent chain and returns the top-most candidate
// ancestor, so cluster workers and spawned children fold into their app.
func (c *Collector) rootOf(p *proc) *proc {
	root := p
	cur := c.ppid[p.pid]
	for depth := 0; cur > 1 && depth < 64; depth++ {
		if q := c.procs[cur]; q != nil && q.candidate && q.seen == c.tick {
			root = q
		}
		cur = c.ppid[cur]
	}
	return root
}

type group struct {
	root      *proc
	procs     []*proc
	listeners []procfs.Listener
	sockets   []string
}

func (c *Collector) collect(now time.Time) {
	c.tick++
	elapsed := 0.0
	if !c.lastTick.IsZero() {
		elapsed = now.Sub(c.lastTick).Seconds()
	}
	c.lastTick = now
	nowUnix := now.Unix()

	// 1. Enumerate processes; classify only the ones we haven't seen.
	var err error
	if c.pids, err = procfs.PIDs(c.pids[:0]); err != nil {
		slog.Error("list /proc", "err", err)
		return
	}
	clear(c.ppid)
	newCandidate := false
	for _, pid := range c.pids {
		st, err := c.r.Stat(pid)
		if err != nil {
			continue
		}
		c.ppid[pid] = st.PPID
		p := c.procs[pid]
		if p == nil || p.start != st.StartTime || p.comm != st.Comm {
			np := c.inspect(pid, st)
			if p != nil && p.start == st.StartTime && p.candidate && np.candidate {
				// Same process, it only renamed itself: keep its counters.
				np.prevCPU, np.hasPrev = p.prevCPU, p.hasPrev
				np.prevRead, np.prevWrite, np.hasIO = p.prevRead, p.prevWrite, p.hasIO
				np.listeners, np.sockets = p.listeners, p.sockets
			} else if np.candidate {
				newCandidate = true
			}
			c.procs[pid] = np
			p = np
		}
		p.stat = st
		p.seen = c.tick
	}
	for pid, p := range c.procs {
		if p.seen != c.tick {
			delete(c.procs, pid)
		}
	}

	// 2. Sockets: full rescan when something new appeared, else periodically.
	scan := newCandidate || now.Sub(c.lastPortScan) >= c.cfg.PortScan
	if scan {
		c.lastPortScan = now
		if err := c.r.TCPListeners(c.tcp); err != nil {
			slog.Warn("read tcp listeners", "err", err)
		}
		c.r.UnixListeners(c.unix)
	}

	// 3. CPU for every candidate (cheap, already in stat) and socket owners.
	groups := map[string]*group{}
	for _, p := range c.procs {
		if !p.candidate {
			continue
		}
		cpu := p.stat.UTime + p.stat.STime
		p.cpuPct = 0
		if p.hasPrev && elapsed > 0 && cpu >= p.prevCPU {
			p.cpuPct = float64(cpu-p.prevCPU) / procfs.ClockTicks / elapsed * 100
		}
		p.prevCPU, p.hasPrev = cpu, true
		if scan {
			p.listeners, p.sockets = p.listeners[:0], p.sockets[:0]
			p.fds, _ = procfs.FDs(p.pid, true, func(target string) {
				ino, ok := procfs.SocketInode(target)
				if !ok {
					return
				}
				if l, ok := c.tcp[ino]; ok && !slices.Contains(p.listeners, l) {
					p.listeners = append(p.listeners, l)
				} else if path, ok := c.unix[ino]; ok && !slices.Contains(p.sockets, path) {
					p.sockets = append(p.sockets, path)
				}
			})
		}
	}
	for _, p := range c.procs {
		if !p.candidate {
			continue
		}
		root := c.rootOf(p)
		g := groups[root.key]
		if g == nil {
			g = &group{root: root}
			groups[root.key] = g
		} else if root.start < g.root.start {
			g.root = root
		}
		g.procs = append(g.procs, p)
		for _, l := range p.listeners {
			if !slices.Contains(g.listeners, l) {
				g.listeners = append(g.listeners, l)
			}
		}
		for _, s := range p.sockets {
			if !slices.Contains(g.sockets, s) {
				g.sockets = append(g.sockets, s)
			}
		}
	}

	// 4. Decide which groups are apps.
	for key, g := range groups {
		root := g.root
		if c.excluded(root) {
			delete(groups, key)
			continue
		}
		// pm2 cluster mode: the listening socket lives in the pm2 daemon,
		// so fall back to the PORT the app was given.
		if len(g.listeners) == 0 && root.envPort > 0 {
			for _, l := range c.tcp {
				if l.Port == root.envPort && !slices.Contains(g.listeners, l) {
					g.listeners = append(g.listeners, l)
				}
			}
		}
		if c.apps[key] != nil {
			continue
		}
		// Unix sockets are mostly private IPC; they only make a process an
		// app when a reverse proxy routes traffic to them.
		eligible := len(g.listeners) > 0 || root.source != "manual" || c.included(root) ||
			(len(g.sockets) > 0 && len(c.proxy.Match(nil, g.sockets)) > 0)
		if !eligible || nowUnix-c.startUnix(root) < int64(c.cfg.MinAge.Seconds()) {
			delete(groups, key)
		}
	}

	// 5. Detailed metrics and lifecycle for apps only.
	var (
		events  []store.Event
		samples []store.Sample
		updated []*store.App
	)
	for key, g := range groups {
		a := c.apps[key]
		root := g.root
		rootStart := c.startUnix(root)
		switch {
		case a == nil:
			a = &app{row: store.App{Key: key, Runtime: root.runtime, FirstSeen: nowUnix}}
			c.fillRow(a, g, rootStart, nowUnix)
			if err := c.store.InsertApp(&a.row); err != nil {
				slog.Error("insert app", "key", key, "err", err)
				continue
			}
			c.apps[key] = a
			slog.Info("discovered app", "name", a.row.Name, "pid", root.pid)
			events = append(events, store.Event{AppID: a.row.ID, TS: nowUnix, Kind: "discovered",
				Detail: fmt.Sprintf("pid %d", root.pid)})
		case a.row.RootPID != root.pid || a.row.RootStart != rootStart:
			kind, detail := "restart", fmt.Sprintf("pid %d → %d", a.row.RootPID, root.pid)
			if !a.up {
				kind, detail = "start", fmt.Sprintf("pid %d", root.pid)
			}
			a.restarts++
			events = append(events, store.Event{AppID: a.row.ID, TS: nowUnix, Kind: kind, Detail: detail})
		}
		a.up = true
		c.fillRow(a, g, rootStart, nowUnix)
		c.measure(a, g, scan, elapsed)
		samples = append(samples, store.Sample{AppID: a.row.ID, CPU: a.summary.CPU, Mem: memOf(&a.summary)})
		updated = append(updated, &a.row)
	}
	for key, a := range c.apps {
		if a.up && groups[key] == nil {
			a.up = false
			a.processes = nil
			a.summary = downSummary(a)
			events = append(events, store.Event{AppID: a.row.ID, TS: nowUnix, Kind: "stop",
				Detail: fmt.Sprintf("pid %d", a.row.RootPID)})
			slog.Info("app stopped", "name", a.row.Name)
		}
	}
	if err := c.store.WriteTick(nowUnix, updated, samples, events); err != nil {
		slog.Error("write tick", "err", err)
	}

	c.sampleHost(elapsed)
	c.publish()
}

func (c *Collector) fillRow(a *app, g *group, rootStart, now int64) {
	root := g.root
	r := &a.row
	r.Name, r.Source, r.Cwd, r.Command, r.PkgVersion = root.name, root.source, root.cwd, root.command(), root.pkgVersion
	r.Unit = root.pm2Name
	if r.Unit == "" {
		r.Unit = root.unit
	}
	r.RootPID, r.RootStart, r.LastSeen = root.pid, rootStart, now
	slices.SortFunc(g.listeners, func(x, y procfs.Listener) int {
		return cmp.Or(cmp.Compare(x.Port, y.Port), strings.Compare(x.Addr, y.Addr))
	})
	slices.Sort(g.sockets)
	r.Ports = mustJSON(g.listeners)
	r.Sockets = mustJSON(g.sockets)
	r.Domains = mustJSON(c.proxy.Match(g.listeners, g.sockets))
	a.gitDir = cmp.Or(root.pkgDir, root.cwd)
}

func (c *Collector) measure(a *app, g *group, scanned bool, elapsed float64) {
	s := AppSummary{
		ID: a.row.ID, Name: a.row.Name, Runtime: a.row.Runtime, Status: "up",
		Source: a.row.Source, Unit: a.row.Unit, Cwd: a.row.Cwd, Command: a.row.Command,
		PkgVersion: a.row.PkgVersion, Affinity: g.root.affinity,
		Ports: nonNil(g.listeners), Sockets: nonNil(g.sockets),
		Domains:   c.proxy.Match(g.listeners, g.sockets),
		StartedAt: a.row.RootStart, Restarts: a.restarts,
		FirstSeen: a.row.FirstSeen, LastSeen: a.row.LastSeen,
	}
	if s.Domains == nil {
		s.Domains = []proxy.Domain{}
	}
	procs := make([]Process, 0, len(g.procs))
	slices.SortFunc(g.procs, func(x, y *proc) int { return cmp.Compare(x.start, y.start) })
	for _, p := range g.procs {
		if !scanned {
			p.fds, _ = procfs.FDs(p.pid, false, nil)
		}
		p.pss, _ = c.r.PSS(p.pid)
		if rd, wr, err := c.r.IO(p.pid); err == nil {
			p.readBps, p.writeBps = 0, 0
			if p.hasIO && elapsed > 0 && rd >= p.prevRead && wr >= p.prevWrite {
				p.readBps = float64(rd-p.prevRead) / elapsed
				p.writeBps = float64(wr-p.prevWrite) / elapsed
			}
			p.prevRead, p.prevWrite, p.hasIO = rd, wr, true
		}
		s.PIDs = append(s.PIDs, p.pid)
		s.CPU += p.cpuPct
		s.RSS += p.stat.RSS
		s.PSS += p.pss
		s.Threads += p.stat.NumThreads
		s.FDs += p.fds
		s.ReadBps += p.readBps
		s.WriteBps += p.writeBps
		if !slices.Contains(s.Cores, p.stat.Processor) {
			s.Cores = append(s.Cores, p.stat.Processor)
		}
		procs = append(procs, Process{
			PID: p.pid, PPID: p.stat.PPID, Cmd: p.command(), CPU: p.cpuPct,
			RSS: p.stat.RSS, Threads: p.stat.NumThreads, Core: p.stat.Processor,
			StartedAt: c.startUnix(p),
		})
	}
	slices.Sort(s.Cores)
	s.Procs = len(s.PIDs)
	a.spark.push(s.CPU, memOf(&s))
	s.SparkCPU, s.SparkMem = a.spark.slices()
	a.summary, a.processes = s, procs
}

func memOf(s *AppSummary) int64 {
	if s.PSS > 0 {
		return s.PSS
	}
	return s.RSS
}

func downSummary(a *app) AppSummary {
	s := AppSummary{
		ID: a.row.ID, Name: a.row.Name, Runtime: a.row.Runtime, Status: "down",
		Source: a.row.Source, Unit: a.row.Unit, Cwd: a.row.Cwd, Command: a.row.Command,
		PkgVersion: a.row.PkgVersion, PIDs: []int{}, Cores: []int{},
		Restarts: a.restarts, FirstSeen: a.row.FirstSeen, LastSeen: a.row.LastSeen,
		SparkCPU: []float64{}, SparkMem: []int64{},
	}
	json.Unmarshal([]byte(a.row.Ports), &s.Ports)
	json.Unmarshal([]byte(a.row.Sockets), &s.Sockets)
	json.Unmarshal([]byte(a.row.Domains), &s.Domains)
	s.Ports, s.Sockets, s.Domains = nonNil(s.Ports), nonNil(s.Sockets), nonNil(s.Domains)
	return s
}

func (c *Collector) sampleHost(elapsed float64) {
	h := &c.host
	h.Hostname = c.hostname
	if t, cpus, err := c.r.HostCPU(); err == nil {
		h.CPUs = cpus
		if dt := t.Total - c.prevHost.Total; c.prevHost.Total > 0 && dt > 0 {
			h.CPUPct = float64(dt-(t.Idle-c.prevHost.Idle)) / float64(dt) * 100
		}
		c.prevHost = t
	}
	h.MemTotal, h.MemAvailable, _ = c.r.MemInfo()
	h.Load, _ = c.r.LoadAvg()
	h.Uptime, _ = c.r.Uptime()
}

func (c *Collector) publish() {
	ov := &Overview{
		Host:    c.host,
		Tracker: Tracker{RSS: c.r.SelfRSS(), Interval: int(c.cfg.Interval.Seconds()), Version: Version},
		Apps:    make([]AppSummary, 0, len(c.apps)),
	}
	details := make(map[int64][]Process, len(c.apps))
	gitDirs := make(map[int64]string, len(c.apps))
	for _, a := range c.apps {
		a.summary.Restarts = a.restarts
		ov.Apps = append(ov.Apps, a.summary)
		details[a.row.ID] = a.processes
		gitDirs[a.row.ID] = cmp.Or(a.gitDir, a.row.Cwd)
	}
	slices.SortFunc(ov.Apps, func(x, y AppSummary) int {
		if x.Status != y.Status {
			return strings.Compare(y.Status, x.Status) // "up" before "down"
		}
		return cmp.Or(strings.Compare(x.Name, y.Name), cmp.Compare(x.ID, y.ID))
	})

	c.mu.Lock()
	c.overview, c.details, c.gitDirs = ov, details, gitDirs
	var msg []byte
	if len(c.subs) > 0 {
		msg, _ = json.Marshal(ov)
	}
	for ch := range c.subs {
		select {
		case ch <- msg:
		default: // slow client: it gets the next tick
		}
	}
	c.mu.Unlock()
}

// Overview returns the latest snapshot. It is immutable; don't modify it.
func (c *Collector) Overview() *Overview {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.overview
}

func (c *Collector) Detail(id int64) (*AppDetail, error) {
	c.mu.RLock()
	var d *AppDetail
	if c.overview != nil {
		for _, s := range c.overview.Apps {
			if s.ID == id {
				d = &AppDetail{AppSummary: s, Processes: c.details[id]}
			}
		}
	}
	gitDir := c.gitDirs[id]
	c.mu.RUnlock()
	if d == nil {
		return nil, ErrNotFound
	}
	if d.Processes == nil {
		d.Processes = []Process{}
	}
	var err error
	if d.Events, err = c.store.Events(id, 50); err != nil {
		return nil, err
	}
	d.Git = gitInfo(gitDir)
	return d, nil
}

func (c *Collector) History(id int64, rng string) (*store.History, error) {
	return c.store.History(id, rng, time.Now().Unix(), int64(c.cfg.Interval.Seconds()))
}

// Forget removes a stopped app and its history.
func (c *Collector) Forget(ctx context.Context, id int64) error {
	req := forgetReq{id: id, reply: make(chan error, 1)}
	select {
	case c.forget <- req:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Collector) doForget(id int64) error {
	for key, a := range c.apps {
		if a.row.ID != id {
			continue
		}
		if a.up {
			return ErrRunning
		}
		if err := c.store.DeleteApp(id); err != nil {
			return err
		}
		delete(c.apps, key)
		c.publish()
		return nil
	}
	return ErrNotFound
}

// Subscribe registers for a JSON overview per tick.
func (c *Collector) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 1)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		delete(c.subs, ch)
		c.mu.Unlock()
	}
}

func readPackage(cwd, script string) (name, version, dir string) {
	dir = cwd
	if script != "" && looksLikeFile(script, cwd) {
		s := script
		if !filepath.IsAbs(s) {
			s = filepath.Join(cwd, s)
		}
		dir = filepath.Dir(s)
	}
	if i := strings.Index(dir, "/node_modules"); i >= 0 {
		dir = dir[:i]
	}
	for range 5 {
		b, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			var pkg struct{ Name, Version string }
			json.Unmarshal(b, &pkg)
			return pkg.Name, pkg.Version, dir
		}
		if dir == "/" || dir == "." || dir == "" {
			break
		}
		dir = filepath.Dir(dir)
	}
	return "", "", ""
}

func gitInfo(dir string) *Git {
	if dir == "" {
		return nil
	}
	for d := dir; ; d = filepath.Dir(d) {
		gd := filepath.Join(d, ".git")
		if fi, err := os.Stat(gd); err == nil {
			if !fi.IsDir() { // worktree: "gitdir: <path>"
				b, _ := os.ReadFile(gd)
				gd = strings.TrimSpace(strings.TrimPrefix(string(b), "gitdir:"))
				if !filepath.IsAbs(gd) {
					gd = filepath.Join(d, gd)
				}
			}
			head, err := os.ReadFile(filepath.Join(gd, "HEAD"))
			if err != nil {
				return nil
			}
			g := &Git{}
			h := strings.TrimSpace(string(head))
			if ref, ok := strings.CutPrefix(h, "ref: "); ok {
				g.Branch = strings.TrimPrefix(ref, "refs/heads/")
				g.Commit = resolveRef(gd, ref)
			} else {
				g.Commit = h
			}
			if len(g.Commit) > 7 {
				g.Commit = g.Commit[:7]
			}
			return g
		}
		if d == "/" || d == "." {
			return nil
		}
	}
}

func resolveRef(gitDir, ref string) string {
	if b, err := os.ReadFile(filepath.Join(gitDir, ref)); err == nil {
		return strings.TrimSpace(string(b))
	}
	b, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if sha, name, ok := strings.Cut(line, " "); ok && name == ref {
			return sha
		}
	}
	return ""
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return "[]"
	}
	return string(b)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
