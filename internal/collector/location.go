package collector

import (
	"encoding/json"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

func location(t Target) Location {
	l := Location{
		Dir: t.Dir, Cwd: t.Cwd, Entry: t.Entry, Exe: t.Exe,
		RuntimeVersion: RuntimeVersion(t.Exe),
		PackageManager: PackageManager(t.Dir, t.Runtime),
		GitRemote:      gitRemote(t.Dir),
	}
	if o, err := Owner(t.Dir); err == nil {
		l.User = o.Name
	}
	switch t.Source {
	case "systemd":
		l.UnitFile = unitFile(t.Unit)
		l.Logs = "journalctl -u " + t.Unit + " -f"
	case "pm2":
		l.Logs = "pm2 logs " + t.Unit
	}
	return l
}

// Scripts returns package.json scripts sorted by name.
func Scripts(dir string) []Script {
	out := []Script{}
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return out
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(b, &pkg) != nil {
		return out
	}
	for name, cmd := range pkg.Scripts {
		out = append(out, Script{Name: name, Command: cmd})
	}
	slices.SortFunc(out, func(a, b Script) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// PackageManager picks the manager from the lockfile in dir.
func PackageManager(dir, runtime string) string {
	for _, c := range []struct{ file, pm string }{
		{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"},
		{"bun.lock", "bun"}, {"bun.lockb", "bun"}, {"package-lock.json", "npm"},
	} {
		if _, err := os.Stat(filepath.Join(dir, c.file)); err == nil {
			return c.pm
		}
	}
	if runtime == "bun" {
		return "bun"
	}
	return "npm"
}

type OwnerInfo struct {
	UID, GID int
	Name     string
	Home     string
}

// Owner returns the user owning dir; jobs run as this user.
func Owner(dir string) (OwnerInfo, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return OwnerInfo{}, err
	}
	st := fi.Sys().(*syscall.Stat_t)
	o := OwnerInfo{UID: int(st.Uid), GID: int(st.Gid), Name: strconv.Itoa(int(st.Uid))}
	if u, err := user.LookupId(o.Name); err == nil {
		o.Name, o.Home = u.Username, u.HomeDir
		if g, err := strconv.Atoi(u.Gid); err == nil {
			o.GID = g
		}
	}
	return o, nil
}

var nodeVersionDir = regexp.MustCompile(`/(v\d+\.\d+\.\d+)/`)
var headerVersion = regexp.MustCompile(`#define NODE_(MAJOR|MINOR|PATCH)_VERSION (\d+)`)

// RuntimeVersion works out the version without executing the runtime (a
// node process would cost the tracker ~40 MiB): nvm/fnm/volta paths carry it,
// and node installs ship include/node/node_version.h.
func RuntimeVersion(exe string) string {
	if exe == "" {
		return ""
	}
	if m := nodeVersionDir.FindStringSubmatch(exe); m != nil {
		return m[1]
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "..", "include", "node", "node_version.h"))
	if err != nil {
		return ""
	}
	v := map[string]string{}
	for _, m := range headerVersion.FindAllStringSubmatch(string(b), -1) {
		v[m[1]] = m[2]
	}
	if v["MAJOR"] == "" {
		return ""
	}
	return "v" + v["MAJOR"] + "." + v["MINOR"] + "." + v["PATCH"]
}

func unitFile(unit string) string {
	if unit == "" {
		return ""
	}
	for _, d := range []string{"/etc/systemd/system", "/run/systemd/system", "/usr/local/lib/systemd/system",
		"/usr/lib/systemd/system", "/lib/systemd/system"} {
		p := filepath.Join(d, unit)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// gitRemote returns origin's URL with any credentials removed.
func gitRemote(dir string) string {
	for d := dir; d != "" && d != "/" && d != "."; d = filepath.Dir(d) {
		b, err := os.ReadFile(filepath.Join(d, ".git", "config"))
		if err != nil {
			continue
		}
		inOrigin := false
		for line := range strings.SplitSeq(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") {
				inOrigin = line == `[remote "origin"]`
				continue
			}
			if k, v, ok := strings.Cut(line, "="); ok && inOrigin && strings.TrimSpace(k) == "url" {
				return stripCredentials(strings.TrimSpace(v))
			}
		}
		return ""
	}
	return ""
}

func stripCredentials(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		// scp-like "git@github.com:me/app.git": no secret in it.
		if strings.Contains(raw, "://") {
			return ""
		}
		return raw
	}
	u.User = nil
	return u.String()
}
