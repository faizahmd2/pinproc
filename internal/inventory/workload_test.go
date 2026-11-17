package inventory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHexAddr(t *testing.T) {
	cases := map[string]string{
		"0100007F":                         "127.0.0.1",
		"00000000":                         "0.0.0.0",
		"0500000A":                         "10.0.0.5",
		"00000000000000000000000001000000": "::1",
		"0000000000000000FFFF00000500000A": "10.0.0.5", // v4-mapped
		"00000000000000000000000000000000": "::",
	}
	for in, want := range cases {
		if got := hexAddr(in); got != want {
			t.Errorf("hexAddr(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestParseUnitShow(t *testing.T) {
	out := []byte(`Id=api.service
Description=Checkout API
FragmentPath=/etc/systemd/system/api.service
DropInPaths=/etc/systemd/system/api.service.d/override.conf
Type=simple
Restart=on-failure
NRestarts=4
ActiveEnterTimestampMonotonic=5000000
ActiveState=active
SubState=running
User=app
WorkingDirectory=/srv/api
Requires=system.slice sysinit.target postgresql.service
Wants=network-online.target redis-server.service
BindsTo=
After=basic.target network-online.target systemd-journald.socket postgresql.service -.mount srv.mount
MemoryMax=2147483648
CPUQuotaPerSecUSec=1.500000s
LimitNOFILESoft=1024

Id=nginx.service
ActiveState=active
SubState=running
MemoryMax=infinity
CPUQuotaPerSecUSec=infinity
`)
	boot := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).Unix()
	m := parseUnitShow(out, boot)
	u := m["api.service"]
	if u == nil || u.Restarts != 4 || u.State != "active (running)" || u.User != "app" || u.MemoryMax != "2048 MiB" || u.CPUQuota != "150%" || u.LimitNOFILE != "1024" {
		t.Fatalf("api = %+v", u)
	}
	if !u.ActiveSince.Equal(time.Unix(boot+5, 0).UTC()) {
		t.Fatalf("active since = %v", u.ActiveSince)
	}
	want := "postgresql.service network-online.target redis-server.service srv.mount"
	if got := join(u.DependsOn); got != want {
		t.Fatalf("depends = %q, want %q", got, want)
	}
	if n := m["nginx.service"]; n == nil || n.MemoryMax != "" || n.CPUQuota != "" {
		t.Fatalf("nginx = %+v", n)
	}
}

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += " "
		}
		out += v
	}
	return out
}

func TestSocketsAttributeListenersClientsAndPeers(t *testing.T) {
	h := newFakeHost(t)
	hdr := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	h.write("proc/net/tcp", hdr+
		"   0: 0100007F:1538 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1001 1 0 100 0 0 10 0\n"+
		"   1: 0100007F:1538 0100007F:9C40 01 00000000:00000000 00:00000000 00000000  1000        0 1002 1 0 100 0 0 10 0\n"+
		"   2: 0100007F:9C40 0100007F:1538 01 00000000:00000000 00:00000000 00000000  1000        0 2001 1 0 100 0 0 10 0\n"+
		"   3: 0200000A:9C41 0900000A:18EB 01 00000000:00000000 00:00000000 00000000  1000        0 2002 1 0 100 0 0 10 0\n"+
		"   4: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 3001 1 0 100 0 0 10 0\n")
	for pid, inodes := range map[string][]string{"100": {"1001", "1002"}, "200": {"2001", "2002"}} {
		for i, ino := range inodes {
			p := filepath.Join(h.proc, pid, "fd", itoa(uint64(3+i)))
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.Symlink("socket:["+ino+"]", p); err != nil {
				t.Fatal(err)
			}
		}
	}
	c := New(Options{Proc: h.proc, Sys: h.sys, Etc: filepath.Join(filepath.Dir(h.proc), "etc"), TopProcs: 10, Enrich: true, Ports: true, AllSockets: true})
	c.users = map[uint64]string{0: "root", 1000: "app"}
	a := c.take(nil)
	b := c.take(a)
	b.at = a.at.Add(time.Second)
	s := c.build(context.Background(), a, b)

	svc := map[string]Service{}
	for _, sv := range s.Services {
		svc[sv.Key] = sv
	}
	pg := svc["unit:postgresql@16.service"]
	if len(pg.Listen) != 1 || pg.Listen[0].Port != 5432 || pg.Listen[0].Clients != 1 || pg.Listen[0].Public || pg.FDs != 2 {
		t.Fatalf("postgres listen = %+v fds=%d", pg.Listen, pg.FDs)
	}
	node := svc["container:0123456789abcdef0123456789abcdef"]
	if len(node.Peers) != 2 {
		t.Fatalf("node peers = %+v", node.Peers)
	}
	names := map[string]bool{}
	for _, p := range node.Peers {
		names[p.Name+"@"+p.Addr] = true
	}
	if !names[pg.Name+"@127.0.0.1"] || !names["redis@10.0.0.9"] {
		t.Fatalf("peer names = %v", names)
	}
	if len(s.Listeners) != 2 || s.Listeners[0].Port != 80 || !s.Listeners[0].Public || s.Listeners[0].Service != "" {
		t.Fatalf("listeners = %+v", s.Listeners)
	}
}
