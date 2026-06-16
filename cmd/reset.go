package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"buycott/internal/server"
	"github.com/spf13/cobra"
)

var (
	resetWipeArtifacts bool
	resetYes           bool
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Clear the current run (tasks, events, releases, logs) and start over",
	Long: `Reset wipes all run state so the pipeline can start from scratch:
tasks, events, releases, LLM logs and counters are deleted. With
--wipe-artifacts, generated project files are removed too (the .buycott state
directory is preserved).

Run this while the pipeline is stopped, then 'buycott start' again. Not
supported over a remote --server connection; run it on the pipeline host.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !resetYes {
			what := "all run state (tasks, events, releases, logs)"
			if resetWipeArtifacts {
				what += " and ALL generated artifacts"
			}
			fmt.Printf("This will permanently delete %s.\nContinue? [y/N]: ", what)
			reader := bufio.NewReader(os.Stdin)
			line, _ := reader.ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		if err := srv.Reset(context.Background(), server.ResetOptions{
			WipeArtifacts: resetWipeArtifacts,
			Restart:       false,
		}); err != nil {
			return err
		}
		fmt.Println("Run cleared. Start a new run with 'buycott start'.")
		return nil
	},
}

func init() {
	resetCmd.Flags().BoolVar(&resetWipeArtifacts, "wipe-artifacts", false, "also delete generated project files under the artifacts directory")
	resetCmd.Flags().BoolVarP(&resetYes, "yes", "y", false, "skip the confirmation prompt")
	rootCmd.AddCommand(resetCmd)
}
