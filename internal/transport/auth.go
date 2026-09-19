package transport

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func clientConfig(cfg Config) (*ssh.ClientConfig, error) {
	signers, err := resolveSigners(cfg.KeyPath)
	if err != nil {
		return nil, err
	}
	callback, err := hostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}
	return &ssh.ClientConfig{User: cfg.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(signers...)}, HostKeyCallback: callback, Timeout: cfg.ConnectTimeout}, nil
}
func resolveSigners(keyPath string) ([]ssh.Signer, error) {
	var signers []ssh.Signer
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		if conn, err := net.Dial("unix", socket); err == nil {
			agentSigners, signErr := agent.NewClient(conn).Signers()
			_ = conn.Close()
			if signErr == nil {
				signers = append(signers, agentSigners...)
			}
		}
	}
	paths := []string{}
	if strings.TrimSpace(keyPath) != "" {
		paths = append(paths, expandHome(keyPath))
	} else if home, err := os.UserHomeDir(); err == nil {
		paths = []string{filepath.Join(home, ".ssh", "id_ed25519"), filepath.Join(home, ".ssh", "id_ecdsa"), filepath.Join(home, ".ssh", "id_rsa")}
	}
	for _, path := range paths {
		signer, err := parseKey(path)
		if err == nil {
			signers = append(signers, signer)
		} else if keyPath != "" {
			if strings.HasSuffix(path, ".pub") {
				return nil, fmt.Errorf("ssh.key_path: %s is a public key; use the PRIVATE key (for example ~/.ssh/id_ed25519), not the .pub file", path)
			}
			return nil, err
		}
	}
	if len(signers) == 0 {
		return nil, fmt.Errorf("no SSH authentication method available: start ssh-agent, set ssh.key_path, or add a default private key")
	}
	return signers, nil
}
func parseKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read SSH private key %s: %w", path, err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err == nil {
		return signer, nil
	}
	var passErr *ssh.PassphraseMissingError
	if !errorsAs(err, &passErr) {
		return nil, fmt.Errorf("parse SSH private key %s: %w", path, err)
	}
	if !isTerminal() {
		return nil, fmt.Errorf("SSH private key %s is encrypted; use an ssh-agent or run from an interactive terminal", path)
	}
	return nil, fmt.Errorf("SSH private key %s is encrypted; load it into ssh-agent (passphrase prompting is intentionally delegated to the agent)", path)
}

// keeps the type assertion easy to test while retaining Go 1.26 compatibility.
func errorsAs(err error, target interface{}) bool {
	if _, ok := err.(*ssh.PassphraseMissingError); ok {
		if p, ok := target.(**ssh.PassphraseMissingError); ok {
			*p = err.(*ssh.PassphraseMissingError)
		}
		return true
	}
	return false
}
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
func isTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
