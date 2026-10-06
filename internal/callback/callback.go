package callback

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Post delivers one captured report, as plain text (the rendered report), to a
// single configured URL. This is the only traffic pinproc ever sends. Delivery is
// best-effort and bounded.
func Post(ctx context.Context, url string, timeout time.Duration, report string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("callback URL is empty")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, strings.NewReader(report))
	if err != nil {
		return fmt.Errorf("create callback request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
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
