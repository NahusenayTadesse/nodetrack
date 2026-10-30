package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "server"), []byte("x"), 0o644)

	cases := []struct {
		runtime, cmd string
		app          bool
		script       string
	}{
		{"node", "node server.js", true, "server.js"},
		{"node", "/usr/bin/node --max-old-space-size=512 -r dotenv/config dist/index.js", true, "dist/index.js"},
		{"node", "node --env-file .env --enable-source-maps build/server.mjs", true, "build/server.mjs"},
		{"node", "node /srv/app/node_modules/.bin/next start -p 3000", true, "/srv/app/node_modules/.bin/next"},
		{"node", "next-server (v15.0.3)", true, ""},
		{"node", "npm start", false, ""},
		{"node", "npm install", false, ""},
		{"node", "node /usr/lib/node_modules/npm/bin/npm-cli.js run build", false, ""},
		{"node", "node /home/u/.cache/node/corepack/pnpm/9.1.0/bin/pnpm.cjs install", false, ""},
		{"node", "PM2 v5.4.2: God Daemon (/root/.pm2)", false, ""},
		{"node", "node /usr/lib/node_modules/pm2/bin/pm2 start app.js", false, ""},
		{"node", "node /srv/app/node_modules/.bin/nodemon server.js", false, ""},
		{"node", "node -e console.log(1)", false, ""},
		{"node", "node --run start", false, ""},
		{"node", "node", false, ""},
		{"node", "node --test", false, ""},
		{"bun", "bun server.ts", true, "server.ts"},
		{"bun", "bun --watch run src/index.ts", true, "src/index.ts"},
		{"bun", "bun run start", false, ""},
		{"bun", "bun start", false, ""},
		{"bun", "bun install", false, ""},
		{"bun", "bun add hono", false, ""},
		{"bun", "bun x prettier", false, ""},
		{"bun", "bunx prettier", false, ""},
		{"bun", "bun --smol server", true, "server"}, // extensionless file present in cwd
		{"bun", "bun", false, ""},
		{"node", "/root/.vscode-server/cli/servers/Stable-abc/server/node /root/.vscode-server/cli/servers/Stable-abc/server/out/server-main.js --port=0", false, ""},
		{"node", "/home/u/.cursor-server/bin/x/node --dns-result-order=ipv4first /home/u/.cursor-server/bin/x/out/bootstrap-fork --type=extensionHost", false, ""},
	}
	for _, c := range cases {
		app, script, reason := classify(c.runtime, strings.Fields(c.cmd), dir)
		if app != c.app || script != c.script {
			t.Errorf("%q: got app=%v script=%q (%s), want app=%v script=%q",
				c.cmd, app, script, reason, c.app, c.script)
		}
	}
}

func TestSystemdUnit(t *testing.T) {
	cases := map[string]string{
		"/system.slice/shop-api.service":                                                "shop-api.service",
		"/system.slice/pm2-root.service":                                                "pm2-root.service",
		"/user.slice/user-1000.slice/session-3.scope":                                   "",
		"/user.slice/user-1000.slice/user@1000.service/app.slice/app-ghostty-1.scope":   "",
		"/user.slice/user-1000.slice/user@1000.service/app.slice/dbus-:1.2-x@0.service": "",
		"/user.slice/user-1000.slice/user@1000.service/app.slice/my-bot.service":        "my-bot.service",
		"/": "",
	}
	for in, want := range cases {
		if got := systemdUnit(in); got != want {
			t.Errorf("systemdUnit(%q) = %q, want %q", in, got, want)
		}
	}
}
