package transport

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func hostKeyCallback(cfg Config) (ssh.HostKeyCallback, error) {
	if cfg.HostKeyPolicy == HostKeyInsecure {
		cfg.Logger.Warn("SSH host-key verification is disabled", "host", cfg.Host)
		return ssh.InsecureIgnoreHostKey(), nil
	}
	path := cfg.KnownHosts
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("find home directory for known_hosts: %w", err)
		}
		path = filepath.Join(home, ".ssh", "known_hosts")
	}
	path = expandHome(path)
	var callback ssh.HostKeyCallback
	if _, err := os.Stat(path); err == nil {
		var err error
		callback, err = knownhosts.New(path)
		if err != nil {
			return nil, fmt.Errorf("load known_hosts %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect known_hosts %s: %w", path, err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if callback != nil {
			if err := callback(hostname, remote, key); err == nil {
				return nil
			} else {
				var keyErr *knownhosts.KeyError
				if !asKeyError(err, &keyErr) {
					return err
				}
				if len(keyErr.Want) > 0 {
					return fmt.Errorf("SSH host key mismatch for %s (possible MITM); refusing connection", hostname)
				}
			}
		}
		fingerprint := ssh.FingerprintSHA256(key)
		switch cfg.HostKeyPolicy {
		case HostKeyStrict:
			return fmt.Errorf("unknown SSH host %s (%s); add its key to %s", hostname, fingerprint, path)
		case HostKeyPrompt:
			if !isTerminal() {
				return fmt.Errorf("unknown SSH host %s (%s); non-interactive mode requires known_hosts or host_key_policy=accept-new", hostname, fingerprint)
			}
			fmt.Fprintf(os.Stderr, "The authenticity of host %s cannot be established. SHA256 fingerprint: %s. Trust it? [y/N] ", hostname, fingerprint)
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil || !strings.EqualFold(strings.TrimSpace(line), "y") && !strings.EqualFold(strings.TrimSpace(line), "yes") {
				return fmt.Errorf("SSH host key not accepted for %s", hostname)
			}
			return appendKnownHost(path, hostname, key)
		case HostKeyAcceptNew:
			return appendKnownHost(path, hostname, key)
		default:
			return fmt.Errorf("invalid ssh.host_key_policy %q", cfg.HostKeyPolicy)
		}
	}, nil
}
func appendKnownHost(path, host string, key ssh.PublicKey) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create known_hosts directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open known_hosts: %w", err)
	}
	defer file.Close()
	_, err = fmt.Fprintln(file, knownhosts.Line([]string{host}, key))
	if err != nil {
		return fmt.Errorf("write known_hosts: %w", err)
	}
	return nil
}
func asKeyError(err error, target **knownhosts.KeyError) bool {
	keyErr, ok := err.(*knownhosts.KeyError)
	if ok {
		*target = keyErr
	}
	return ok
}
