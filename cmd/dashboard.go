package cmd

import (
	"fmt"

	"buycott/internal/dashboard"
	"github.com/spf13/cobra"
)

var dashboardPort int

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the web dashboard (optionally connecting to a remote pipeline via --server)",
	Long: `Start the Buycott web dashboard without running the pipeline.

In split-process mode, connect the dashboard to a running pipeline:

  buycott dashboard --server pipeline-host:8080 --port 8000

In single-process mode (no --server), the dashboard reads from the local
state DB directly — useful for local development with buycott start.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		port := dashboardPort
		if port == 0 && cfg != nil {
			port = cfg.Dashboard.Port
		}
		if port == 0 {
			port = 8000
		}
		fmt.Printf("Dashboard listening on http://0.0.0.0:%d\n", port)
		return dashboard.Listen(srv, port)
	},
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 0, "HTTP port (default: config dashboard.port or 8000)")
	rootCmd.AddCommand(dashboardCmd)
}
