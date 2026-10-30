// Package control starts, stops and restarts apps, runs their package
// scripts, and repoints proxy domains. Everything here is off unless the
// server was started with -controls and basic auth.
package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"nodetrack/internal/collector"
	"nodetrack/internal/procfs"
	"nodetrack/internal/store"
)

const (
	maxRunningJobs = 2 // small VPSes have one or two cores
	keepJobs       = 50
	jobTimeout     = 30 * time.Minute
	actionTimeout  = 90 * time.Second
	domainTimeout  = 60 * time.Second
)

var (
	ErrDisabled = errors.New("controls are disabled on this server")
	ErrBusy     = errors.New("busy")
)

// Refused is a request that can't be carried out (HTTP 422).
type Refused struct{ Msg string }

func (r Refused) Error() string { return r.Msg }

func refuse(format string, a ...any) error { return Refused{fmt.Sprintf(format, a...)} }

type Config struct {
	Enabled    bool
	Self       string // this binary, for the privileged config helper
	OLSConf    string
	OLSRestart string
	OLSCheck   string
}

type Controller struct {
	cfg   Config
	store *store.Store
	run   runner

	mu       sync.Mutex
	jobs     []*Job
	domainMu sync.Mutex
}

func New(cfg Config, st *store.Store) *Controller {
	return &Controller{cfg: cfg, store: st, run: newRunner()}
}

type Availability struct {
	Enabled bool     `json:"enabled"`
	Actions []string `json:"actions"`
	Jobs    bool     `json:"jobs"`
	Reason  string   `json:"reason"`
}

func (c *Controller) Enabled() bool { return c.cfg.Enabled }

func (c *Controller) Available(t collector.Target) Availability {
	a := Availability{Enabled: c.cfg.Enabled, Actions: []string{}}
	if !c.cfg.Enabled {
		a.Reason = "controls are off: start nodetrack with -controls and basic auth"
		return a
	}
	var reasons []string
	switch t.Source {
	case "systemd", "pm2":
		if t.Up {
			a.Actions = append(a.Actions, "stop", "restart")
		} else {
			a.Actions = append(a.Actions, "start")
		}
	default:
		if t.Up {
			a.Actions = append(a.Actions, "stop")
			reasons = append(reasons, "started by hand: it can be stopped, but nothing can start it again from here")
		} else {
			reasons = append(reasons, "started by hand: nothing can start it again from here")
		}
	}
	if _, err := os.Stat(filepath.Join(t.Dir, "package.json")); err != nil {
		reasons = append(reasons, "no package.json in "+t.Dir)
	} else if o, err := collector.Owner(t.Dir); err != nil {
		reasons = append(reasons, "can't read the owner of "+t.Dir)
	} else if !c.run.systemd && o.UID != os.Getuid() {
		reasons = append(reasons, "scripts run as the app's owner, which needs nodetrack running as root")
	} else {
		a.Jobs = true
	}
	a.Reason = strings.Join(reasons, "; ")
	return a
}

// Action starts, stops or restarts an app and waits for the result.
func (c *Controller) Action(t collector.Target, action string) (string, error) {
	if !c.cfg.Enabled {
		return "", ErrDisabled
	}
	if !slices.Contains(c.Available(t).Actions, action) {
		state := "running"
		if !t.Up {
			state = "stopped"
		}
		return "", refuse("can't %s a %s %s app", action, state, t.Source)
	}
	var out string
	var err error
	switch t.Source {
	case "systemd":
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		var b []byte
		b, err = exec.CommandContext(ctx, "systemctl", action, t.Unit).CombinedOutput()
		cancel()
		out = string(b)
	case "pm2":
		out, err = c.pm2(t, action)
	default:
		out, err = stopProcesses(t)
	}
	detail := action + ": ok"
	if err != nil {
		detail = action + ": failed"
	}
	c.event(t.ID, "action", detail)
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return out, refuse("%s failed: %s", action, msg)
	}
	return out, nil
}

func (c *Controller) pm2(t collector.Target, action string) (string, error) {
	uid := t.UID
	if uid < 0 {
		o, err := collector.Owner(t.Dir)
		if err != nil {
			return "", err
		}
		uid = o.UID
	}
	o, err := ownerByUID(uid)
	if err != nil {
		return "", err
	}
	home := cmpOr(t.PM2Home, filepath.Join(o.Home, ".pm2"))
	// Without a running daemon the pm2 CLI would spawn one inside this
	// short-lived command, and it would die with it.
	if !pm2DaemonRunning(home) {
		return "", refuse("the pm2 daemon for %s (%s) isn't running", o.Name, home)
	}
	bin := findExecutable("pm2", t.Exe, o.Home)
	if bin == "" {
		return "", refuse("pm2 executable not found for %s", o.Name)
	}
	env := append(baseEnv(o, t.Exe), "PM2_HOME="+home)
	out, code, err := c.run.run(spec{Name: "pm2-" + randID(), UID: o.UID, GID: o.GID,
		Dir: t.Dir, Env: env, Argv: []string{bin, action, t.Unit}}, actionTimeout)
	if err == nil && code != 0 {
		err = fmt.Errorf("pm2 exited with %d", code)
	}
	return out, err
}

func pm2DaemonRunning(home string) bool {
	pids, _ := procfs.PIDs(nil)
	r := procfs.NewReader()
	for _, pid := range pids {
		argv, err := r.Cmdline(pid)
		if err != nil || len(argv) == 0 {
			continue
		}
		s := strings.Join(argv, " ")
		if strings.Contains(s, "God Daemon") && strings.Contains(s, "("+home+")") {
			return true
		}
	}
	return false
}

// stopProcesses sends SIGTERM to a hand-started app and waits for it to go.
func stopProcesses(t collector.Target) (string, error) {
	pids := t.PIDs
	if len(pids) == 0 && t.RootPID > 0 {
		pids = []int{t.RootPID}
	}
	for _, pid := range pids {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return "", fmt.Errorf("signal pid %d: %w", pid, err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		alive := 0
		for _, pid := range pids {
			if syscall.Kill(pid, 0) == nil {
				alive++
			}
		}
		if alive == 0 {
			return fmt.Sprintf("sent SIGTERM to %v; all exited", pids), nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return "", fmt.Errorf("processes %v still running 10s after SIGTERM", pids)
}

// StartJob runs `<pm> install` or `<pm> run <script>` as the app's owner.
func (c *Controller) StartJob(t collector.Target, kind, script string) (*Job, error) {
	if !c.cfg.Enabled {
		return nil, ErrDisabled
	}
	if !c.Available(t).Jobs {
		return nil, refuse("%s", c.Available(t).Reason)
	}
	o, err := collector.Owner(t.Dir)
	if err != nil {
		return nil, err
	}
	pm := collector.PackageManager(t.Dir, t.Runtime)
	var args []string
	switch kind {
	case "install":
		args = []string{"install"}
	case "run":
		if !slices.ContainsFunc(collector.Scripts(t.Dir), func(s collector.Script) bool { return s.Name == script }) {
			return nil, refuse("package.json has no script %q", script)
		}
		args = []string{"run", script}
	default:
		return nil, refuse("unknown job kind %q", kind)
	}
	bin := findExecutable(pm, t.Exe, o.Home)
	if bin == "" {
		return nil, refuse("%s not found next to %s or in the usual places", pm, t.Exe)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	running := 0
	for _, j := range c.jobs {
		if j.View(false).Status == "running" {
			if j.AppID == t.ID {
				return nil, fmt.Errorf("%w: a job is already running for this app", ErrBusy)
			}
			running++
		}
	}
	if running >= maxRunningJobs {
		return nil, fmt.Errorf("%w: %d jobs are already running", ErrBusy, running)
	}

	job := newJob(t.ID, kind, script, pm+" "+strings.Join(args, " "), o.Name, t.Dir)
	env := append(baseEnv(o, t.Exe), "FORCE_COLOR=0")
	switch pm {
	case "npm":
		env = append(env, "npm_config_fund=false", "npm_config_update_notifier=false")
	case "pnpm":
		// Without a TTY pnpm aborts instead of asking to rebuild node_modules.
		env = append(env, "npm_config_confirm_modules_purge=false", "npm_config_update_notifier=false")
	}
	fmt.Fprintf(job, "$ %s   (as %s in %s)\n", pm+" "+strings.Join(args, " "), o.Name, t.Dir)
	p, err := c.run.start(spec{Name: "job-" + job.ID, UID: o.UID, GID: o.GID, Dir: t.Dir,
		Env: env, Argv: append([]string{bin}, args...)}, job)
	if err != nil {
		return nil, err
	}
	job.proc = p
	c.jobs = append(c.jobs, job)
	c.trimJobs()

	go func() {
		timer := time.AfterFunc(jobTimeout, func() { c.Cancel(job.ID) })
		code, err := p.wait()
		timer.Stop()
		job.finish(code, err)
		v := job.View(false)
		detail := fmt.Sprintf("%s: %s", v.Command, v.Status)
		if v.ExitCode != nil && v.Status != "canceled" {
			detail = fmt.Sprintf("%s: exit %d", v.Command, *v.ExitCode)
		}
		c.event(t.ID, "job", fmt.Sprintf("%s, %ds", detail, v.FinishedAt-v.StartedAt))
	}()
	return job, nil
}

func (c *Controller) trimJobs() {
	for len(c.jobs) > keepJobs {
		i := slices.IndexFunc(c.jobs, func(j *Job) bool { return j.View(false).Status != "running" })
		if i < 0 {
			return
		}
		c.jobs = slices.Delete(c.jobs, i, i+1)
	}
}

func (c *Controller) Job(id string) *Job {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, j := range c.jobs {
		if j.ID == id {
			return j
		}
	}
	return nil
}

// JobsFor returns an app's most recent jobs, newest first, without output.
func (c *Controller) JobsFor(appID int64, n int) []JobInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []JobInfo{}
	for i := len(c.jobs) - 1; i >= 0 && len(out) < n; i-- {
		if c.jobs[i].AppID == appID {
			out = append(out, c.jobs[i].View(false))
		}
	}
	return out
}

func (c *Controller) Cancel(id string) (*Job, error) {
	if !c.cfg.Enabled {
		return nil, ErrDisabled
	}
	j := c.Job(id)
	if j == nil {
		return nil, nil
	}
	j.mu.Lock()
	running := j.info.Status == "running"
	if running {
		j.canceled = true
	}
	j.mu.Unlock()
	if running && j.proc != nil {
		go j.proc.stop()
	}
	return j, nil
}

// PointDomain repoints an OpenLiteSpeed route at addr. The edit runs as a
// separate privileged process because this service's filesystem is read-only.
func (c *Controller) PointDomain(t collector.Target, vhost, backend, addr, label string) (string, error) {
	if !c.cfg.Enabled {
		return "", ErrDisabled
	}
	if c.cfg.OLSConf == "" {
		return "", refuse("OpenLiteSpeed support is disabled")
	}
	if !c.domainMu.TryLock() {
		return "", fmt.Errorf("%w: another domain change is in progress", ErrBusy)
	}
	defer c.domainMu.Unlock()
	argv := []string{c.cfg.Self, "ols-point", "-conf", c.cfg.OLSConf, "-vhost", vhost,
		"-backend", backend, "-addr", addr, "-restart", c.cfg.OLSRestart, "-check", c.cfg.OLSCheck}
	out, code, err := c.run.run(spec{Name: "ols-" + randID(), Root: true,
		Env: []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}, Argv: argv}, domainTimeout)
	if err == nil && code != 0 {
		err = fmt.Errorf("exit %d", code)
	}
	detail := fmt.Sprintf("domain %s -> %s: ok", label, addr)
	if err != nil {
		detail = fmt.Sprintf("domain %s -> %s: failed", label, addr)
	}
	c.event(t.ID, "action", detail)
	if err != nil {
		return out, refuse("changing the domain failed: %s", cmpOr(strings.TrimSpace(out), err.Error()))
	}
	return out, nil
}

func (c *Controller) event(appID int64, kind, detail string) {
	if err := c.store.AddEvent(store.Event{AppID: appID, TS: time.Now().Unix(), Kind: kind, Detail: detail}); err != nil {
		slog.Error("record event", "err", err)
	}
	slog.Info("control", "app", appID, "kind", kind, "detail", detail)
}

func baseEnv(o collector.OwnerInfo, exe string) []string {
	path := []string{}
	if exe != "" {
		path = append(path, filepath.Dir(exe))
	}
	if o.Home != "" {
		path = append(path, o.Home+"/.bun/bin", o.Home+"/.local/share/pnpm", o.Home+"/.npm-global/bin")
	}
	path = append(path, "/usr/local/bin", "/usr/bin", "/bin")
	return []string{"PATH=" + strings.Join(path, ":"), "HOME=" + o.Home, "USER=" + o.Name, "LOGNAME=" + o.Name}
}

// findExecutable looks for a tool next to the app's runtime first, so an
// nvm-installed npm matches the node the app runs on.
func findExecutable(name, exe, home string) string {
	var dirs []string
	if exe != "" {
		if name == "bun" && filepath.Base(exe) == "bun" {
			return exe
		}
		dirs = append(dirs, filepath.Dir(exe))
	}
	if home != "" {
		dirs = append(dirs, home+"/.bun/bin", home+"/.local/share/pnpm", home+"/.npm-global/bin")
	}
	dirs = append(dirs, "/usr/local/bin", "/usr/bin", "/bin")
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

func ownerByUID(uid int) (collector.OwnerInfo, error) {
	b, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return collector.OwnerInfo{}, err
	}
	for line := range bytes.SplitSeq(b, []byte{'\n'}) {
		f := strings.Split(string(line), ":")
		if len(f) < 7 || f[2] != strconv.Itoa(uid) {
			continue
		}
		gid, _ := strconv.Atoi(f[3])
		return collector.OwnerInfo{UID: uid, GID: gid, Name: f[0], Home: f[5]}, nil
	}
	return collector.OwnerInfo{}, fmt.Errorf("no user with uid %d", uid)
}

func randID() string {
	var b [4]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
