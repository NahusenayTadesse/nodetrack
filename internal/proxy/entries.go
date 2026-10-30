package proxy

import (
	"slices"
	"strings"

	"nodetrack/internal/procfs"
)

// Entry is one domain route as offered in the "point a domain here" picker.
type Entry struct {
	ID       string   `json:"id"`
	Hosts    []string `json:"hosts"`
	Path     string   `json:"path"`
	TLS      bool     `json:"tls"`
	Source   string   `json:"source"`
	Port     int      `json:"port"`
	Socket   string   `json:"socket"`
	AppID    *int64   `json:"appId"`
	AppName  string   `json:"appName"`
	Static   bool     `json:"static"`
	Editable bool     `json:"editable"`
	Reason   string   `json:"reason"`

	upHost string
}

// AppListeners is what Entries needs to know which app sits behind a route.
type AppListeners struct {
	ID      int64
	Name    string
	Ports   []procfs.Listener
	Sockets []string
}

// Entries lists every route the proxies know, with the app currently behind
// it. Only OpenLiteSpeed routes can be edited.
func (r *Resolver) Entries(apps []AppListeners) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Entry
	if m := r.olsModel; m != nil {
		for _, vh := range m.vhosts {
			if len(vh.domains) == 0 {
				continue
			}
			if len(vh.backends) == 0 {
				e := Entry{ID: vh.name + "#", Hosts: vh.domains, Path: "/", TLS: vh.tls,
					Source: "openlitespeed", Static: true, Editable: true}
				switch {
				case vh.configFile == "":
					e.Editable, e.Reason = false, "site config is inline in httpd_config.conf"
				case !vh.htaccess || vh.docRoot == "":
					e.Editable, e.Reason = false, "site doesn't load .htaccess rewrite rules"
				}
				out = append(out, e)
				continue
			}
			for _, b := range vh.backends {
				t, _ := parseOLSAddr(b.addr)
				e := Entry{ID: vh.name + "#" + b.name, Hosts: vh.domains, Path: b.path, TLS: vh.tls,
					Source: "openlitespeed", Port: t.port, Socket: t.socket, upHost: t.host, Editable: true}
				switch {
				case vh.configFile == "":
					e.Editable, e.Reason = false, "site config is inline in httpd_config.conf"
				case !b.local && b.addr == b.name:
					e.Editable, e.Reason = false, "a rewrite rule proxies straight to this address; change it in CyberPanel"
				case !b.local:
					e.Editable, e.Reason = false, "backend \""+b.name+"\" is shared by other sites (httpd_config.conf)"
				}
				out = append(out, e)
			}
		}
	}
	other := func(routes []Route, why string) {
		for _, rt := range routes {
			out = append(out, Entry{ID: rt.Source + "#" + rt.Host + "#" + rt.Path, Hosts: []string{rt.Host},
				Path: rt.Path, TLS: rt.TLS, Source: rt.Source, Port: rt.UpPort, Socket: rt.Socket,
				upHost: rt.UpHost, Reason: why})
		}
	}
	other(r.nginx, "editing nginx configs isn't supported yet")
	other(r.caddy, "editing Caddy configs isn't supported yet")

	for i := range out {
		e := &out[i]
		rt := Route{UpHost: e.upHost, UpPort: e.Port, Socket: e.Socket}
		if rt.UpHost == "" && rt.Socket == "" {
			continue
		}
		for _, a := range apps {
			if rt.matches(a.Ports, a.Sockets, r.localIPs) {
				id := a.ID
				e.AppID, e.AppName = &id, a.Name
				break
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Entry) int {
		return strings.Compare(a.Hosts[0]+a.Path, b.Hosts[0]+b.Path)
	})
	return out
}

// OLSEntry splits an OpenLiteSpeed entry ID into vhost and backend.
func OLSEntry(id string) (vhost, backend string, ok bool) {
	if strings.HasPrefix(id, "nginx#") || strings.HasPrefix(id, "caddy#") {
		return "", "", false
	}
	return strings.Cut(id, "#")
}
