package transport

import (
	"context"
	"path/filepath"
	"strings"
)

const factsCommand = "cat /etc/os-release 2>/dev/null; echo '---'; uname -r; echo '---'; command -v systemctl journalctl ss netstat dnf yum apt rpm dpkg service 2>/dev/null"

func probeFacts(ctx context.Context, e Executor) (Facts, error) {
	r := e.Run(ctx, Command{Key: "bootstrap.facts", Argv: factsCommand})
	if r.Err != nil {
		return Facts{}, r.Err
	}
	sections := strings.Split(r.Stdout, "---\n")
	f := Facts{Family: FamilyUnknown, Has: map[string]bool{}}
	if len(sections) > 0 {
		for _, line := range strings.Split(sections[0], "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok {
				continue
			}
			v = strings.Trim(v, "\"")
			switch k {
			case "ID":
				f.OSID = strings.ToLower(v)
			case "ID_LIKE":
				f.OSIDLike = strings.Fields(strings.ToLower(v))
			case "VERSION_ID":
				f.Version = v
			}
		}
	}
	if len(sections) > 1 {
		f.Kernel = strings.TrimSpace(sections[1])
	}
	if len(sections) > 2 {
		for _, value := range strings.Fields(sections[2]) {
			f.Has[filepath.Base(strings.TrimSpace(value))] = true
		}
	}
	f.Family = familyFor(f.OSID, f.OSIDLike)
	return f, nil
}
func familyFor(id string, likes []string) Family {
	values := append([]string{id}, likes...)
	for _, v := range values {
		switch v {
		case "debian", "ubuntu", "linuxmint":
			return FamilyDebian
		case "rhel", "fedora", "centos", "amzn", "rocky", "almalinux":
			return FamilyRHEL
		}
	}
	return FamilyUnknown
}
