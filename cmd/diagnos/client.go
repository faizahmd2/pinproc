package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/spf13/cobra"
)

// newReportCmd is the one command an operator runs on the box: trigger an
// investigation on the local service and print the report. --last shows the
// previous report without running a new one.
func newReportCmd() *cobra.Command {
	var last, asJSON bool
	var hint, dimension string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "report",
		Short: "run an investigation on the local service and print the report",
		RunE: func(cmd *cobra.Command, args []string) error {
			listen, key := clientTarget()
			if timeout <= 0 {
				timeout = 35 * time.Second
			}

			path := "/investigate"
			if last {
				path = "/report"
			}
			q := url.Values{}
			if asJSON {
				q.Set("json", "true")
			}
			if hint != "" {
				q.Set("hint", hint)
			}
			if dimension != "" {
				q.Set("dimension", dimension)
			}
			endpoint := "http://" + listen + path
			if len(q) > 0 {
				endpoint += "?" + q.Encode()
			}

			req, err := http.NewRequest(http.MethodGet, endpoint, nil)
			if err != nil {
				return err
			}
			if key != "" {
				req.Header.Set("Authorization", "Bearer "+key)
			}
			resp, err := (&http.Client{Timeout: timeout}).Do(req)
			if err != nil {
				return fmt.Errorf("could not reach pinproc service on %s — is pinproc.service running? (%w)", listen, err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("pinproc service requires an API key; re-run with sudo so the configured key can be used")
			}
			fmt.Fprint(cmd.OutOrStdout(), string(body))
			if !strings.HasSuffix(string(body), "\n") {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&last, "last", false, "show the last report without running a new investigation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of markdown")
	cmd.Flags().StringVar(&hint, "hint", "", "operator context, e.g. \"memory alert\"")
	cmd.Flags().StringVar(&dimension, "dimension", "", "focus: cpu|memory|io|network|scheduling|filesystem|limits")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "client timeout (default 35s)")
	return cmd
}

// clientTarget resolves the service address and API key from the managed config,
// falling back to the documented default when the config cannot be read.
func clientTarget() (listen, key string) {
	listen = "127.0.0.1:8080"
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return listen, ""
	}
	if cfg.Server.Listen != "" {
		listen = cfg.Server.Listen
	}
	return listen, cfg.Server.APIKey
}
