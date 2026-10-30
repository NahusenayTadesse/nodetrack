package proxy

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Only plain host:port addresses ever get written into a config file.
var safeAddr = regexp.MustCompile(`^(127\.\d{1,3}\.\d{1,3}\.\d{1,3}|\[::1\]|localhost|\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):\d{1,5}$`)

const staticBackend = "nodetrack_app"

// OLSPoint points a vhost's proxy backend at addr (for a static site it adds
// a proxy in front of the files). Every file it touches is backed up first;
// the returned restore func puts them back.
func OLSPoint(confPath, vhost, backend, addr string) (restore func() error, summary string, err error) {
	if !safeAddr.MatchString(addr) {
		return nil, "", fmt.Errorf("refusing unsafe address %q", addr)
	}
	m, err := loadOLSModel(confPath)
	if err != nil {
		return nil, "", err
	}
	var vh *olsVhost
	for _, v := range m.vhosts {
		if v.name == vhost {
			vh = v
		}
	}
	if vh == nil {
		return nil, "", fmt.Errorf("no virtual host %q", vhost)
	}
	if vh.configFile == "" {
		return nil, "", errors.New("site config is inline in httpd_config.conf")
	}

	var backups backupSet
	if backend != "" {
		var b *olsBackend
		for i := range vh.backends {
			if vh.backends[i].name == backend {
				b = &vh.backends[i]
			}
		}
		if b == nil || !b.local {
			return nil, "", fmt.Errorf("backend %q is not defined in %s", backend, vh.configFile)
		}
		if err := backups.save(vh.configFile); err != nil {
			return nil, "", err
		}
		if err := setProcAddress(vh.configFile, backend, addr); err != nil {
			backups.restore()
			return nil, "", err
		}
		return backups.restore, fmt.Sprintf("%s: backend %s %s -> %s", vhost, backend, b.addr, addr), nil
	}

	// Static site: add a backend and a proxy rule in front of the files.
	if len(vh.backends) > 0 {
		return nil, "", errors.New("site already proxies; pick its backend instead")
	}
	if !vh.htaccess || vh.docRoot == "" {
		return nil, "", errors.New("site doesn't load .htaccess rewrite rules")
	}
	if err := backups.save(vh.configFile); err != nil {
		return nil, "", err
	}
	if err := backups.save(vh.htaccessFile); err != nil {
		backups.restore()
		return nil, "", err
	}
	block := fmt.Sprintf(`
extprocessor %s {
  type                    proxy
  address                 %s
  maxConns                100
  pcKeepAliveTimeout      60
  initTimeout             60
  retryTimeout            0
  respBuffer              0
}
`, staticBackend, addr)
	if err := appendFile(vh.configFile, block); err != nil {
		backups.restore()
		return nil, "", err
	}
	old, _ := os.ReadFile(vh.htaccessFile)
	rules := "# added by nodetrack\nRewriteEngine On\nRewriteCond %{REQUEST_URI} !^/\\.well-known/\n" +
		"RewriteRule ^(.*)$ HTTP://" + staticBackend + "/$1 [P,L]\n# end nodetrack\n"
	if err := os.WriteFile(vh.htaccessFile, append([]byte(rules), old...), 0o644); err != nil {
		backups.restore()
		return nil, "", err
	}
	if fi, err := os.Stat(vh.docRoot); err == nil {
		st := fi.Sys().(*syscall.Stat_t)
		os.Lchown(vh.htaccessFile, int(st.Uid), int(st.Gid))
	}
	return backups.restore, fmt.Sprintf("%s: added proxy -> %s in front of %s", vhost, addr, vh.docRoot), nil
}

var procStart = regexp.MustCompile(`^\s*extprocessor\s+(\S+)\s*\{\s*$`)
var addrLine = regexp.MustCompile(`^(\s*address\s+)\S+(.*)$`)

func setProcAddress(path, name, addr string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	in, done := false, false
	for i, l := range lines {
		if m := procStart.FindStringSubmatch(l); m != nil {
			in = m[1] == name
			continue
		}
		if !in {
			continue
		}
		if strings.TrimSpace(l) == "}" {
			in = false
			continue
		}
		if m := addrLine.FindStringSubmatch(l); m != nil {
			lines[i] = m[1] + addr + m[2]
			done = true
		}
	}
	if !done {
		return fmt.Errorf("no address line for %s in %s", name, path)
	}
	return writeKeepMode(path, []byte(strings.Join(lines, "\n")))
}

func writeKeepMode(path string, data []byte) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, fi.Mode().Perm())
}

func appendFile(path, s string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(s); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// backupSet copies files aside before they're edited.
type backupSet struct {
	items []struct {
		path, backup string
		existed      bool
	}
}

func (b *backupSet) save(path string) error {
	item := struct {
		path, backup string
		existed      bool
	}{path: path}
	if data, err := os.ReadFile(path); err == nil {
		fi, _ := os.Stat(path)
		item.existed = true
		item.backup = path + ".bak-nodetrack-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(item.backup, data, fi.Mode().Perm()); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	b.items = append(b.items, item)
	return nil
}

func (b *backupSet) restore() error {
	var errs []error
	for i := len(b.items) - 1; i >= 0; i-- {
		it := b.items[i]
		if !it.existed {
			if err := os.Remove(it.path); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
			continue
		}
		data, err := os.ReadFile(it.backup)
		if err == nil {
			err = writeKeepMode(it.path, data)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
