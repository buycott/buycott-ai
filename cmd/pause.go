package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var pauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause the pipeline (completes current task first)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := srv.Pause(); err != nil {
			return err
		}
		fmt.Println("Pipeline paused.")
		return nil
	},
}

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume a paused pipeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := srv.Resume(); err != nil {
			return err
		}
		fmt.Println("Pipeline resumed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pauseCmd)
	rootCmd.AddCommand(resumeCmd)
}
