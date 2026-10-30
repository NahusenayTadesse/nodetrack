package proxy

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type directive struct {
	name  string
	args  []string
	block []directive // nil unless the directive has a { } body
}

// NginxRoutes parses the config rooted at confPath (following include
// globs) and extracts every server_name -> proxy_pass/grpc_pass route.
func NginxRoutes(confPath string) ([]Route, error) {
	tree, err := loadNginx(confPath, filepath.Dir(confPath), 0)
	if err != nil {
		return nil, err
	}
	return extractNginx(tree), nil
}

func loadNginx(path, prefix string, depth int) ([]directive, error) {
	if depth > 10 {
		return nil, errors.New("nginx: include depth exceeded")
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	toks, err := tokenize(string(src))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	p := parser{toks: toks}
	tree, err := p.block(false)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return expandIncludes(tree, prefix, depth)
}

func expandIncludes(ds []directive, prefix string, depth int) ([]directive, error) {
	out := make([]directive, 0, len(ds))
	for _, d := range ds {
		if d.name == "include" && d.block == nil && len(d.args) == 1 {
			pat := d.args[0]
			if !filepath.IsAbs(pat) {
				pat = filepath.Join(prefix, pat)
			}
			matches, _ := filepath.Glob(pat)
			for _, m := range matches {
				sub, err := loadNginx(m, prefix, depth+1)
				if err != nil {
					// One unreadable/broken include shouldn't hide every other site.
					continue
				}
				out = append(out, sub...)
			}
			continue
		}
		if d.block != nil {
			b, err := expandIncludes(d.block, prefix, depth)
			if err != nil {
				return nil, err
			}
			d.block = b
		}
		out = append(out, d)
	}
	return out, nil
}

func tokenize(s string) ([]string, error) {
	var toks []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == ';' || c == '{' || c == '}':
			toks = append(toks, s[i:i+1])
			i++
		case c == '"' || c == '\'':
			var b strings.Builder
			j := i + 1
			for ; j < len(s) && s[j] != c; j++ {
				if s[j] == '\\' && j+1 < len(s) {
					j++
				}
				b.WriteByte(s[j])
			}
			if j >= len(s) {
				return nil, errors.New("unterminated quote")
			}
			// Prefix with a NUL marker so a quoted ";" isn't read as syntax.
			toks = append(toks, "\x00"+b.String())
			i = j + 1
		default:
			j := i
			for j < len(s) {
				d := s[j]
				if d == ' ' || d == '\t' || d == '\n' || d == '\r' || d == ';' || d == '{' || d == '}' {
					// ${var} is an argument, not a block.
					if d == '{' && j > i && s[j-1] == '$' {
						if k := strings.IndexByte(s[j:], '}'); k > 0 {
							j += k + 1
							continue
						}
					}
					break
				}
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks, nil
}

type parser struct {
	toks []string
	pos  int
}

func (p *parser) block(nested bool) ([]directive, error) {
	var ds []directive
	for p.pos < len(p.toks) {
		t := p.toks[p.pos]
		if t == "}" {
			if !nested {
				return nil, errors.New("unexpected }")
			}
			p.pos++
			return ds, nil
		}
		d := directive{name: unquote(t)}
		p.pos++
		for p.pos < len(p.toks) {
			t = p.toks[p.pos]
			p.pos++
			if t == ";" {
				break
			}
			if t == "{" {
				body, err := p.block(true)
				if err != nil {
					return nil, err
				}
				if body == nil {
					body = []directive{}
				}
				d.block = body
				break
			}
			d.args = append(d.args, unquote(t))
		}
		ds = append(ds, d)
	}
	if nested {
		return nil, errors.New("missing }")
	}
	return ds, nil
}

func unquote(t string) string { return strings.TrimPrefix(t, "\x00") }

type target struct {
	host   string
	port   int
	socket string
}

func extractNginx(tree []directive) []Route {
	upstreams := map[string][]string{}
	var servers []directive
	var walk func([]directive)
	walk = func(ds []directive) {
		for _, d := range ds {
			switch {
			case d.name == "upstream" && d.block != nil && len(d.args) > 0:
				for _, s := range d.block {
					if s.name == "server" && len(s.args) > 0 {
						upstreams[d.args[0]] = append(upstreams[d.args[0]], s.args[0])
					}
				}
			case d.name == "server" && d.block != nil:
				servers = append(servers, d)
			case d.block != nil:
				walk(d.block)
			}
		}
	}
	walk(tree)

	var routes []Route
	for _, srv := range servers {
		var names []string
		tls := false
		listenPort := ""
		vars := map[string]string{}
		for _, d := range srv.block {
			switch d.name {
			case "server_name":
				for _, n := range d.args {
					if n != "" && n != "_" && !strings.HasPrefix(n, "~") {
						names = append(names, n)
					}
				}
			case "listen":
				for _, a := range d.args {
					if a == "ssl" || a == "quic" {
						tls = true
					}
				}
				if len(d.args) > 0 {
					p := d.args[0]
					if i := strings.LastIndexByte(p, ':'); i >= 0 {
						p = p[i+1:]
					}
					if p == "443" {
						tls = true
					}
					if listenPort == "" {
						listenPort = p
					}
				}
			case "set":
				if len(d.args) == 2 {
					vars[d.args[0]] = d.args[1]
				}
			}
		}
		if len(names) == 0 {
			names = []string{"*:" + listenPort}
		}

		var locs func(ds []directive, path string)
		locs = func(ds []directive, path string) {
			for _, d := range ds {
				switch {
				case (d.name == "proxy_pass" || d.name == "grpc_pass") && len(d.args) > 0:
					for _, t := range parsePass(d.args[0], upstreams, vars) {
						for _, n := range names {
							routes = append(routes, Route{
								Domain: Domain{Host: n, Path: path, Source: "nginx", TLS: tls},
								UpHost: t.host, UpPort: t.port, Socket: t.socket,
							})
						}
					}
				case d.name == "location" && d.block != nil && len(d.args) > 0:
					locs(d.block, d.args[len(d.args)-1])
				case d.name == "if" && d.block != nil:
					locs(d.block, path)
				}
			}
		}
		locs(srv.block, "/")
	}
	return routes
}

func parsePass(v string, upstreams map[string][]string, vars map[string]string) []target {
	if strings.HasPrefix(v, "$") {
		if r, ok := vars[v]; ok {
			v = r
		}
	}
	defPort := 80
	for _, s := range [...]string{"https://", "grpcs://"} {
		if strings.HasPrefix(v, s) {
			defPort = 443
		}
	}
	if i := strings.Index(v, "://"); i >= 0 {
		v = v[i+3:]
	}
	if strings.HasPrefix(v, "unix:") {
		return parseServerAddr(v, defPort)
	}
	if i := strings.IndexByte(v, '/'); i >= 0 {
		v = v[:i]
	}
	if strings.Contains(v, "$") {
		return nil // runtime-resolved target; can't know statically
	}
	if ups, ok := upstreams[v]; ok {
		var out []target
		for _, u := range ups {
			out = append(out, parseServerAddr(u, defPort)...)
		}
		return out
	}
	return parseServerAddr(v, defPort)
}

// parseServerAddr handles "127.0.0.1:3000", "[::1]:3000", "localhost",
// and "unix:/run/app.sock" (optionally followed by ":/uri").
func parseServerAddr(a string, defPort int) []target {
	if rest, ok := strings.CutPrefix(a, "unix:"); ok {
		if i := strings.IndexByte(rest, ':'); i >= 0 {
			rest = rest[:i]
		}
		return []target{{socket: rest}}
	}
	host, portStr, err := net.SplitHostPort(a)
	if err != nil {
		return []target{{host: strings.Trim(a, "[]"), port: defPort}}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil
	}
	return []target{{host: host, port: port}}
}

// configSignature hashes path, size and mtime of every file under the
// config's directory so routes are only re-parsed when something changed.
func configSignature(confPath string) uint64 {
	h := fnv.New64a()
	filepath.WalkDir(filepath.Dir(confPath), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := os.Stat(p) // follows sites-enabled symlinks
		if err != nil || info.IsDir() {
			return nil
		}
		fmt.Fprintf(h, "%s|%d|%d\n", p, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return h.Sum64()
}
