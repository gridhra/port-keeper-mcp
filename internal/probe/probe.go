// Package probe answers "is anything listening on this loopback port, and who?"
// It never keeps a socket open: bind attempts are closed immediately.
package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Listening reports whether a process is listening on port on IPv4 or IPv6 loopback.
// It tries to bind with SO_REUSEADDR disabled (so a wildcard listener is detected on
// BSD-derived systems too) and, as a second opinion, tries to connect.
func Listening(port int) bool {
	for _, host := range []string{"127.0.0.1", "[::1]"} {
		if bindFails(host, port) {
			return true
		}
	}
	for _, host := range []string{"127.0.0.1", "[::1]"} {
		if connects(host, port) {
			return true
		}
	}
	return false
}

func bindFails(host string, port int) bool {
	lc := net.ListenConfig{Control: disableReuseAddr}
	ln, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)))
	if err != nil {
		// EADDRINUSE means someone is there. Anything else (IPv6 disabled,
		// permission) is treated as "not listening" rather than a false positive.
		return errors.Is(err, syscall.EADDRINUSE)
	}
	_ = ln.Close()
	return false
}

func connects(host string, port int) bool {
	d := net.Dialer{Timeout: 150 * time.Millisecond}
	c, err := d.Dial("tcp", net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// Owner describes the process found listening on a port.
type Owner struct {
	PID     int
	Command string
	Cwd     string // may be empty when not discoverable
}

// LookupOwner returns the listening process, using lsof when available.
// An empty Owner means "unknown".
func LookupOwner(ctx context.Context, port int) Owner {
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return Owner{}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, lsof, "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		return Owner{} // lsof exits 1 when nothing matches
	}
	var o Owner
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			if o.PID == 0 {
				o.PID, _ = strconv.Atoi(line[1:])
			}
		case 'c':
			if o.Command == "" {
				o.Command = line[1:]
			}
		}
	}
	if o.PID == 0 {
		return Owner{}
	}
	if out, err := exec.CommandContext(ctx, lsof, "-a", "-p", strconv.Itoa(o.PID), "-d", "cwd", "-Fn").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "n") {
				o.Cwd = line[1:]
				break
			}
		}
	}
	return o
}

// ListenerMap returns every loopback/any-address TCP listener as port -> Owner
// with one lsof call (plus one per distinct PID for the working directory).
// ok is false when lsof is unavailable; the map is then empty.
func ListenerMap(ctx context.Context) (map[int]Owner, bool) {
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, lsof, "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpcn").Output()
	if err != nil {
		return map[int]Owner{}, true
	}
	owners := map[int]Owner{}
	var cur Owner
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			cur = Owner{}
			cur.PID, _ = strconv.Atoi(line[1:])
		case 'c':
			cur.Command = line[1:]
		case 'n':
			i := strings.LastIndex(line, ":")
			if i < 0 {
				continue
			}
			if port, err := strconv.Atoi(line[i+1:]); err == nil {
				if _, seen := owners[port]; !seen {
					owners[port] = cur
				}
			}
		}
	}
	cwds := map[int]string{}
	for port, o := range owners {
		if o.PID == 0 {
			continue
		}
		cwd, done := cwds[o.PID]
		if !done {
			if out, err := exec.CommandContext(ctx, lsof, "-a", "-p", strconv.Itoa(o.PID), "-d", "cwd", "-Fn").Output(); err == nil {
				for _, line := range strings.Split(string(out), "\n") {
					if strings.HasPrefix(line, "n") {
						cwd = line[1:]
						break
					}
				}
			}
			cwds[o.PID] = cwd
		}
		o.Cwd = cwd
		owners[port] = o
	}
	return owners, true
}

// String formats an owner for humans.
func (o Owner) String() string {
	if o.PID == 0 {
		return "unknown process"
	}
	s := fmt.Sprintf("pid %d", o.PID)
	if o.Command != "" {
		s += " (" + o.Command + ")"
	}
	return s
}

// ListenersOfCommand returns the ports on which processes whose command name
// is name are listening. ok is false when lsof is unavailable.
func ListenersOfCommand(ctx context.Context, name string) (ports []int, ok bool) {
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, lsof, "-nP", "-iTCP", "-sTCP:LISTEN", "-a", "-c", name, "-Fn").Output()
	if err != nil {
		return nil, true // exit 1: nothing matched
	}
	seen := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}
		i := strings.LastIndex(line, ":")
		if i < 0 {
			continue
		}
		if p, err := strconv.Atoi(line[i+1:]); err == nil && !seen[p] {
			seen[p] = true
			ports = append(ports, p)
		}
	}
	return ports, true
}
