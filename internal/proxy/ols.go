package proxy

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// OpenLiteSpeed (what CyberPanel runs) config: line-based "key value"
// directives, "name args {" blocks and "<<<END_x ... END_x" heredocs.

type olsNode struct {
	key   string
	value string
	block []*olsNode
	isBlk bool
}

func parseOLS(src string) []*olsNode {
	root := &olsNode{isBlk: true}
	stack := []*olsNode{root}
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || line[0] == '#' {
			continue
		}
		cur := stack[len(stack)-1]
		if line == "}" {
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		value = strings.TrimSpace(value)
		if strings.HasSuffix(line, "{") {
			n := &olsNode{key: key, value: strings.TrimSpace(strings.TrimSuffix(value, "{")), isBlk: true}
			if key == "{" {
				n.key, n.value = "", ""
			}
			cur.block = append(cur.block, n)
			stack = append(stack, n)
			continue
		}
		if term, ok := strings.CutPrefix(value, "<<<"); ok {
			var body []string
			for i++; i < len(lines) && strings.TrimSpace(lines[i]) != term; i++ {
				body = append(body, lines[i])
			}
			value = strings.Join(body, "\n")
		}
		cur.block = append(cur.block, &olsNode{key: key, value: value})
	}
	return root.block
}

func olsGet(ns []*olsNode, key string) string {
	for _, n := range ns {
		if !n.isBlk && strings.EqualFold(n.key, key) {
			return n.value
		}
	}
	return ""
}

type olsVhost struct {
	name, root string
	domains    []string
	tls        bool
	body       []*olsNode
}

// proxyRule matches `RewriteRule ... HTTP://backend/... [P...]`.
var proxyRule = regexp.MustCompile(`(?i)RewriteRule\s+\S+\s+https?://([^/\s]+)`)

// OLSRoutes parses httpd_config.conf, every vhost.conf it references and
// each vhost's .htaccess, and returns domain -> proxy backend routes.
func OLSRoutes(confPath string) ([]Route, error) {
	src, err := os.ReadFile(confPath)
	if err != nil {
		return nil, err
	}
	serverRoot := filepath.Dir(filepath.Dir(confPath)) // .../lsws/conf/x.conf -> .../lsws
	top := parseOLS(string(src))

	procs := map[string]string{} // extprocessor name -> address (type proxy)
	collectProcs := func(ns []*olsNode) {
		for _, n := range ns {
			if n.isBlk && strings.EqualFold(n.key, "extprocessor") &&
				strings.EqualFold(olsGet(n.block, "type"), "proxy") {
				procs[n.value] = olsGet(n.block, "address")
			}
		}
	}
	collectProcs(top)

	vhosts := map[string]*olsVhost{}
	var order []string
	for _, n := range top {
		if !n.isBlk || !strings.EqualFold(n.key, "virtualhost") {
			continue
		}
		vh := &olsVhost{name: n.value, body: n.block}
		expand := func(s string) string {
			return strings.NewReplacer("$SERVER_ROOT", serverRoot, "$VH_NAME", vh.name, "$VH_ROOT", vh.root).Replace(s)
		}
		vh.root = strings.TrimSuffix(expand(olsGet(n.block, "vhRoot")), "/")
		if cf := expand(olsGet(n.block, "configFile")); cf != "" {
			if b, err := os.ReadFile(cf); err == nil {
				vh.body = append(vh.body, parseOLS(string(b))...)
			}
		}
		vhosts[vh.name] = vh
		order = append(order, vh.name)
	}

	for _, n := range top {
		if !n.isBlk || !strings.EqualFold(n.key, "listener") {
			continue
		}
		secure := olsGet(n.block, "secure") == "1" || strings.HasSuffix(olsGet(n.block, "address"), ":443")
		for _, m := range n.block {
			if m.isBlk || !strings.EqualFold(m.key, "map") {
				continue
			}
			name, list, _ := strings.Cut(m.value, " ")
			vh := vhosts[name]
			if vh == nil {
				continue
			}
			vh.tls = vh.tls || secure
			for _, d := range strings.Split(list, ",") {
				if d = strings.TrimSpace(d); d != "" && d != "*" && !slices.Contains(vh.domains, d) {
					vh.domains = append(vh.domains, d)
				}
			}
		}
	}

	var routes []Route
	for _, name := range order {
		vh := vhosts[name]
		expand := strings.NewReplacer("$VH_NAME", vh.name, "$VH_ROOT", vh.root, "$SERVER_ROOT", serverRoot).Replace
		if len(vh.domains) == 0 {
			if d := expand(olsGet(vh.body, "vhDomain")); d != "" {
				vh.domains = append(vh.domains, d)
			}
			for _, a := range strings.Split(expand(olsGet(vh.body, "vhAliases")), ",") {
				if a = strings.TrimSpace(a); a != "" {
					vh.domains = append(vh.domains, a)
				}
			}
		}
		if !vh.tls {
			for _, n := range vh.body {
				if n.isBlk && strings.EqualFold(n.key, "vhssl") {
					vh.tls = true
				}
			}
		}

		local := map[string]string{}
		for k, v := range procs {
			local[k] = v
		}
		vhProcs := map[string]string{}
		for _, n := range vh.body {
			if n.isBlk && strings.EqualFold(n.key, "extprocessor") &&
				strings.EqualFold(olsGet(n.block, "type"), "proxy") {
				local[n.value] = olsGet(n.block, "address")
				vhProcs[n.value] = local[n.value]
			}
		}

		used := map[string]string{} // backend -> path
		for _, n := range vh.body {
			switch {
			case n.isBlk && strings.EqualFold(n.key, "context") &&
				strings.EqualFold(olsGet(n.block, "type"), "proxy"):
				if h := olsGet(n.block, "handler"); h != "" {
					used[h] = n.value
				}
			case n.isBlk && strings.EqualFold(n.key, "rewrite"):
				for _, m := range proxyRule.FindAllStringSubmatch(olsGet(n.block, "rules"), -1) {
					setDefault(used, m[1], "/")
				}
			}
		}
		// CyberPanel's "Rewrite Rules" editor writes public_html/.htaccess.
		docRoot := expand(olsGet(vh.body, "docRoot"))
		if docRoot == "" && vh.root != "" {
			docRoot = vh.root + "/public_html"
		}
		if docRoot != "" {
			if b, err := os.ReadFile(filepath.Join(docRoot, ".htaccess")); err == nil {
				for _, m := range proxyRule.FindAllStringSubmatch(string(b), -1) {
					setDefault(used, m[1], "/")
				}
			}
		}
		// A proxy backend defined inside a vhost is meant for that vhost even
		// if the rule using it lives somewhere we can't read.
		for name := range vhProcs {
			setDefault(used, name, "/")
		}

		for backend, path := range used {
			addr, ok := local[backend]
			if !ok {
				addr = backend // rewrite straight to host:port
			}
			t, ok := parseOLSAddr(addr)
			if !ok {
				continue
			}
			for _, d := range vh.domains {
				routes = append(routes, Route{
					Domain: Domain{Host: d, Path: path, Source: "openlitespeed", TLS: vh.tls},
					UpHost: t.host, UpPort: t.port, Socket: t.socket,
				})
			}
		}
	}
	return routes, nil
}

func parseOLSAddr(a string) (target, bool) {
	a = strings.TrimSpace(a)
	if rest, ok := strings.CutPrefix(a, "uds://"); ok {
		return target{socket: "/" + strings.TrimPrefix(rest, "/")}, true
	}
	if i := strings.Index(a, "://"); i >= 0 {
		a = a[i+3:]
	}
	a = strings.TrimSuffix(a, "/")
	if a == "" || strings.ContainsAny(a, "$ ") {
		return target{}, false
	}
	t := parseServerAddr(a, 80)
	if len(t) == 0 {
		return target{}, false
	}
	return t[0], true
}

func setDefault(m map[string]string, k, v string) {
	if _, ok := m[k]; !ok {
		m[k] = v
	}
}
