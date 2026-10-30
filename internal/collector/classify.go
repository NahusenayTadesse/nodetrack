package collector

import (
	"os"
	"path/filepath"
	"strings"
)

// runtimeOf maps an executable path to "node", "bun" or "".
func runtimeOf(exe string) string {
	switch filepath.Base(exe) {
	case "node", "nodejs":
		return "node"
	case "bun":
		return "bun"
	}
	return ""
}

// Package managers and supervisors. They are never apps themselves; the
// processes they spawn are.
var wrapperArgv0 = map[string]bool{
	"npm": true, "npx": true, "yarn": true, "yarnpkg": true, "pnpm": true,
	"pnpx": true, "corepack": true, "bunx": true, "pm2": true, "PM2": true,
	"nodemon": true, "node-gyp": true,
}

var wrapperScripts = []string{
	"/npm/bin/", "npm-cli.js", "npx-cli.js",
	"/yarn/bin/", "/yarn.js", "/yarn.cjs",
	"/pnpm/bin/", "pnpm.cjs", "pnpm.mjs",
	"/corepack/",
	"/pm2/bin/", "/pm2/lib/",
	"/nodemon/", "/.bin/nodemon",
	"/tsx/dist/cli", "/.bin/tsx",
	"/ts-node-dev/", "/.bin/ts-node-dev",
	"/forever/", "/concurrently/", "/.bin/concurrently",
	"/npm-run-all", "/node-gyp/",
}

// Editor remote servers and language servers run on node and often listen
// on localhost, but they are tooling, not apps.
var toolingPaths = []string{
	"/.vscode-server", "/.vscodium-server", "/.cursor-server", "/.windsurf-server",
	"/.vscode/extensions/", "/.cursor/extensions/", "/.local/share/JetBrains/",
	"/typescript/lib/tsserver.js", "/typescript/lib/typingsInstaller.js",
}

// Node flags that consume the following argument.
var nodeValueFlags = map[string]bool{
	"-r": true, "--require": true, "--import": true, "--loader": true,
	"--experimental-loader": true, "--env-file": true, "--env-file-if-exists": true,
	"-C": true, "--conditions": true, "--title": true, "--input-type": true,
	"--watch-path": true, "--inspect-port": true, "--disable-warning": true,
}

var nodeNonAppFlags = map[string]bool{
	"-e": true, "--eval": true, "-p": true, "--print": true, "-c": true,
	"--check": true, "-i": true, "--interactive": true, "-v": true,
	"--version": true, "-h": true, "--help": true, "--test": true,
	"--run": true, // `node --run start` runs a package.json script
}

// Bun subcommands that are tooling, not long-running apps.
var bunToolCmds = map[string]bool{
	"install": true, "i": true, "add": true, "a": true, "remove": true, "rm": true,
	"update": true, "upgrade": true, "x": true, "create": true, "c": true,
	"init": true, "pm": true, "link": true, "unlink": true, "outdated": true,
	"publish": true, "patch": true, "patch-commit": true, "build": true,
	"test": true, "repl": true, "audit": true, "info": true, "why": true,
	"completions": true, "help": true, "exec": true, "feedback": true,
}

var bunValueFlags = map[string]bool{
	"-r": true, "--preload": true, "--env-file": true, "--cwd": true,
	"-c": true, "--config": true, "--tsconfig-override": true, "-d": true,
	"--define": true, "-l": true, "--loader": true, "--conditions": true,
	"--main-fields": true, "--extension-order": true, "--port": true,
}

// classify decides whether a node/bun process is an app. It returns the
// entry script ("" when the process rewrote its title) or the reason it is
// not an app.
func classify(runtime string, argv []string, cwd string) (app bool, script, reason string) {
	if len(argv) == 0 {
		return false, "", "no cmdline"
	}
	joined := strings.Join(argv, " ")
	for _, t := range toolingPaths {
		if strings.Contains(joined, t) {
			return false, "", "editor tooling"
		}
	}
	argv0 := filepath.Base(argv[0])
	if wrapperArgv0[argv0] || strings.Contains(joined, "God Daemon") {
		return false, "", "wrapper " + argv0
	}
	if runtimeOf(argv[0]) == "" && argv0 != "bun" && argv0 != "node" {
		// Title was rewritten (process.title / pm2 / next-server): it is an
		// app; its command line is just the title.
		return true, "", ""
	}
	if runtime == "bun" {
		return classifyBun(argv, cwd)
	}
	return classifyNode(argv)
}

func classifyNode(argv []string) (bool, string, string) {
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		switch {
		case nodeNonAppFlags[a]:
			return false, "", "node " + a
		case a == "--":
			continue
		case strings.HasPrefix(a, "-"):
			if nodeValueFlags[a] {
				i++
			}
		default:
			for _, w := range wrapperScripts {
				if strings.Contains(a, w) {
					return false, "", "wrapper " + filepath.Base(a)
				}
			}
			return true, a, ""
		}
	}
	return false, "", "node repl"
}

func classifyBun(argv []string, cwd string) (bool, string, string) {
	var rest []string // non-flag args
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		if strings.HasPrefix(a, "-") {
			if a == "--cwd" && i+1 < len(argv) {
				cwd = argv[i+1]
			}
			if v, ok := strings.CutPrefix(a, "--cwd="); ok {
				cwd = v
			}
			if bunValueFlags[a] {
				i++
			}
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) == 0 {
		return false, "", "bun without script"
	}
	sub := rest[0]
	if bunToolCmds[sub] {
		return false, "", "bun " + sub
	}
	if sub == "run" {
		if len(rest) < 2 {
			return false, "", "bun run"
		}
		sub = rest[1]
	}
	if looksLikeFile(sub, cwd) {
		return true, sub, ""
	}
	// `bun start` / `bun run dev`: a package.json script runner that spawns
	// the real app as a child.
	return false, "", "bun run " + sub
}

var scriptExts = []string{".js", ".mjs", ".cjs", ".ts", ".mts", ".cts", ".tsx", ".jsx"}

func looksLikeFile(s, cwd string) bool {
	if strings.ContainsRune(s, '/') {
		return true
	}
	for _, e := range scriptExts {
		if strings.HasSuffix(s, e) {
			return true
		}
	}
	if !filepath.IsAbs(s) {
		s = filepath.Join(cwd, s)
	}
	fi, err := os.Stat(s)
	return err == nil && fi.Mode().IsRegular()
}

// systemdUnit returns the service owning a cgroup path, or "" for login
// sessions, desktop app scopes and the per-user manager itself.
func systemdUnit(cgroup string) string {
	parts := strings.Split(cgroup, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		s := parts[i]
		if strings.HasSuffix(s, ".scope") {
			return ""
		}
		if strings.HasSuffix(s, ".service") {
			for _, p := range [...]string{"user@", "app-", "dbus-", "session-"} {
				if strings.HasPrefix(s, p) {
					return ""
				}
			}
			return s
		}
	}
	return ""
}

var containerMarkers = []string{"docker", "containerd", "kubepods", "libpod", "lxc", "machine.slice"}

func inContainerCgroup(cgroup string) bool {
	for _, m := range containerMarkers {
		if strings.Contains(cgroup, m) {
			return true
		}
	}
	return false
}
