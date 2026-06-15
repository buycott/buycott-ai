package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"buycott/internal/dashboard"
	"buycott/internal/grpcserver"
)

var noDashboard bool

var startCmd = &cobra.Command{
	Use:   "start [direction]",
	Short: "Start the pipeline with an initial product direction",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		direction := args[0]

		if cfg == nil {
			return fmt.Errorf("start requires a local config (not supported with --server)")
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		fmt.Printf("Starting pipeline: %q\n", direction)
		if err := srv.Start(ctx, direction); err != nil {
			return fmt.Errorf("start: %w", err)
		}

		port := cfg.API.Port
		artifacts := cfg.Project.ArtifactsPath

		fmt.Printf("gRPC API   listening on :%d\n", port)
		go func() {
			if err := grpcserver.Listen(srv, artifacts, port); err != nil {
				fmt.Fprintf(os.Stderr, "gRPC server error: %v\n", err)
			}
		}()

		if !noDashboard {
			dashPort := cfg.Dashboard.Port
			fmt.Printf("Dashboard  listening on http://localhost:%d\n", dashPort)
			go func() {
				if err := dashboard.Listen(srv, dashPort); err != nil {
					fmt.Fprintf(os.Stderr, "dashboard error: %v\n", err)
				}
			}()
		}

		<-ctx.Done()
		fmt.Println("\nShutting down...")
		return srv.Stop()
	},
}

func init() {
	startCmd.Flags().BoolVar(&noDashboard, "no-dashboard", false, "do not start the web dashboard (run it as a separate buycott dashboard process)")
	rootCmd.AddCommand(startCmd)
}
