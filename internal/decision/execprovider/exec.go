package execprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	"github.com/faizahmd2/pinproc/internal/decision"
	"github.com/faizahmd2/pinproc/internal/provider"
)

// Provider executes an installed provider using the stable stdin/stdout protocol.
type Provider struct {
	manifest  provider.Manifest
	config    map[string]any
	binaryDir string
	maxOutput int64
}

// New creates a provider backed by an installed executable.
func New(manifest provider.Manifest, cfg map[string]any) *Provider {
	return &Provider{
		manifest:  manifest,
		config:    cloneMap(cfg),
		binaryDir: provider.BinaryDir,
		maxOutput: 1 << 20,
	}
}

// NewWithBinaryDir is intended for tests and development.
func NewWithBinaryDir(manifest provider.Manifest, cfg map[string]any, dir string) *Provider {
	p := New(manifest, cfg)
	if strings.TrimSpace(dir) != "" {
		p.binaryDir = dir
	}
	return p
}

// Name returns the provider ID.
func (p *Provider) Name() string { return p.manifest.ID }

// Ask invokes one provider process. Providers own provider-specific retries.
func (p *Provider) Ask(ctx context.Context, state any, questions map[string]decision.Question) (map[string]decision.Answer, error) {
	if p == nil {
		return nil, fmt.Errorf("provider is nil")
	}
	if err := p.manifest.Validate(); err != nil {
		return nil, err
	}

	stateBytes, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshal provider state: %w", err)
	}
	reqID := fmt.Sprintf("req-%d", sequence.Add(1))
	req := provider.Request{
		ProtocolVersion: provider.ProtocolVersion,
		RequestID:       reqID,
		Method:          "ask",
		Config:          cloneMap(p.config),
		State:           stateBytes,
		Questions:       questions,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}

	cmd := exec.CommandContext(ctx, p.manifest.BinaryPath(p.binaryDir))
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "PINPROC_PROVIDER_PROTOCOL=1"}
	cmd.Dir = "/"
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open provider stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open provider stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start provider %q: %w", p.manifest.ID, err)
	}

	writeDone := make(chan error, 1)
	go func() {
		_, werr := stdin.Write(append(payload, '
'))
		if cerr := stdin.Close(); werr == nil {
			werr = cerr
		}
		writeDone <- werr
	}()

	select {
	case err := <-writeDone:
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, fmt.Errorf("write provider request: %w", err)
		}
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, ctx.Err()
	}

	readDone := make(chan struct {
		data []byte
		err  error
	}, 1)
	go func() {
		data, rerr := io.ReadAll(io.LimitReader(stdout, p.maxOutput+1))
		readDone <- struct {
			data []byte
			err  error
		}{data: data, err: rerr}
	}()

	var result struct {
		data []byte
		err  error
	}
	select {
	case result = <-readDone:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, ctx.Err()
	}

	if int64(len(result.data)) > p.maxOutput {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("provider response exceeds %d bytes", p.maxOutput)
	}
	if result.err != nil {
		_ = cmd.Wait()
		return nil, fmt.Errorf("read provider response: %w", result.err)
	}

	if waitErr := cmd.Wait(); waitErr != nil {
		return nil, fmt.Errorf("provider %q exited unsuccessfully: %w", p.manifest.ID, waitErr)
	}

	var response provider.Response
	if err := json.Unmarshal(bytes.TrimSpace(result.data), &response); err != nil {
		return nil, fmt.Errorf("decode provider response: %w", err)
	}
	if err := response.Validate(reqID); err != nil {
		return nil, err
	}
	if response.Answers == nil {
		return nil, fmt.Errorf("provider %q returned no answers", p.manifest.ID)
	}
	return response.Answers, nil
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

var sequence atomic.Uint64
