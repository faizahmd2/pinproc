package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/procfs"
	"github.com/faizahmd2/pinproc/internal/source"
)

// Resolver resolves Linux process and machine identity from read-only sources.
type dockerContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	Labels map[string]string `json:"Labels"`
}

type Resolver struct {
	src              source.Source
	dockerOnce       sync.Once
	dockerContainers map[string]dockerContainer
}

// New returns an identity resolver bound to a source.
func New(src source.Source) *Resolver { return &Resolver{src: src} }

// ResolveAll enriches observed process entities with proved identity.
func (r *Resolver) ResolveAll(ctx context.Context, inv *contract.Investigation) error {
	if r == nil || r.src == nil || inv == nil {
		return nil
	}
	for i := range inv.Evidence {
		if inv.Evidence[i].Entity.Kind != contract.EntityProcess {
			continue
		}
		pid := strings.TrimPrefix(inv.Evidence[i].Entity.ID, "pid:")
		if _, err := strconv.Atoi(pid); err != nil {
			continue
		}
		svc, err := r.Resolve(ctx, pid)
		if err != nil {
			inv.IdentityGaps++
			inv.Limitations = append(inv.Limitations, fmt.Sprintf("identity unavailable for pid %s: %v", pid, err))
			continue
		}
		inv.Evidence[i].Entity.Display = display(svc, pid)
		inv.Evidence[i].Entity.Service = &svc
	}
	return nil
}

// Resolve resolves one process by deterministic provenance order.
func (r *Resolver) Resolve(ctx context.Context, pid string) (contract.Service, error) {
	reads := []source.Read{
		{Key: "cgroup", Path: "/proc/" + pid + "/cgroup", Kind: source.ReadFile, Optional: true},
		{Key: "cmdline", Path: "/proc/" + pid + "/cmdline", Kind: source.ReadFile, Optional: true},
		{Key: "exe", Path: "/proc/" + pid + "/exe", Kind: source.ReadLink, Optional: true},
		{Key: "cwd", Path: "/proc/" + pid + "/cwd", Kind: source.ReadLink, Optional: true},
		{Key: "status", Path: "/proc/" + pid + "/status", Kind: source.ReadFile, Optional: true},
		{Key: "stat", Path: "/proc/" + pid + "/stat", Kind: source.ReadFile, Optional: true},
		{Key: "host_stat", Path: "/proc/stat", Kind: source.ReadFile, Optional: true},
		{Key: "passwd", Path: "/etc/passwd", Kind: source.ReadFile, Optional: true},
		{Key: "tcp", Path: "/proc/net/tcp", Kind: source.ReadFile, Optional: true},
		{Key: "tcp6", Path: "/proc/net/tcp6", Kind: source.ReadFile, Optional: true},
		{Key: "fd", Path: "/proc/" + pid + "/fd/*", Kind: source.ReadGlobLinks, MaxBytes: 4096, Optional: true},
	}
	snap, err := r.src.Snapshot(ctx, reads)
	if err != nil {
		return contract.Service{}, err
	}
	paths := procfs.ParseCgroup(first(snap, "cgroup"))
	svc := contract.Service{CgroupPath: firstPath(paths)}
	if unit := systemdUnit(paths); unit != "" {
		svc.Unit = unit
		svc.Name = strings.TrimSuffix(unit, ".service")
		svc.NameSource = contract.ProvenanceSystemd
	}
	if runtime, id := containerID(paths); id != "" {
		svc.Container = &contract.ContainerRef{Runtime: runtime, ID: id}
		if r.src.Name() != "replay" && !strings.HasPrefix(r.src.Name(), "replay:") {
			if info, ok := r.dockerInfo(ctx, id); ok {
				if len(info.Names) > 0 {
					svc.Container.Name = strings.TrimPrefix(info.Names[0], "/")
				}
				svc.Container.Image = info.Image
				if info.Labels != nil {
					svc.Container.PodName = info.Labels["io.kubernetes.pod.name"]
					svc.Container.Namespace = info.Labels["io.kubernetes.namespace"]
				}
			}
		}
		if svc.Name == "" {
			svc.Name = runtime + ":" + shortID(id)
			svc.NameSource = contract.ProvenanceDocker
		}
	}
	if b := first(snap, "exe"); len(b) > 0 {
		svc.Exe = string(b)
	}
	if svc.Exe != "" && svc.Name == "" {
		svc.Name = filepath.Base(svc.Exe)
		svc.NameSource = contract.ProvenanceExe
	}
	if b := first(snap, "cmdline"); len(b) > 0 {
		svc.Cmdline = procfs.ParseCmdline(b)
	}
	if svc.Name == "" && len(svc.Cmdline) > 0 {
		svc.Name = filepath.Base(svc.Cmdline[0])
		svc.NameSource = contract.ProvenanceCmdline
	}
	if b := first(snap, "cwd"); len(b) > 0 {
		svc.Cwd = string(b)
	}
	if b := first(snap, "status"); len(b) > 0 {
		if st, e := procfs.ParsePidStatus(b); e == nil {
			uid := strconv.FormatUint(st.Uid[0], 10)
			svc.User = uid
			if name := passwdUser(first(snap, "passwd"), uid); name != "" {
				svc.User = name
			}
		}
	}
	if b := first(snap, "stat"); len(b) > 0 {
		if st, e := procfs.ParsePidStat(b); e == nil {
			if host := first(snap, "host_stat"); len(host) > 0 {
				if hs, he := procfs.ParseStat(host); he == nil {
					svc.StartedAt = time.Unix(hs.BootTime, 0).Add(procfs.TicksToDuration(st.Starttime))
				}
			}
			if svc.Name == "" {
				svc.Name = st.Comm
				svc.NameSource = contract.ProvenanceUnknown
			}
		}
	}
	svc.ListenPorts = listenerPorts(snap.Reads["fd"], first(snap, "tcp"), first(snap, "tcp6"))
	svc.LogPaths, svc.ConfigPaths = inferPaths(svc.Cmdline, svc.Cwd, svc.Exe)
	if svc.Name == "" {
		svc.Name = "pid:" + pid
		svc.NameSource = contract.ProvenanceUnknown
	}
	return svc, nil
}

// ResolveMachine resolves top-level host identity.
func (r *Resolver) ResolveMachine(ctx context.Context) (contract.MachineIdentity, error) {
	reads := []source.Read{
		{Key: "hostname", Path: "/etc/hostname", Kind: source.ReadFile, Optional: true},
		{Key: "machine_id", Path: "/etc/machine-id", Kind: source.ReadFile, Optional: true},
		{Key: "os_release", Path: "/etc/os-release", Kind: source.ReadFile, Optional: true},
		{Key: "version", Path: "/proc/version", Kind: source.ReadFile, Optional: true},
		{Key: "uptime", Path: "/proc/uptime", Kind: source.ReadFile, Optional: true},
		{Key: "stat", Path: "/proc/stat", Kind: source.ReadFile, Optional: true},
		{Key: "meminfo", Path: "/proc/meminfo", Kind: source.ReadFile, Optional: true},
	}
	snap, e := r.src.Snapshot(ctx, reads)
	if e != nil {
		return contract.MachineIdentity{}, e
	}
	m := contract.MachineIdentity{
		Hostname:     strings.TrimSpace(string(first(snap, "hostname"))),
		MachineID:    strings.TrimSpace(string(first(snap, "machine_id"))),
		Kernel:       strings.TrimSpace(string(first(snap, "version"))),
		Architecture: runtime.GOARCH,
	}
	if r.src.Name() == "local" {
		m.PrimaryIP = primaryIPv4()
	}
	for _, line := range strings.Split(string(first(snap, "os_release")), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			m.OS = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			break
		}
	}
	if m.OS == "" {
		for _, line := range strings.Split(string(first(snap, "os_release")), "\n") {
			if strings.HasPrefix(line, "ID=") {
				m.OS = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
				break
			}
		}
	}
	if p, e := procfs.ParseMemInfo(first(snap, "meminfo")); e == nil {
		m.MemTotal = p.MemTotal
	}
	if st, e := procfs.ParseStat(first(snap, "stat")); e == nil {
		m.CPUs = len(st.PerCore)
		if m.CPUs < 1 {
			m.CPUs = 1
		}
	}
	if f := strings.Fields(string(first(snap, "uptime"))); len(f) > 0 {
		sec, _ := strconv.ParseFloat(f[0], 64)
		m.Uptime = time.Duration(sec * float64(time.Second))
	}
	return m, nil
}

func first(s source.Snapshot, key string) []byte {
	r := s.Reads[key]
	if len(r) == 0 {
		return nil
	}
	return r[0].Data
}
func firstPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[len(paths)-1]
}
func systemdUnit(paths []string) string {
	for _, p := range paths {
		b := filepath.Base(strings.TrimSuffix(p, "/"))
		if strings.HasSuffix(b, ".service") {
			return b
		}
	}
	return ""
}
func shortID(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
func (r *Resolver) dockerInfo(ctx context.Context, id string) (dockerContainer, bool) {
	r.dockerOnce.Do(func() {
		r.dockerContainers = map[string]dockerContainer{}
		dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", "/var/run/docker.sock")
		}}
		client := &http.Client{Transport: transport, Timeout: 500 * time.Millisecond}
		defer transport.CloseIdleConnections()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/v1.41/version", nil)
		if err != nil {
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/v1.41/containers/json?all=0", nil)
		if err != nil {
			return
		}
		resp, err = client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return
		}
		var list []dockerContainer
		if json.NewDecoder(resp.Body).Decode(&list) != nil {
			return
		}
		for _, item := range list {
			r.dockerContainers[item.ID] = item
		}
	})
	for full, info := range r.dockerContainers {
		if strings.HasPrefix(full, id) || strings.HasPrefix(id, full) {
			return info, true
		}
	}
	return dockerContainer{}, false
}

func display(s contract.Service, pid string) string {
	if s.Name == "" {
		return "pid " + pid
	}
	if s.Unit != "" {
		return s.Name + " (systemd " + s.Unit + ", pid " + pid + ")"
	}
	if s.Container != nil {
		return s.Name + " (container " + shortID(s.Container.ID) + ", pid " + pid + ")"
	}
	return s.Name + " (pid " + pid + ")"
}
func containerID(paths []string) (string, string) {
	for _, p := range paths {
		for _, prefix := range []string{"docker-", "cri-containerd-", "crio-"} {
			i := strings.LastIndex(p, prefix)
			if i < 0 {
				continue
			}
			rest := strings.TrimSuffix(p[i+len(prefix):], ".scope")
			if j := strings.IndexByte(rest, '/'); j >= 0 {
				rest = rest[:j]
			}
			if len(rest) >= 8 {
				rt := "docker"
				if prefix == "cri-containerd-" {
					rt = "containerd"
				}
				if prefix == "crio-" {
					rt = "cri-o"
				}
				return rt, rest
			}
		}
	}
	return "", ""
}

func passwdUser(data []byte, uid string) string {
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Split(line, ":")
		if len(f) >= 3 && f[2] == uid {
			return f[0]
		}
	}
	return ""
}

func listenerPorts(fdRaw []source.Raw, tcp, tcp6 []byte) []contract.Port {
	owned := map[string]bool{}
	for _, r := range fdRaw {
		v := string(r.Data)
		if strings.HasPrefix(v, "socket:[") {
			owned[strings.TrimSuffix(strings.TrimPrefix(v, "socket:["), "]")] = true
		}
	}
	var out []contract.Port
	for _, data := range [][]byte{tcp, tcp6} {
		for _, line := range strings.Split(string(data), "\n")[1:] {
			f := strings.Fields(line)
			if len(f) < 10 || f[3] != "0A" {
				continue
			}
			parts := strings.Split(f[1], ":")
			if len(parts) != 2 || !owned[f[9]] {
				continue
			}
			p, err := strconv.ParseInt(parts[1], 16, 32)
			if err != nil {
				continue
			}
			out = append(out, contract.Port{Proto: "tcp", Addr: "0.0.0.0", Port: int(p)})
		}
	}
	return out
}

func inferPaths(cmd []string, cwd, exe string) ([]string, []string) {
	seenLog, seenCfg := map[string]bool{}, map[string]bool{}
	var logs, cfgs []string
	add := func(v string, log bool) {
		if !strings.HasPrefix(v, "/") {
			return
		}
		if log {
			if !seenLog[v] {
				seenLog[v] = true
				logs = append(logs, v)
			}
		} else if !seenCfg[v] {
			seenCfg[v] = true
			cfgs = append(cfgs, v)
		}
	}
	for _, arg := range append(append([]string{}, cmd...), cwd, exe) {
		l := strings.ToLower(arg)
		if strings.Contains(l, "log") || strings.HasSuffix(l, ".log") {
			add(arg, true)
		}
		if strings.HasSuffix(l, ".yaml") || strings.HasSuffix(l, ".yml") || strings.HasSuffix(l, ".json") || strings.HasSuffix(l, ".toml") || strings.Contains(l, "config") {
			add(arg, false)
		}
	}
	return logs, cfgs
}

func primaryIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			default:
				continue
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}
