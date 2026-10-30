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

// proxyRule matches `RewriteRule ... HTTP://backend/... [P...]`.
var proxyRule = regexp.MustCompile(`(?i)RewriteRule\s+\S+\s+https?://([^/\s]+)`)

// olsBackend is a proxy target used by a vhost.
type olsBackend struct {
	name  string // extprocessor name; host:port when a rule proxies directly
	addr  string
	path  string
	local bool // defined in the vhost's own config file (safe to edit)
}

type olsVhost struct {
	name, root   string
	configFile   string
	docRoot      string
	htaccess     bool // autoLoadHtaccess on
	domains      []string
	tls          bool
	backends     []olsBackend
	body         []*olsNode
	htaccessFile string
}

type olsModel struct {
	vhosts []*olsVhost
}

func loadOLSModel(confPath string) (*olsModel, error) {
	src, err := os.ReadFile(confPath)
	if err != nil {
		return nil, err
	}
	serverRoot := filepath.Dir(filepath.Dir(confPath)) // .../lsws/conf/x.conf -> .../lsws
	top := parseOLS(string(src))

	global := map[string]string{} // extprocessor name -> address (type proxy)
	for _, n := range top {
		if isProxyProc(n) {
			global[n.value] = olsGet(n.block, "address")
		}
	}

	m := &olsModel{}
	byName := map[string]*olsVhost{}
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
			vh.configFile = cf
			if b, err := os.ReadFile(cf); err == nil {
				vh.body = append(vh.body, parseOLS(string(b))...)
			}
		}
		byName[vh.name] = vh
		m.vhosts = append(m.vhosts, vh)
	}

	for _, n := range top {
		if !n.isBlk || !strings.EqualFold(n.key, "listener") {
			continue
		}
		secure := olsGet(n.block, "secure") == "1" || strings.HasSuffix(olsGet(n.block, "address"), ":443")
		for _, mp := range n.block {
			if mp.isBlk || !strings.EqualFold(mp.key, "map") {
				continue
			}
			name, list, _ := strings.Cut(mp.value, " ")
			vh := byName[name]
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

	for _, vh := range m.vhosts {
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
		local := map[string]string{}
		for _, n := range vh.body {
			switch {
			case n.isBlk && strings.EqualFold(n.key, "vhssl"):
				vh.tls = true
			case n.isBlk && strings.EqualFold(n.key, "rewrite"):
				vh.htaccess = vh.htaccess || olsGet(n.block, "autoLoadHtaccess") == "1"
			case isProxyProc(n):
				local[n.value] = olsGet(n.block, "address")
			}
		}

		used := map[string]string{} // backend -> path
		var order []string
		use := func(name, path string) {
			if _, ok := used[name]; !ok {
				used[name] = path
				order = append(order, name)
			}
		}
		for _, n := range vh.body {
			switch {
			case n.isBlk && strings.EqualFold(n.key, "context") &&
				strings.EqualFold(olsGet(n.block, "type"), "proxy"):
				if h := olsGet(n.block, "handler"); h != "" {
					use(h, n.value)
				}
			case n.isBlk && strings.EqualFold(n.key, "rewrite"):
				for _, mm := range proxyRule.FindAllStringSubmatch(olsGet(n.block, "rules"), -1) {
					use(mm[1], "/")
				}
			}
		}
		// CyberPanel's "Rewrite Rules" editor writes public_html/.htaccess.
		vh.docRoot = expand(olsGet(vh.body, "docRoot"))
		if vh.docRoot == "" && vh.root != "" {
			vh.docRoot = vh.root + "/public_html"
		}
		if vh.docRoot != "" {
			vh.htaccessFile = filepath.Join(vh.docRoot, ".htaccess")
			if b, err := os.ReadFile(vh.htaccessFile); err == nil {
				for _, mm := range proxyRule.FindAllStringSubmatch(string(b), -1) {
					use(mm[1], "/")
				}
			}
		}
		// A proxy backend defined inside a vhost is meant for that vhost even
		// if the rule using it lives somewhere we can't read.
		var localNames []string
		for name := range local {
			localNames = append(localNames, name)
		}
		slices.Sort(localNames)
		for _, name := range localNames {
			use(name, "/")
		}

		for _, name := range order {
			b := olsBackend{name: name, path: used[name]}
			switch addr, ok := local[name]; {
			case ok:
				b.addr, b.local = addr, true
			case global[name] != "":
				b.addr = global[name]
			default:
				b.addr = name // rewrite straight to host:port
			}
			vh.backends = append(vh.backends, b)
		}
	}
	return m, nil
}

func isProxyProc(n *olsNode) bool {
	return n.isBlk && strings.EqualFold(n.key, "extprocessor") &&
		strings.EqualFold(olsGet(n.block, "type"), "proxy")
}

// OLSRoutes parses httpd_config.conf, every vhost.conf it references and
// each vhost's .htaccess, and returns domain -> proxy backend routes.
func OLSRoutes(confPath string) ([]Route, error) {
	m, err := loadOLSModel(confPath)
	if err != nil {
		return nil, err
	}
	return m.routes(), nil
}

func (m *olsModel) routes() []Route {
	var routes []Route
	for _, vh := range m.vhosts {
		for _, b := range vh.backends {
			t, ok := parseOLSAddr(b.addr)
			if !ok {
				continue
			}
			for _, d := range vh.domains {
				routes = append(routes, Route{
					Domain: Domain{Host: d, Path: b.path, Source: "openlitespeed", TLS: vh.tls},
					UpHost: t.host, UpPort: t.port, Socket: t.socket,
				})
			}
		}
	}
	return routes
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
