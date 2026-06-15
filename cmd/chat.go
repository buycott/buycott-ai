package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var chatInject bool

var chatCmd = &cobra.Command{
	Use:   "chat <role> \"message\"",
	Short: "Send a message to a running agent and stream its response",
	Long: `Send a message to the named agent role and print the streamed response.

Use --inject to append the exchange to the active task's conversation history,
allowing it to influence what the agent does next.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		role := args[0]
		message := args[1]

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		ch, err := srv.Chat(ctx, role, message, chatInject)
		if err != nil {
			return fmt.Errorf("chat: %w", err)
		}

		for chunk := range ch {
			fmt.Print(chunk)
		}
		fmt.Println()
		return nil
	},
}

func init() {
	chatCmd.Flags().BoolVar(&chatInject, "inject", false, "append exchange to the active task's conversation history")
	rootCmd.AddCommand(chatCmd)
}
