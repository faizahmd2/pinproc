package inventory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSSHDFirstValueWinsAndIncludes(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sshd_config.d"), 0o755)
	os.WriteFile(filepath.Join(dir, "sshd_config.d", "50-cloud.conf"), []byte("PasswordAuthentication no\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "sshd_config"), []byte(`# comment
Include `+filepath.Join(dir, "sshd_config.d", "*.conf")+`
PasswordAuthentication yes
PermitRootLogin=no
Match User deploy
  PermitRootLogin yes
`), 0o644)
	v := sshdValues(filepath.Join(dir, "sshd_config"), 0)
	if v["passwordauthentication"] != "no" || v["permitrootlogin"] != "no" {
		t.Fatalf("got %v", v)
	}
}

func TestServiceFDCheck(t *testing.T) {
	s := &Snapshot{Services: []Service{
		{Name: "nginx", Kind: "systemd", Procs: 1, FDs: 900, FDLimit: 1024},
		{Name: "api", Kind: "systemd", Procs: 1, FDs: 10, FDLimit: 1024, Listen: []Listener{{Proto: "tcp", Port: 8080, Clients: 120}}},
		{Name: "ssh", Kind: "systemd", Procs: 1, FDs: 6, FDLimit: 1024, Listen: []Listener{{Proto: "tcp", Port: 22, Clients: 1}}},
		{Name: "db", Kind: "systemd", Procs: 2, FDs: 200, FDLimit: 65536},
	}}
	ch := checkServiceFDs(s)
	if ch.State != "warn" || ch.Value != "nginx 900 of 1024 · api limit 1024 (low for a network service)" {
		t.Fatalf("got %+v", ch)
	}
}
