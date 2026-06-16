package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"buycott/internal/model"
	"github.com/spf13/cobra"
)

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiGreen  = "\033[32m"
	ansiBlue   = "\033[34m"
	ansiCyan   = "\033[36m"
	ansiYellow = "\033[33m"
	ansiPurple = "\033[35m"
	ansiGray   = "\033[90m"
)

var (
	convTaskID string
	convRole   string
	convLimit  int
	noColor    bool
)

var conversationCmd = &cobra.Command{
	Use:     "conversation",
	Aliases: []string{"conv", "convo"},
	Short:   "View LLM prompt/response logs as conversation threads",
	Long: `Display every prompt sent to and response received from each model,
formatted as a threaded conversation (like a Slack thread).

Filter by task ID or role to narrow the view:

  buycott conversation --task abc123
  buycott conversation --role backend
  buycott conversation --role pm --limit 5`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logs, err := srv.ListConversations(convTaskID, convRole, convLimit)
		if err != nil {
			return err
		}
		if len(logs) == 0 {
			fmt.Println("No conversation logs found.")
			return nil
		}

		// ListConversations returns newest-first; reverse for chronological display.
		for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
			logs[i], logs[j] = logs[j], logs[i]
		}

		useColor := !noColor && isTerminal()
		for _, l := range logs {
			printThread(l, useColor)
		}
		return nil
	},
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func printThread(l *model.LLMLog, color bool) {
	c := func(code, s string) string {
		if !color {
			return s
		}
		return code + s + ansiReset
	}
	dim := func(s string) string { return c(ansiDim, s) }
	bold := func(s string) string { return c(ansiBold, s) }

	callColor := map[string]string{
		"process_task":   ansiBlue,
		"review_task":    ansiYellow,
		"generate_tasks": ansiGreen,
		"check_release":  ansiPurple,
		"chat":           ansiCyan,
	}
	cc := callColor[l.CallType]
	if cc == "" {
		cc = ansiGray
	}

	// ── Thread header ─────────────────────────────────────────────────────
	sep := strings.Repeat("─", 72)
	fmt.Println(dim(sep))

	header := fmt.Sprintf(" %s  %s · %s",
		c(cc, "●"),
		bold(l.CallType),
		c(ansiGray, l.Role+" · "+l.Model),
	)
	if l.TaskID != "" {
		header += dim("  task:" + l.TaskID[:8])
	}
	fmt.Println(header)

	meta := fmt.Sprintf(" %s", l.CreatedAt.Local().Format(time.RFC3339))
	if l.InputTokens+l.OutputTokens > 0 {
		meta += fmt.Sprintf("  %s in  %s out",
			c(ansiGray, strconv.Itoa(l.InputTokens)),
			c(ansiGray, strconv.Itoa(l.OutputTokens)),
		)
	}
	if l.DurationMs > 0 {
		meta += fmt.Sprintf("  %s", dim(fmt.Sprintf("%.2fs", float64(l.DurationMs)/1000)))
	}
	fmt.Println(dim(meta))
	fmt.Println(dim(sep))
	fmt.Println()

	// ── Messages ──────────────────────────────────────────────────────────
	for i, msg := range l.Messages {
		_ = i
		switch msg.Role {
		case "system":
			printBubble("⚙  system", c(ansiGray, ""), msg.Content, color, true)
		case "user":
			printBubble("▶  user", c(ansiBlue, ""), msg.Content, color, false)
		case "assistant":
			printBubble("◀  assistant", c(ansiGreen, ""), msg.Content, color, false)
		default:
			printBubble(msg.Role, "", msg.Content, color, false)
		}
		fmt.Println()
	}

	// ── Response ──────────────────────────────────────────────────────────
	if l.Response != "" {
		printBubble("◀  assistant", c(ansiGreen, ""), l.Response, color, false)
		fmt.Println()
	}

	fmt.Println()
}

// printBubble renders a single message with a role label and word-wrapped body.
func printBubble(label, colorPrefix, content string, color, faded bool) {
	_ = colorPrefix
	const indent = "    "
	const maxWidth = 100

	labelLine := "  " + label
	if color && faded {
		fmt.Println("\033[90m" + labelLine + ansiReset)
	} else if color {
		fmt.Println(ansiBold + labelLine + ansiReset)
	} else {
		fmt.Println(labelLine)
	}

	lines := strings.Split(content, "\n")
	printed := 0
	for _, line := range lines {
		// Word-wrap long lines.
		for len(line) > 0 {
			if utf8.RuneCountInString(line) <= maxWidth {
				out := indent + line
				if color && faded {
					fmt.Println("\033[90m" + out + ansiReset)
				} else {
					fmt.Println(out)
				}
				break
			}
			// Find wrap point.
			cut := maxWidth
			if sp := strings.LastIndex(line[:cut], " "); sp > maxWidth/2 {
				cut = sp
			}
			out := indent + line[:cut]
			if color && faded {
				fmt.Println("\033[90m" + out + ansiReset)
			} else {
				fmt.Println(out)
			}
			line = strings.TrimLeft(line[cut:], " ")
		}
		printed++
		// Truncate system prompts at 30 lines to keep output readable.
		if faded && printed >= 30 && len(lines) > 32 {
			remaining := len(lines) - printed
			fmt.Printf("\033[90m"+indent+"… (%d more lines — use --no-color and pipe to a pager for full output)"+ansiReset+"\n", remaining)
			break
		}
	}
}

func init() {
	conversationCmd.Flags().StringVar(&convTaskID, "task", "", "filter by task ID (prefix match)")
	conversationCmd.Flags().StringVar(&convRole, "role", "", "filter by role name (e.g. pm, backend)")
	conversationCmd.Flags().IntVar(&convLimit, "limit", 20, "max number of exchanges to show (0 = all)")
	conversationCmd.Flags().BoolVar(&noColor, "no-color", false, "disable ANSI colors")
	rootCmd.AddCommand(conversationCmd)
}
