package transport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
)

type HostKeyPolicy string

const (
	HostKeyStrict    HostKeyPolicy = "strict"
	HostKeyPrompt    HostKeyPolicy = "prompt"
	HostKeyAcceptNew HostKeyPolicy = "accept-new"
	HostKeyInsecure  HostKeyPolicy = "insecure"
)

type Config struct {
	Host           string
	User           string
	Port           int
	KeyPath        string
	KnownHosts     string
	HostKeyPolicy  HostKeyPolicy
	JumpHosts      []string
	ConnectTimeout time.Duration
	CommandTimeout time.Duration
	MaxParallel    int
	MaxOutputBytes int64
	Logger         *slog.Logger
}

// SSHExecutor maintains exactly one authenticated SSH client for a target.
// SSH sessions are multiplexed over that client by Run and RunAll.
type SSHExecutor struct {
	client    *ssh.Client
	chain     []*ssh.Client
	cfg       Config
	logger    *slog.Logger
	stop      chan struct{}
	done      chan struct{}
	mu        sync.RWMutex
	dead      error
	facts     Facts
	factsErr  error
	factsOnce sync.Once
}

func Dial(ctx context.Context, cfg Config) (*SSHExecutor, error) {
	if strings.TrimSpace(cfg.Host) == "" {
		return nil, fmt.Errorf("ssh host is required")
	}
	if strings.TrimSpace(cfg.User) == "" {
		return nil, fmt.Errorf("ssh user is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 10 * time.Second
	}
	if cfg.CommandTimeout <= 0 {
		cfg.CommandTimeout = 30 * time.Second
	}
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = 4
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = 1024 * 1024
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.MaxParallel > 8 {
		cfg.Logger.Warn("ssh max_parallel exceeds sshd-safe limit; using 8", "configured", cfg.MaxParallel)
		cfg.MaxParallel = 8
	}
	if cfg.HostKeyPolicy == "" {
		cfg.HostKeyPolicy = HostKeyPrompt
	}
	sshConfig, err := clientConfig(cfg)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		client, chain, err := dialChain(ctx, cfg, sshConfig)
		if err == nil {
			e := &SSHExecutor{client: client, chain: chain, cfg: cfg, logger: cfg.Logger, stop: make(chan struct{}), done: make(chan struct{})}
			go e.keepalive()
			return e, nil
		}
		lastErr = err
		if !isNetworkError(err) {
			break
		}
		if attempt < 2 {
			delay := time.Duration(1<<attempt) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return nil, fmt.Errorf("connect SSH %s@%s: %w", cfg.User, address(cfg.Host, cfg.Port), lastErr)
}

func dialChain(ctx context.Context, cfg Config, sshConfig *ssh.ClientConfig) (*ssh.Client, []*ssh.Client, error) {
	addresses := append(append([]string{}, cfg.JumpHosts...), address(cfg.Host, cfg.Port))
	var chain []*ssh.Client
	var raw net.Conn
	for i, addr := range addresses {
		var err error
		if i == 0 {
			raw, err = dialTCP(ctx, addr, cfg.ConnectTimeout)
		} else {
			raw, err = chain[len(chain)-1].Dial("tcp", addr)
		}
		if err != nil {
			closeClients(chain)
			return nil, nil, err
		}
		conn, chans, reqs, err := handshake(ctx, raw, addr, sshConfig)
		if err != nil {
			_ = raw.Close()
			closeClients(chain)
			return nil, nil, err
		}
		client := ssh.NewClient(conn, chans, reqs)
		chain = append(chain, client)
	}
	return chain[len(chain)-1], chain, nil
}
func dialTCP(ctx context.Context, addr string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, "tcp", addr)
}
func handshake(ctx context.Context, raw net.Conn, addr string, cfg *ssh.ClientConfig) (ssh.Conn, <-chan ssh.NewChannel, <-chan *ssh.Request, error) {
	type result struct {
		c   ssh.Conn
		ch  <-chan ssh.NewChannel
		r   <-chan *ssh.Request
		err error
	}
	done := make(chan result, 1)
	go func() { c, ch, r, err := ssh.NewClientConn(raw, addr, cfg); done <- result{c, ch, r, err} }()
	select {
	case r := <-done:
		return r.c, r.ch, r.r, r.err
	case <-ctx.Done():
		_ = raw.Close()
		return nil, nil, nil, ctx.Err()
	}
}
func closeClients(clients []*ssh.Client) {
	for i := len(clients) - 1; i >= 0; i-- {
		_ = clients[i].Close()
	}
}
func address(host string, port int) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}
func isNetworkError(err error) bool { var ne net.Error; return errors.As(err, &ne) }

func (e *SSHExecutor) Run(ctx context.Context, cmd Command) Result {
	started := time.Now()
	result := Result{Key: cmd.Key, ExitCode: -1}
	if strings.TrimSpace(cmd.Argv) == "" {
		result.Err = fmt.Errorf("command argv is empty")
		return result
	}
	if dead := e.connectionError(); dead != nil {
		result.Err = dead
		return result
	}
	timeout := cmd.Timeout
	if timeout <= 0 {
		timeout = e.cfg.CommandTimeout
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout+3*time.Second)
	defer cancel()
	session, err := e.client.NewSession()
	if err != nil {
		result.Err = fmt.Errorf("open SSH session: %w", err)
		result.Duration = time.Since(started)
		return result
	}
	defer session.Close()
	stdout, stderr := newCappedBuffer(limitFor(cmd.MaxBytes, e.cfg.MaxOutputBytes)), newCappedBuffer(limitFor(cmd.MaxBytes, e.cfg.MaxOutputBytes))
	session.Stdout, session.Stderr = stdout, stderr
	if err := session.Start(wrapCommand(cmd, timeout)); err != nil {
		result.Err = fmt.Errorf("start remote command: %w", err)
		result.Duration = time.Since(started)
		return result
	}
	done := make(chan error, 1)
	go func() { done <- session.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			result.ExitCode = 0
		} else {
			var exitErr *ssh.ExitError
			if errors.As(err, &exitErr) {
				result.ExitCode = exitErr.ExitStatus()
			} else {
				result.Err = fmt.Errorf("wait remote command: %w", err)
			}
		}
	case <-commandCtx.Done():
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		select {
		case <-done:
		default:
		}
		result.TimedOut, result.ExitCode = true, 124
	}
	result.Stdout, result.Stderr, result.Truncated, result.Duration = stdout.String(), stderr.String(), stdout.Truncated() || stderr.Truncated(), time.Since(started)
	e.logger.Info("remote command completed", "host", e.cfg.Host, "command_key", cmd.Key, "duration_ms", result.Duration.Milliseconds(), "exit_code", result.ExitCode, "timed_out", result.TimedOut, "truncated", result.Truncated)
	e.logger.Debug("remote command argv", "host", e.cfg.Host, "command_key", cmd.Key, "argv", cmd.Argv)
	return result
}
func wrapCommand(cmd Command, timeout time.Duration) string {
	seconds := int(timeout.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	argv := strings.ReplaceAll(cmd.Argv, "'", "'\\''")
	prefix := "LC_ALL=C timeout -k 2 " + strconv.Itoa(seconds) + " sh -c '" + argv + "'"
	if cmd.Sudo {
		return "sudo -n env " + prefix
	}
	return prefix
}
func (e *SSHExecutor) RunAll(ctx context.Context, cmds []Command) []Result {
	results := make([]Result, len(cmds))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(e.cfg.MaxParallel)
	for i := range cmds {
		i := i
		group.Go(func() error { results[i] = e.Run(groupCtx, cmds[i]); return nil })
	}
	_ = group.Wait()
	return results
}

// RunScript executes one bounded shell script as a single SSH session.
func (e *SSHExecutor) RunScript(ctx context.Context, script string, timeout time.Duration, maxBytes int64) Result {
	return e.Run(ctx, Command{Key: "script", Argv: script, Timeout: timeout, MaxBytes: maxBytes})
}
func (e *SSHExecutor) Facts(ctx context.Context) (Facts, error) {
	e.factsOnce.Do(func() { e.facts, e.factsErr = probeFacts(ctx, e) })
	return e.facts, e.factsErr
}
func (e *SSHExecutor) connectionError() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.dead != nil {
		return fmt.Errorf("SSH connection lost: %w", e.dead)
	}
	return nil
}
func (e *SSHExecutor) keepalive() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer close(e.done)
	for {
		select {
		case <-e.stop:
			return
		case <-ticker.C:
			if _, _, err := e.client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				e.mu.Lock()
				e.dead = err
				e.mu.Unlock()
				return
			}
		}
	}
}
func (e *SSHExecutor) Close() error {
	select {
	case <-e.stop:
	default:
		close(e.stop)
	}
	<-e.done
	closeClients(e.chain)
	return nil
}
