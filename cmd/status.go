package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show pipeline status",
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := srv.GetStatus()
		if err != nil {
			return err
		}

		fmt.Printf("Running:     %v\n", st.Running)
		fmt.Printf("Paused:      %v\n", st.Paused)
		fmt.Printf("Queue:       %d pending\n", st.QueueLength)
		fmt.Printf("Completed:   %d tasks\n", st.Completed)
		fmt.Printf("Escalated:   %d tasks\n", st.Escalated)

		if st.ActiveTask != nil {
			t := st.ActiveTask
			fmt.Printf("\nActive Task: [%s] %s\n", t.ID[:8], t.Title)
			fmt.Printf("  Role:      %s\n", t.AssignedRole)
			fmt.Printf("  Retries:   %d\n", t.RetryCount)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
