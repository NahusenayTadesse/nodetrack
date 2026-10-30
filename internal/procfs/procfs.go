// Package procfs reads the small subset of /proc that nodetrack needs.
//
// A Reader owns one scratch buffer that is reused for every file it reads,
// so a collection tick allocates almost nothing. A Reader is not safe for
// concurrent use; the collector owns exactly one.
package procfs

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
)

// Root is the procfs mount point. Overridable for tests.
var Root = "/proc"

// ClockTicks is USER_HZ. It is 100 on every mainstream Linux build.
const ClockTicks = 100

var PageSize = int64(os.Getpagesize())

var errFormat = errors.New("procfs: unexpected format")

type Reader struct {
	buf []byte
}

func NewReader() *Reader { return &Reader{buf: make([]byte, 0, 8192)} }

func (r *Reader) read(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := r.buf[:0]
	for {
		if len(b) == cap(b) {
			b = append(b, 0)[:len(b)]
		}
		n, err := f.Read(b[len(b):cap(b)])
		b = b[:len(b)+n]
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	r.buf = b
	return b, nil
}

func pidPath(pid int, name string) string {
	return Root + "/" + strconv.Itoa(pid) + "/" + name
}

// PIDs lists the numeric entries of /proc.
func PIDs(dst []int) ([]int, error) {
	f, err := os.Open(Root)
	if err != nil {
		return dst, err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return dst, err
	}
	for _, n := range names {
		if n[0] < '0' || n[0] > '9' {
			continue
		}
		if pid, err := strconv.Atoi(n); err == nil {
			dst = append(dst, pid)
		}
	}
	return dst, nil
}

// Stat is the parsed subset of /proc/<pid>/stat.
type Stat struct {
	PID        int
	PPID       int
	Comm       string
	State      byte
	UTime      uint64 // clock ticks
	STime      uint64 // clock ticks
	NumThreads int
	StartTime  uint64 // clock ticks since boot
	RSS        int64  // bytes
	Processor  int    // CPU it last ran on
}

func (r *Reader) Stat(pid int) (Stat, error) {
	st := Stat{PID: pid}
	b, err := r.read(pidPath(pid, "stat"))
	if err != nil {
		return st, err
	}
	open := bytes.IndexByte(b, '(')
	end := bytes.LastIndexByte(b, ')')
	if open < 0 || end < open || end+2 >= len(b) {
		return st, errFormat
	}
	st.Comm = string(b[open+1 : end])
	rest := b[end+2:]
	// Field numbers below are proc(5) numbers minus 3 (state is field 3).
	for field := 0; len(rest) > 0; field++ {
		var f []byte
		if j := bytes.IndexByte(rest, ' '); j >= 0 {
			f, rest = rest[:j], rest[j+1:]
		} else {
			f, rest = bytes.TrimSpace(rest), nil
		}
		switch field {
		case 0:
			if len(f) > 0 {
				st.State = f[0]
			}
		case 1:
			st.PPID = int(atou(f))
		case 11:
			st.UTime = atou(f)
		case 12:
			st.STime = atou(f)
		case 17:
			st.NumThreads = int(atou(f))
		case 19:
			st.StartTime = atou(f)
		case 21:
			st.RSS = int64(atou(f)) * PageSize
		case 36:
			st.Processor = int(atou(f))
			return st, nil
		}
	}
	return st, nil
}

// Exe returns the target of /proc/<pid>/exe with any " (deleted)" suffix
// stripped (that happens after the runtime binary is upgraded in place).
func Exe(pid int) (string, error) {
	s, err := os.Readlink(pidPath(pid, "exe"))
	return strings.TrimSuffix(s, " (deleted)"), err
}

func Cwd(pid int) (string, error) { return os.Readlink(pidPath(pid, "cwd")) }

// PIDNamespace returns the pid namespace identity, e.g. "pid:[4026531836]".
func PIDNamespace(pid int) (string, error) { return os.Readlink(pidPath(pid, "ns/pid")) }

func SelfPIDNamespace() string {
	s, _ := os.Readlink(Root + "/self/ns/pid")
	return s
}

// Cmdline returns argv. When a program rewrote its title (process.title,
// pm2, npm) the kernel hands back a single space-separated string, which is
// split here so callers always see argv-like fields.
func (r *Reader) Cmdline(pid int) ([]string, error) {
	b, err := r.read(pidPath(pid, "cmdline"))
	if err != nil {
		return nil, err
	}
	b = bytes.TrimRight(b, "\x00 ")
	if len(b) == 0 {
		return nil, nil
	}
	var args []string
	if bytes.IndexByte(b, 0) < 0 {
		args = strings.Fields(string(b))
	} else {
		args = strings.Split(string(b), "\x00")
	}
	return args, nil
}

// Cgroup returns the unified (v2) cgroup path, or the first v1 path.
func (r *Reader) Cgroup(pid int) (string, error) {
	b, err := r.read(pidPath(pid, "cgroup"))
	if err != nil {
		return "", err
	}
	var first string
	for line := range bytes.SplitSeq(b, []byte{'\n'}) {
		parts := bytes.SplitN(line, []byte{':'}, 3)
		if len(parts) != 3 {
			continue
		}
		if bytes.Equal(parts[0], []byte("0")) {
			return string(parts[2]), nil
		}
		if first == "" {
			first = string(parts[2])
		}
	}
	return first, nil
}

// Environ returns only the requested variables. Everything else in the
// environment (which routinely holds secrets) is never copied out of the
// scratch buffer.
func (r *Reader) Environ(pid int, keys ...string) (map[string]string, error) {
	b, err := r.read(pidPath(pid, "environ"))
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(keys))
	for kv := range bytes.SplitSeq(b, []byte{0}) {
		eq := bytes.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		for _, k := range keys {
			if string(kv[:eq]) == k {
				out[k] = string(kv[eq+1:])
			}
		}
	}
	return out, nil
}

// PSS returns proportional set size in bytes from smaps_rollup.
func (r *Reader) PSS(pid int) (int64, error) {
	b, err := r.read(pidPath(pid, "smaps_rollup"))
	if err != nil {
		return 0, err
	}
	if v, ok := field(b, "Pss:"); ok {
		return int64(atou(v)) * 1024, nil
	}
	return 0, errFormat
}

// IO returns bytes read from and written to storage.
func (r *Reader) IO(pid int) (read, write uint64, err error) {
	b, err := r.read(pidPath(pid, "io"))
	if err != nil {
		return 0, 0, err
	}
	rv, _ := field(b, "read_bytes:")
	wv, _ := field(b, "write_bytes:")
	return atou(rv), atou(wv), nil
}

// CPUAffinity returns Cpus_allowed_list from status, e.g. "0-3".
func (r *Reader) CPUAffinity(pid int) string {
	b, err := r.read(pidPath(pid, "status"))
	if err != nil {
		return ""
	}
	v, _ := field(b, "Cpus_allowed_list:")
	return string(v)
}

// FDs calls fn with the target of every open file descriptor when
// withTargets is set, and returns the descriptor count either way.
func FDs(pid int, withTargets bool, fn func(target string)) (int, error) {
	dir := pidPath(pid, "fd")
	f, err := os.Open(dir)
	if err != nil {
		return 0, err
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return 0, err
	}
	if withTargets {
		for _, n := range names {
			if t, err := os.Readlink(dir + "/" + n); err == nil {
				fn(t)
			}
		}
	}
	return len(names), nil
}

// field finds "key  value unit" lines (status, smaps_rollup, io, meminfo)
// and returns the first token after the key.
func field(b []byte, key string) ([]byte, bool) {
	i := bytes.Index(b, []byte(key))
	if i < 0 {
		return nil, false
	}
	v := b[i+len(key):]
	if j := bytes.IndexByte(v, '\n'); j >= 0 {
		v = v[:j]
	}
	v = bytes.TrimSpace(v)
	if j := bytes.IndexByte(v, ' '); j >= 0 {
		v = v[:j]
	}
	return v, true
}

func atou(b []byte) uint64 {
	var n uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}
