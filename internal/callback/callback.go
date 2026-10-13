package callback

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Send delivers one report, as plain text (the rendered report) in the request
// body, to a single configured URL using the given HTTP method (POST or GET). This
// is the only traffic pinproc ever sends. Delivery is best-effort and bounded.
func Send(ctx context.Context, method, url string, timeout time.Duration, report string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("callback URL is empty")
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodPost
	}
	if method != http.MethodPost && method != http.MethodGet {
		return fmt.Errorf("callback method %q is not supported (use POST or GET)", method)
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, url, strings.NewReader(report))
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
