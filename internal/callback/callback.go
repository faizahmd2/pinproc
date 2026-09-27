package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
)

// Post sends one bounded report webhook request. Callback delivery is best-effort
// and is deliberately outside the investigation result path.
func Post(ctx context.Context, url string, timeout time.Duration, inv *contract.Investigation) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("callback URL is empty")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	body, err := json.Marshal(inv)
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create callback request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pinproc")

	client := &http.Client{Timeout: timeout}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("callback request failed: %w", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("callback returned HTTP %d", res.StatusCode)
	}
	return nil
}
