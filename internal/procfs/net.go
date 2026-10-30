package procfs

import (
	"bytes"
	"encoding/hex"
	"net/netip"
	"strconv"
)

// Listener is a listening TCP socket.
type Listener struct {
	Addr string `json:"addr"`
	Port int    `json:"port"`
}

// TCPListeners maps socket inode to listening address for IPv4 and IPv6.
// The map is cleared and refilled so it can be reused between scans.
func (r *Reader) TCPListeners(dst map[uint64]Listener) error {
	clear(dst)
	var firstErr error
	for _, name := range [...]string{"/net/tcp", "/net/tcp6"} {
		b, err := r.read(Root + name)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		parseTCP(b, dst)
	}
	return firstErr
}

func parseTCP(b []byte, dst map[uint64]Listener) {
	lines := bytes.Split(b, []byte{'\n'})
	for _, line := range lines[1:] {
		fs := bytes.Fields(line)
		// sl local rem st tx:rx tr:when retrnsmt uid timeout inode
		if len(fs) < 10 || string(fs[3]) != "0A" { // 0A = TCP_LISTEN
			continue
		}
		local := fs[1]
		colon := bytes.IndexByte(local, ':')
		if colon < 0 {
			continue
		}
		port, err := strconv.ParseUint(string(local[colon+1:]), 16, 16)
		if err != nil {
			continue
		}
		addr, ok := decodeAddr(local[:colon])
		if !ok {
			continue
		}
		dst[atou(fs[9])] = Listener{Addr: addr, Port: int(port)}
	}
}

// decodeAddr turns the kernel's hex address (host byte order per 32-bit
// word, i.e. little-endian on x86/arm) into a printable IP.
func decodeAddr(h []byte) (string, bool) {
	raw := make([]byte, len(h)/2)
	if _, err := hex.Decode(raw, h); err != nil {
		return "", false
	}
	for i := 0; i+4 <= len(raw); i += 4 {
		raw[i], raw[i+1], raw[i+2], raw[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	ip, ok := netip.AddrFromSlice(raw)
	if !ok {
		return "", false
	}
	return ip.Unmap().String(), true
}

// UnixListeners maps socket inode to filesystem path for listening unix
// sockets (abstract sockets are skipped; reverse proxies can't target them).
func (r *Reader) UnixListeners(dst map[uint64]string) error {
	clear(dst)
	b, err := r.read(Root + "/net/unix")
	if err != nil {
		return err
	}
	lines := bytes.Split(b, []byte{'\n'})
	for _, line := range lines[1:] {
		// Num RefCount Protocol Flags Type St Inode Path
		fs := bytes.Fields(line)
		if len(fs) < 8 || fs[7][0] != '/' {
			continue
		}
		flags, err := strconv.ParseUint(string(fs[3]), 16, 32)
		if err != nil || flags&0x10000 == 0 { // __SO_ACCEPTCON
			continue
		}
		dst[atou(fs[6])] = string(fs[7])
	}
	return nil
}

// SocketInode extracts the inode from an fd link like "socket:[12345]".
func SocketInode(target string) (uint64, bool) {
	const p = "socket:["
	if len(target) < len(p)+2 || target[:len(p)] != p || target[len(target)-1] != ']' {
		return 0, false
	}
	n, err := strconv.ParseUint(target[len(p):len(target)-1], 10, 64)
	return n, err == nil
}
