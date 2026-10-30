// Package proxy discovers which public domains a reverse proxy (nginx or
// Caddy) routes to which local ports and unix sockets.
package proxy

import (
	"context"
	"log/slog"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"nodetrack/internal/procfs"
)

// Domain is a public entry point that ends up at an app.
type Domain struct {
	Host   string `json:"host"`
	Path   string `json:"path"`
	Source string `json:"source"`
	TLS    bool   `json:"tls"`
}

// Route is one proxy rule: Domain -> upstream.
type Route struct {
	Domain
	UpHost string // empty when Socket is set
	UpPort int
	Socket string
}

type Resolver struct {
	NginxConf  string // e.g. /etc/nginx/nginx.conf; "" disables
	OLSConf    string // e.g. /usr/local/lsws/conf/httpd_config.conf; "" disables
	CaddyAdmin string // e.g. http://127.0.0.1:2019; "" disables

	mu       sync.RWMutex
	nginx    []Route
	ols      []Route
	olsModel *olsModel
	caddy    []Route
	localIPs map[string]bool

	refreshMu        sync.Mutex // serializes refreshes and guards the signatures
	nginxSig, olsSig uint64
}

// RefreshNow re-reads proxy configs immediately (after an edit).
func (r *Resolver) RefreshNow(ctx context.Context) { r.refresh(ctx) }

// Run refreshes routes immediately and then periodically until ctx ends.
func (r *Resolver) Run(ctx context.Context, every time.Duration) {
	r.refresh(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.refresh(ctx)
		}
	}
}

func (r *Resolver) refresh(ctx context.Context) {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	local := map[string]bool{"localhost": true, "0.0.0.0": true, "::": true}
	if h, err := os.Hostname(); err == nil {
		local[h] = true
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				local[ipn.IP.String()] = true
			}
		}
	}

	nginx, nginxChanged := loadIfChanged("nginx", r.NginxConf, &r.nginxSig, NginxRoutes,
		func(rs []Route) int { return len(rs) })
	ols, olsChanged := loadIfChanged("openlitespeed", r.OLSConf, &r.olsSig, loadOLSModel,
		func(m *olsModel) int { return len(m.vhosts) })

	var caddy []Route
	caddyOK := false
	if r.CaddyAdmin != "" {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		routes, err := CaddyRoutes(cctx, r.CaddyAdmin)
		cancel()
		caddy, caddyOK = routes, err == nil
	}

	r.mu.Lock()
	r.localIPs = local
	if nginxChanged {
		r.nginx = nginx
	}
	if olsChanged {
		r.olsModel, r.ols = ols, ols.routes()
	}
	if r.CaddyAdmin != "" {
		if caddyOK {
			r.caddy = caddy
		} else {
			r.caddy = nil
		}
	}
	r.mu.Unlock()
}

// loadIfChanged re-parses a file-based proxy config only when a file under
// its directory changed since the last successful parse.
func loadIfChanged[T any](name, conf string, sig *uint64, parse func(string) (T, error), count func(T) int) (T, bool) {
	var zero T
	if conf == "" {
		return zero, false
	}
	if _, err := os.Stat(conf); err != nil {
		return zero, false
	}
	s := configSignature(conf)
	if s == *sig {
		return zero, false
	}
	v, err := parse(conf)
	if err != nil {
		slog.Warn("proxy config", "proxy", name, "err", err)
		return zero, false
	}
	*sig = s
	slog.Info("proxy config loaded", "proxy", name, "items", count(v))
	return v, true
}

// Match returns the domains that route to any of the given listeners or
// unix sockets, sorted and de-duplicated.
func (r *Resolver) Match(ports []procfs.Listener, sockets []string) []Domain {
	if len(ports) == 0 && len(sockets) == 0 {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Domain
	check := func(routes []Route) {
		for _, rt := range routes {
			if rt.matches(ports, sockets, r.localIPs) && !slices.Contains(out, rt.Domain) {
				out = append(out, rt.Domain)
			}
		}
	}
	check(r.nginx)
	check(r.ols)
	check(r.caddy)
	slices.SortFunc(out, func(a, b Domain) int {
		if c := strings.Compare(a.Host, b.Host); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

func (rt *Route) matches(ports []procfs.Listener, sockets []string, local map[string]bool) bool {
	if rt.Socket != "" {
		return slices.Contains(sockets, rt.Socket)
	}
	if !local[rt.UpHost] && !isLoopback(rt.UpHost) {
		return false
	}
	for _, l := range ports {
		if l.Port != rt.UpPort {
			continue
		}
		// An app bound to a single address only answers on that address.
		if l.Addr == "0.0.0.0" || l.Addr == "::" || l.Addr == rt.UpHost ||
			(isLoopback(l.Addr) && isLoopback(rt.UpHost)) {
			return true
		}
	}
	return false
}

func isLoopback(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
