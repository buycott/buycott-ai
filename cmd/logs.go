package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Print or stream the event log",
	RunE: func(cmd *cobra.Command, args []string) error {
		follow, _ := cmd.Flags().GetBool("follow")

		if !follow {
			events, err := srv.ListEvents(0)
			if err != nil {
				return err
			}
			for _, e := range events {
				payloadJSON, _ := json.Marshal(e.Payload)
				fmt.Printf("%s  %-30s  %s\n", e.CreatedAt.Format("15:04:05"), e.Type, string(payloadJSON))
			}
			return nil
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		ch, err := srv.StreamEvents(ctx)
		if err != nil {
			return err
		}

		for e := range ch {
			payloadJSON, _ := json.Marshal(e.Payload)
			fmt.Printf("%s  %-30s  %s\n", e.CreatedAt.Format("15:04:05"), e.Type, string(payloadJSON))
		}
		return nil
	},
}

func init() {
	logsCmd.Flags().BoolP("follow", "f", false, "stream events as they occur")
	rootCmd.AddCommand(logsCmd)
}
