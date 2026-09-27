package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/faizahmd2/pinproc/internal/decision"
	"github.com/faizahmd2/pinproc/internal/provider"
)

const maxProviderOutput = 8 << 20

type Provider struct {
	manifest provider.Manifest
	config map[string]string
}

func New(manifest provider.Manifest, cfg map[string]string) *Provider {
	copyCfg := map[string]string{}
	for k, v := range cfg { copyCfg[k] = v }
	return &Provider{manifest: manifest, config: copyCfg}
}

func (p *Provider) Name() string { return p.manifest.ID }

func (p *Provider) Ask(ctx context.Context, state any, questions map[string]decision.Question) (map[string]decision.Answer, error) {
	req := request{ProtocolVersion: provider.ProtocolVersion, Action: "ask", State: state, Questions: questions, Config: p.config}
	body, err := json.Marshal(req)
	if err != nil { return nil, fmt.Errorf("encode provider request: %w", err) }
	cmd := exec.CommandContext(ctx, p.manifest.Executable)
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" { detail = err.Error() } else { detail = fmt.Sprintf("%v: %s", err, detail) }
		return nil, fmt.Errorf("provider %q failed: %s", p.manifest.ID, detail)
	}
	var res response
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil { return nil, fmt.Errorf("provider %q returned invalid JSON: %w", p.manifest.ID, err) }
	if res.ProtocolVersion != provider.ProtocolVersion { return nil, fmt.Errorf("provider %q returned unsupported protocol version %d", p.manifest.ID, res.ProtocolVersion) }
	if res.Error != "" { return nil, fmt.Errorf("provider %q error: %s", p.manifest.ID, res.Error) }
	if res.Answers == nil { return nil, fmt.Errorf("provider %q returned no answers", p.manifest.ID) }
	return res.Answers, nil
}

type request struct {
	ProtocolVersion int `json:"protocol_version"`
	Action string `json:"action"`
	State any `json:"state"`
	Questions map[string]decision.Question `json:"questions"`
	Config map[string]string `json:"config,omitempty"`
}

type response struct {
	ProtocolVersion int `json:"protocol_version"`
	Answers map[string]decision.Answer `json:"answers,omitempty"`
	Error string `json:"error,omitempty"`
}

type limitedBuffer struct { buf bytes.Buffer; n int }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := maxProviderOutput - b.n
	if remaining <= 0 { return 0, io.ErrShortBuffer }
	if len(p) > remaining { p = p[:remaining] }
	n, _ := b.buf.Write(p)
	b.n += n
	if b.n >= maxProviderOutput { return n, io.ErrShortBuffer }
	return n, nil
}
func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }
func (b *limitedBuffer) String() string { return b.buf.String() }