package cmd

import (
	"fmt"
	"os"

	"buycott/internal/config"
	"buycott/internal/grpcclient"
	"buycott/internal/server"
	"github.com/spf13/cobra"
)

var (
	cfgPath    string
	serverAddr string
	cfg        *config.Config
	srv        server.Server
)

var rootCmd = &cobra.Command{
	Use:   "buycott",
	Short: "Multi-model Task Pipeline — orchestrate LLM agents for software development",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "help" {
			return nil
		}

		// Remote mode: connect to running server over gRPC.
		if serverAddr != "" {
			c, err := grpcclient.Dial(serverAddr)
			if err != nil {
				return fmt.Errorf("connect to %s: %w", serverAddr, err)
			}
			srv = c
			return nil
		}

		// Local mode: load config and create in-process server.
		var err error
		if cfgPath != "" {
			cfg, err = config.Load(cfgPath)
		} else {
			cfg = config.Default()
		}
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		srv, err = server.NewLocal(cfg)
		if err != nil {
			return fmt.Errorf("init server: %w", err)
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "", "path to config YAML (default: built-in defaults)")
	rootCmd.PersistentFlags().StringVar(&serverAddr, "server", "", "connect to remote buycott server (host:port)")
}
