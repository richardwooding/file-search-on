package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/richardwooding/file-search-on/internal/monitor"
)

// MonitorsCmd lists the monitoring dashboards of every currently-running
// file-search-on instance, read from the shared peer registry. Reading
// also prunes registry entries for processes that have since died, so
// `monitors` doubles as a registry-cleanup pass.
type MonitorsCmd struct {
	Output string `short:"o" name:"output" enum:"default,bare,json" default:"default" help:"Output format: default (table: mode / pid / age / dir / url), bare (one URL per line — pipe to a browser opener), or json."`
}

func (c *MonitorsCmd) Run(_ context.Context) error {
	peers := monitor.Peers()

	switch c.Output {
	case "bare":
		for _, p := range peers {
			_, _ = fmt.Fprintln(os.Stdout, p.URL)
		}
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(peers)
	default:
		if len(peers) == 0 {
			_, _ = fmt.Fprintln(os.Stdout, "No active file-search-on dashboards.")
			_, _ = fmt.Fprintln(os.Stdout, "Start one with: file-search-on mcp --monitor   (or watch --monitor)")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "MODE\tPID\tAGE\tDIR\tURL")
		now := time.Now()
		for _, p := range peers {
			age := now.Sub(p.StartedAt).Round(time.Second)
			_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n", p.Mode, p.PID, age, p.WorkingDir, p.URL)
		}
		_ = tw.Flush()
	}
	return nil
}
