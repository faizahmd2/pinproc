package main

import (
	"fmt"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/deliver"
	"github.com/faizahmd2/pinproc/internal/doc"
	"github.com/faizahmd2/pinproc/internal/render"
	"github.com/spf13/cobra"
)

// newRenderCmd re-renders a saved document. Every pinproc output is a pure
// function of its JSON, so an alert or report can be turned into any format later
// (or in another timezone) without touching the machine again.
func newRenderCmd() *cobra.Command {
	var as, tz string
	cmd := &cobra.Command{
		Use:   "render <alert.json|report.json>",
		Short: "re-render a saved alert/report JSON as html, email or text (stdout)",
		Example: `  sudo pinproc render /var/lib/pinproc/alert.json > alert.html
  sudo pinproc render /var/lib/pinproc/report.json --as email > email.html
  pinproc render report.json --as text --timezone IST`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := doc.Load(args[0])
			if err != nil {
				return err
			}
			opt := render.Options{}
			if tz != "" {
				loc, zone, err := render.ParseZone(tz)
				if err != nil {
					return err
				}
				opt = render.Options{Location: loc, Zone: zone}
			} else if cfg, err := config.Load(cfgPath); err == nil {
				opt = deliver.Options(cfg)
			}
			w := cmd.OutOrStdout()
			switch as {
			case "html":
				b, err := render.HTML(d, opt)
				if err != nil {
					return err
				}
				_, err = w.Write(b)
				return err
			case "email":
				e, err := render.RenderEmail(d, opt, render.FileName(d, "html"))
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(w, e.HTML)
				return err
			case "text":
				t := d.Text
				if t == "" {
					t = render.Text(d, opt)
				}
				_, err = fmt.Fprint(w, t)
				return err
			}
			return fmt.Errorf("--as must be html, email or text")
		},
	}
	cmd.Flags().StringVar(&as, "as", "html", "output: html, email or text")
	cmd.Flags().StringVar(&tz, "timezone", "", "override report.timezone, e.g. IST or UTC")
	return cmd
}
