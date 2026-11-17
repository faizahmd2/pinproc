package inventory

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Host checks are the facts a senior engineer looks at first on a box they did
// not build — told from the running workloads' point of view (will they get
// memory, file handles, a sane clock, logs that survive a reboot?). Each check is
// a plain read of /proc, /sys or /etc and names the command that verifies it.
// They describe; they never change anything.

func (c *Collector) checks(ctx context.Context, s *Snapshot) []Check {
	var out []Check
	add := func(ch Check) {
		if ch.Name != "" {
			out = append(out, ch)
		}
	}
	add(c.checkOOM(s))
	add(c.checkSwap(s))
	add(c.checkFiles(s))
	add(checkServiceFDs(s))
	add(checkExposed(s))
	add(c.checkBacklog(s))
	add(c.checkConntrack())
	add(checkClock())
	add(c.checkJournal())
	if c.opt.Units {
		add(c.checkFailed(ctx))
	}
	add(checkRestarts(s))
	add(c.checkSSH())
	add(c.checkFirewall(s))
	add(c.checkUpdates())
	add(c.checkReboot())
	add(checkFilesystems(s))
	add(checkRootServices(s))
	add(checkContainers(s))
	return out
}

// gib prints a size the way df -h does (1024-based, one decimal).
func gib(v uint64) string {
	f := float64(v)
	for _, u := range []string{"B", "KiB", "MiB", "GiB", "TiB"} {
		if f < 1000 || u == "TiB" {
			if u == "B" {
				return fmt.Sprintf("%.0f B", f)
			}
			return fmt.Sprintf("%.1f %s", f, u)
		}
		f /= 1024
	}
	return ""
}

func (c *Collector) sysctl(name string) string {
	return readString(filepath.Join(c.opt.Proc, "sys", strings.ReplaceAll(name, ".", "/")))
}

func (c *Collector) checkOOM(s *Snapshot) Check {
	ch := Check{ID: "oom", Area: "memory", Name: "OOM kills since boot", Cmd: "journalctl -k -b -g 'Killed process'"}
	if s.Memory.OOMKills == 0 {
		ch.State, ch.Value = "ok", "none"
		return ch
	}
	ch.State, ch.Value = "warn", fmt.Sprintf("%d process(es) killed by the kernel OOM killer", s.Memory.OOMKills)
	ch.Hint = "a workload has hit the memory ceiling before; check the journal for which one"
	return ch
}

func (c *Collector) checkSwap(s *Snapshot) Check {
	ch := Check{ID: "swap", Area: "memory", Name: "Swap", Cmd: "swapon --show && sysctl vm.swappiness vm.overcommit_memory"}
	sw := c.sysctl("vm.swappiness")
	oc := map[string]string{"0": "heuristic", "1": "always", "2": "strict"}[c.sysctl("vm.overcommit_memory")]
	extra := ""
	if sw != "" {
		extra = " · swappiness " + sw
	}
	if oc != "" {
		extra += " · overcommit " + oc
	}
	if s.Memory.SwapTotal == 0 {
		ch.State, ch.Value = "info", "none"+extra
		ch.Hint = "without swap a short memory spike goes straight to the OOM killer"
		if s.Memory.UsedPct >= 85 {
			ch.State = "warn"
		}
		return ch
	}
	ch.State = "ok"
	ch.Value = fmt.Sprintf("%s, %s used%s", gib(s.Memory.SwapTotal), gib(s.Memory.SwapUsed), extra)
	if s.Memory.SwapOutBPS > 1<<20 {
		ch.State, ch.Hint = "warn", "actively swapping out right now"
	}
	return ch
}

func (c *Collector) checkFiles(s *Snapshot) Check {
	n := s.Net
	if n.FDMax == 0 {
		return Check{}
	}
	ch := Check{ID: "file-max", Area: "files", Name: "Open files (system)", State: "ok", Cmd: "cat /proc/sys/fs/file-nr"}
	if n.FDMax >= 1<<62 { // the kernel's "no limit" (LONG_MAX)
		ch.Value = fmt.Sprintf("%d open · no system-wide limit", n.FDUsed)
		return ch
	}
	p := pct(float64(n.FDUsed), float64(n.FDMax))
	ch.Value = fmt.Sprintf("%d of %d (%.1f%%)", n.FDUsed, n.FDMax, p)
	if p >= 80 {
		ch.State = "warn"
	}
	return ch
}

func checkServiceFDs(s *Snapshot) Check {
	ch := Check{ID: "nofile", Area: "files", Name: "Per-service open-file limits", Cmd: "grep 'open files' /proc/<pid>/limits"}
	var bad []string
	checked := 0
	for _, sv := range s.Services {
		if sv.FDLimit == 0 || sv.Kind == "kernel" {
			continue
		}
		checked++
		perProc := sv.FDs
		if sv.Procs > 1 {
			perProc = sv.FDs / sv.Procs
		}
		switch {
		case float64(perProc) >= 0.7*float64(sv.FDLimit):
			bad = append(bad, fmt.Sprintf("%s %d of %d", sv.Name, perProc, sv.FDLimit))
		case sv.FDLimit <= 1024 && (clients(sv) >= 64 || perProc >= 256):
			bad = append(bad, fmt.Sprintf("%s limit %d (low for a network service)", sv.Name, sv.FDLimit))
		}
	}
	if checked == 0 {
		return Check{}
	}
	if len(bad) == 0 {
		ch.State, ch.Value = "ok", fmt.Sprintf("%d services well below their nofile limit", checked)
		return ch
	}
	ch.State, ch.Value = "warn", strings.Join(bad, " · ")
	ch.Hint = "raise LimitNOFILE= in the unit (systemctl edit <unit>) before it starts refusing connections"
	return ch
}

func clients(sv Service) int {
	n := 0
	for _, l := range sv.Listen {
		n += l.Clients
	}
	return n
}

func checkExposed(s *Snapshot) Check {
	ch := Check{ID: "exposed", Area: "network", Name: "Listening on all/public interfaces", Cmd: "ss -tulpn"}
	var pub []string
	seen := map[string]bool{}
	for _, l := range s.Listeners {
		if !l.Public {
			continue
		}
		label := strings.TrimSuffix(l.Proto, "6") + "/" + strconv.Itoa(l.Port)
		if l.Service != "" {
			label += " " + l.Service
		}
		if !seen[label] {
			seen[label] = true
			pub = append(pub, label)
		}
	}
	if len(s.Listeners) == 0 {
		return Check{}
	}
	if len(pub) == 0 {
		ch.State, ch.Value = "ok", "nothing — every listener is loopback-only"
		return ch
	}
	ch.State = "info"
	if len(pub) > 12 {
		pub = append(pub[:12], fmt.Sprintf("+%d more", len(pub)-12))
	}
	ch.Value = strings.Join(pub, " · ")
	return ch
}

func (c *Collector) checkBacklog(s *Snapshot) Check {
	ch := Check{ID: "backlog", Area: "network", Name: "Accept-queue overflows", Cmd: "nstat -az TcpExtListenOverflows; sysctl net.core.somaxconn"}
	som := c.sysctl("net.core.somaxconn")
	if s.Net.ListenOverflows == 0 {
		ch.State, ch.Value = "ok", "none since boot"
		if som != "" {
			ch.Value += " · somaxconn " + som
		}
		return ch
	}
	ch.State = "warn"
	ch.Value = fmt.Sprintf("%d connections dropped since boot (somaxconn %s)", s.Net.ListenOverflows, som)
	ch.Hint = "a server accepted connections slower than they arrived; raise its backlog or find what blocks its accept loop"
	return ch
}

func (c *Collector) checkConntrack() Check {
	max := readUint(filepath.Join(c.opt.Proc, "sys/net/netfilter/nf_conntrack_max"))
	if max == 0 {
		return Check{}
	}
	cur := readUint(filepath.Join(c.opt.Proc, "sys/net/netfilter/nf_conntrack_count"))
	ch := Check{ID: "conntrack", Area: "network", Name: "Connection tracking table", State: "ok",
		Cmd: "sysctl net.netfilter.nf_conntrack_count net.netfilter.nf_conntrack_max"}
	p := pct(float64(cur), float64(max))
	ch.Value = fmt.Sprintf("%d of %d (%.0f%%)", cur, max, p)
	if p >= 80 {
		ch.State, ch.Hint = "warn", "new connections are dropped when the table is full"
	}
	return ch
}

func (c *Collector) checkJournal() Check {
	ch := Check{ID: "journal", Area: "logs", Name: "System journal", Cmd: "journalctl --disk-usage"}
	persistent := false
	var size int64
	if st, err := os.Stat("/var/log/journal"); err == nil && st.IsDir() {
		persistent = true
		_ = filepath.WalkDir("/var/log/journal", func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if info, err := d.Info(); err == nil {
					size += info.Size()
				}
			}
			return nil
		})
	}
	storage, maxUse := "", ""
	for _, f := range append([]string{filepath.Join(c.opt.Etc, "systemd/journald.conf")}, globSorted(filepath.Join(c.opt.Etc, "systemd/journald.conf.d/*.conf"))...) {
		kv := iniValues(f)
		if v := kv["Storage"]; v != "" {
			storage = v
		}
		if v := kv["SystemMaxUse"]; v != "" {
			maxUse = v
		}
	}
	if storage == "volatile" || storage == "none" {
		persistent = false
	}
	if _, err := os.Stat("/run/systemd/journal"); err != nil && !persistent {
		return Check{} // no journald on this host
	}
	if !persistent {
		ch.State, ch.Value = "warn", "volatile — logs are lost on reboot"
		ch.Hint = "mkdir -p /var/log/journal && systemctl restart systemd-journald to keep logs across reboots"
		return ch
	}
	ch.State = "ok"
	ch.Value = fmt.Sprintf("persistent · %s on disk", gib(uint64(size)))
	if maxUse != "" {
		ch.Value += " · capped at " + maxUse
	} else {
		ch.Value += " · default cap (10% of /var, max 4G)"
	}
	return ch
}

func (c *Collector) checkFailed(ctx context.Context) Check {
	names, ok := failedUnits(ctx)
	if !ok {
		return Check{}
	}
	ch := Check{ID: "failed", Area: "services", Name: "Failed systemd units", Cmd: "systemctl --failed"}
	if len(names) == 0 {
		ch.State, ch.Value = "ok", "none"
		return ch
	}
	ch.State, ch.Value = "warn", strings.Join(names, " · ")
	ch.Hint = "systemctl status <unit> shows why"
	return ch
}

func checkRestarts(s *Snapshot) Check {
	var bad []string
	seen := 0
	for _, sv := range s.Services {
		if sv.Systemd == nil {
			continue
		}
		seen++
		if sv.Systemd.Restarts > 0 {
			bad = append(bad, fmt.Sprintf("%s ×%d", sv.Unit, sv.Systemd.Restarts))
		}
	}
	if seen == 0 {
		return Check{}
	}
	ch := Check{ID: "restarts", Area: "services", Name: "Service restarts (since boot)", Cmd: "systemctl show -p NRestarts <unit>"}
	if len(bad) == 0 {
		ch.State, ch.Value = "ok", "no automatic restarts"
		return ch
	}
	ch.State, ch.Value = "warn", strings.Join(bad, " · ")
	ch.Hint = "a service that keeps restarting is crashing; journalctl -u <unit> shows the exit reason"
	return ch
}

func (c *Collector) checkSSH() Check {
	main := filepath.Join(c.opt.Etc, "ssh/sshd_config")
	if _, err := os.Stat(main); err != nil {
		return Check{}
	}
	v := sshdValues(main, 0)
	pw, root := v["passwordauthentication"], v["permitrootlogin"]
	if pw == "" {
		pw = "yes" // OpenSSH default
	}
	if root == "" {
		root = "prohibit-password"
	}
	ch := Check{ID: "ssh", Area: "security", Name: "SSH login", Cmd: "sshd -T | grep -Ei 'passwordauthentication|permitrootlogin'"}
	ch.Value = "password auth " + pw + " · root login " + root
	ch.State = "ok"
	if strings.EqualFold(pw, "yes") || strings.EqualFold(root, "yes") {
		ch.State = "warn"
		ch.Hint = "prefer keys only: PasswordAuthentication no, PermitRootLogin no (or prohibit-password)"
	}
	return ch
}

// sshdValues reads sshd_config the way sshd does: the first value of a keyword
// wins, Include is expanded in place, and parsing stops at the first Match block.
func sshdValues(path string, depth int) map[string]string {
	out := map[string]string{}
	if depth > 4 {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(strings.Replace(line, "=", " ", 1))
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		if key == "match" {
			break
		}
		if key == "include" {
			for _, pat := range fields[1:] {
				if !filepath.IsAbs(pat) {
					pat = filepath.Join(filepath.Dir(path), pat)
				}
				for _, inc := range globSorted(pat) {
					for k, v := range sshdValues(inc, depth+1) {
						if _, set := out[k]; !set {
							out[k] = v
						}
					}
				}
			}
			continue
		}
		if _, set := out[key]; !set {
			out[key] = fields[1]
		}
	}
	return out
}

func (c *Collector) checkFirewall(s *Snapshot) Check {
	var parts []string
	kv := iniValues(filepath.Join(c.opt.Etc, "ufw/ufw.conf"))
	if strings.EqualFold(kv["ENABLED"], "yes") {
		parts = append(parts, "ufw enabled")
	}
	has := map[string]bool{}
	for _, sv := range s.Services {
		has[sv.Name] = true
	}
	for _, d := range []string{"firewalld", "fail2ban", "crowdsec", "nftables"} {
		if has[d] {
			parts = append(parts, d+" running")
		}
	}
	ch := Check{ID: "firewall", Area: "security", Name: "Host firewall / brute-force guard", Cmd: "ufw status verbose || firewall-cmd --list-all || nft list ruleset"}
	if len(parts) == 0 {
		ch.State, ch.Value = "info", "none detected on the host (cloud security groups may still apply)"
		return ch
	}
	ch.State, ch.Value = "ok", strings.Join(parts, " · ")
	return ch
}

func (c *Collector) checkUpdates() Check {
	ch := Check{ID: "updates", Area: "updates", Name: "Automatic security updates"}
	if _, err := os.Stat(filepath.Join(c.opt.Etc, "apt")); err == nil {
		ch.Cmd = "apt-config dump | grep -i unattended-upgrade"
		val := ""
		for _, f := range globSorted(filepath.Join(c.opt.Etc, "apt/apt.conf.d/*")) {
			b := readFile(f)
			for _, line := range strings.Split(string(b), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "APT::Periodic::Unattended-Upgrade") {
					val = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "APT::Periodic::Unattended-Upgrade")), "\";")
				}
			}
		}
		if val != "" && val != "0" {
			ch.State, ch.Value = "ok", "unattended-upgrades enabled"
		} else {
			ch.State, ch.Value = "warn", "unattended-upgrades not enabled"
			ch.Hint = "apt install unattended-upgrades && dpkg-reconfigure -plow unattended-upgrades"
		}
		return ch
	}
	if _, err := os.Stat(filepath.Join(c.opt.Etc, "dnf")); err == nil {
		ch.Cmd = "systemctl list-timers 'dnf-automatic*'"
		if len(globSorted(filepath.Join(c.opt.Etc, "systemd/system/timers.target.wants/dnf-automatic*"))) > 0 {
			ch.State, ch.Value = "ok", "dnf-automatic timer enabled"
		} else {
			ch.State, ch.Value = "warn", "dnf-automatic not enabled"
			ch.Hint = "dnf install dnf-automatic && systemctl enable --now dnf-automatic-install.timer"
		}
		return ch
	}
	return Check{}
}

func (c *Collector) checkReboot() Check {
	ch := Check{ID: "reboot", Area: "updates", Name: "Reboot required", Cmd: "cat /var/run/reboot-required.pkgs"}
	if _, err := os.Stat("/var/run/reboot-required"); err != nil {
		if _, err := os.Stat(filepath.Join(c.opt.Etc, "apt")); err != nil {
			return Check{} // only Debian-family hosts record this
		}
		ch.State, ch.Value = "ok", "no"
		return ch
	}
	pkgs := strings.Fields(string(readFile("/var/run/reboot-required.pkgs")))
	ch.State, ch.Value = "warn", "yes"
	if len(pkgs) > 0 {
		if len(pkgs) > 5 {
			pkgs = append(pkgs[:5], "…")
		}
		ch.Value += " — updated: " + strings.Join(uniqueStrings(pkgs), ", ")
	}
	ch.Hint = "patched kernel/libraries are not running until the next reboot"
	return ch
}

func checkFilesystems(s *Snapshot) Check {
	ch := Check{ID: "fs", Area: "storage", Name: "Filesystems", Cmd: "df -h && df -i"}
	var parts []string
	worst := "ok"
	n := 0
	for _, d := range s.Disks {
		for _, m := range d.Mounts {
			n++
			p := fmt.Sprintf("%s %.0f%%", m.Path, m.UsedPct)
			if m.InodePct >= 85 {
				p += fmt.Sprintf(" (inodes %.0f%%)", m.InodePct)
			}
			if m.UsedPct >= 85 || m.InodePct >= 85 {
				worst = "warn"
			}
			parts = append(parts, p)
		}
	}
	if n == 0 {
		return Check{}
	}
	ch.State = worst
	if len(parts) > 8 {
		parts = append(parts[:8], "…")
	}
	ch.Value = strings.Join(parts, " · ")
	if n == 1 {
		ch.Value += " · data shares the root volume"
	}
	return ch
}

var rootOK = map[string]bool{"ssh": true, "sshd": true, "containerd": true, "dockerd": true, "docker": true, "systemd": true,
	"chronyd": true, "chrony": true, "cron": true, "crond": true, "snapd": true, "rsyslog": true, "kubelet": true, "multipathd": true}

func checkRootServices(s *Snapshot) Check {
	var names []string
	for _, sv := range s.Services {
		if sv.User != "root" || rootOK[sv.Name] || strings.HasPrefix(sv.Name, "systemd") || sv.Kind == "kernel" {
			continue
		}
		public := false
		for _, l := range sv.Listen {
			public = public || l.Public
		}
		if public {
			names = append(names, sv.Name)
		}
	}
	ch := Check{ID: "root", Area: "security", Name: "Network services running as root", Cmd: "ps -o user,pid,cmd -p <pid>"}
	if len(names) == 0 {
		ch.State, ch.Value = "ok", "none besides system daemons"
		return ch
	}
	ch.State, ch.Value = "info", strings.Join(names, " · ")
	ch.Hint = "run app servers as a dedicated user (User= in the unit) so a compromise is contained"
	return ch
}

func checkContainers(s *Snapshot) Check {
	n := 0
	rt := map[string]bool{}
	for _, sv := range s.Services {
		if sv.Kind == "container" {
			n++
		}
		switch sv.Name {
		case "docker", "containerd", "podman", "crio", "kubelet":
			rt[sv.Name] = true
		}
	}
	if n == 0 && len(rt) == 0 {
		return Check{}
	}
	var names []string
	for k := range rt {
		names = append(names, k)
	}
	sort.Strings(names)
	v := fmt.Sprintf("%d running container(s)", n)
	if len(names) > 0 {
		v = strings.Join(names, ", ") + " · " + v
	}
	return Check{ID: "containers", Area: "services", Name: "Container runtime", State: "info", Value: v, Cmd: "docker ps || crictl ps"}
}

func globSorted(pat string) []string {
	m, _ := filepath.Glob(pat)
	sort.Strings(m)
	return m
}

// iniValues reads KEY=VALUE lines (journald.conf, ufw.conf), ignoring sections.
func iniValues(path string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(readFile(path)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' || line[0] == '[' {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}
