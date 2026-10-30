package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type caddyRoute struct {
	Match []struct {
		Host []string `json:"host"`
		Path []string `json:"path"`
	} `json:"match"`
	Handle []caddyHandler `json:"handle"`
}

type caddyHandler struct {
	Handler   string       `json:"handler"`
	Routes    []caddyRoute `json:"routes"`
	Upstreams []struct {
		Dial string `json:"dial"`
	} `json:"upstreams"`
}

type caddyConfig struct {
	Apps struct {
		HTTP struct {
			Servers map[string]struct {
				Listen []string     `json:"listen"`
				Routes []caddyRoute `json:"routes"`
			} `json:"servers"`
		} `json:"http"`
	} `json:"apps"`
}

// CaddyRoutes reads the live config from Caddy's admin API. This works for
// both Caddyfile and JSON setups because Caddy always exposes adapted JSON.
func CaddyRoutes(ctx context.Context, admin string) ([]Route, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(admin, "/")+"/config/", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("caddy admin: %s", resp.Status)
	}
	var cfg caddyConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, err
	}
	var routes []Route
	for _, srv := range cfg.Apps.HTTP.Servers {
		tls := false
		for _, l := range srv.Listen {
			if strings.HasSuffix(l, ":443") {
				tls = true
			}
		}
		walkCaddy(srv.Routes, nil, "/", tls, &routes)
	}
	return routes, nil
}

func walkCaddy(rs []caddyRoute, hosts []string, path string, tls bool, out *[]Route) {
	for _, r := range rs {
		h, p := hosts, path
		for _, m := range r.Match {
			if len(m.Host) > 0 {
				h = m.Host
			}
			if len(m.Path) > 0 {
				p = m.Path[0]
			}
		}
		for _, hd := range r.Handle {
			switch hd.Handler {
			case "subroute":
				walkCaddy(hd.Routes, h, p, tls, out)
			case "reverse_proxy":
				names := h
				if len(names) == 0 {
					names = []string{"*"}
				}
				for _, u := range hd.Upstreams {
					t, ok := parseDial(u.Dial)
					if !ok {
						continue
					}
					for _, n := range names {
						*out = append(*out, Route{
							Domain: Domain{Host: n, Path: p, Source: "caddy", TLS: tls},
							UpHost: t.host, UpPort: t.port, Socket: t.socket,
						})
					}
				}
			}
		}
	}
}

// parseDial handles "localhost:3000" and "unix//run/app.sock".
func parseDial(d string) (target, bool) {
	if strings.Contains(d, "{") {
		return target{}, false // placeholder, resolved per request
	}
	if rest, ok := strings.CutPrefix(d, "unix/"); ok {
		return target{socket: rest}, true
	}
	t := parseServerAddr(d, 80)
	if len(t) == 0 {
		return target{}, false
	}
	return t[0], true
}
