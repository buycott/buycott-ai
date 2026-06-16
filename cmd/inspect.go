package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"buycott/internal/model"
	"github.com/spf13/cobra"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Inspect tasks or artifacts",
}

var inspectTaskCmd = &cobra.Command{
	Use:   "task <id>",
	Short: "Show full details for a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		t, err := srv.GetTask(args[0])
		if err != nil {
			return err
		}
		if t == nil {
			return fmt.Errorf("task %q not found", args[0])
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(t)
	},
}

var inspectTasksCmd = &cobra.Command{
	Use:   "tasks [status]",
	Short: "List tasks, optionally filtered by status",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filter := model.TaskFilter{}
		if len(args) == 1 {
			filter.Status = model.TaskStatus(args[0])
		}
		tasks, err := srv.ListTasks(filter)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			fmt.Printf("[%s] %-15s %-12s %s\n", t.ID[:8], t.AssignedRole, t.Status, t.Title)
		}
		return nil
	},
}

var inspectArtifactsCmd = &cobra.Command{
	Use:   "artifacts [subpath]",
	Short: "List files in the artifacts volume",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cfg == nil {
			return fmt.Errorf("inspect artifacts requires local mode (not supported with --server)")
		}
		root := cfg.Project.ArtifactsPath
		if len(args) == 1 {
			root = filepath.Join(root, args[0])
		}
		return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(cfg.Project.ArtifactsPath, path)
			if info.IsDir() {
				fmt.Printf("%s/\n", rel)
			} else {
				fmt.Printf("%-60s %d bytes\n", rel, info.Size())
			}
			return nil
		})
	},
}

var inspectReleasesCmd = &cobra.Command{
	Use:   "releases",
	Short: "List all releases",
	RunE: func(cmd *cobra.Command, args []string) error {
		releases, err := srv.ListReleases()
		if err != nil {
			return err
		}
		if len(releases) == 0 {
			fmt.Println("No releases yet.")
			return nil
		}
		for _, r := range releases {
			fmt.Printf("%-12s  %s  %s\n", r.Version, r.CreatedAt.Format("2006-01-02 15:04"), r.Path)
			if r.Notes != "" {
				for _, line := range strings.SplitAfter(r.Notes, "\n") {
					fmt.Printf("              %s", line)
				}
				fmt.Println()
			}
		}
		return nil
	},
}

func init() {
	inspectCmd.AddCommand(inspectTaskCmd)
	inspectCmd.AddCommand(inspectTasksCmd)
	inspectCmd.AddCommand(inspectArtifactsCmd)
	inspectCmd.AddCommand(inspectReleasesCmd)
	rootCmd.AddCommand(inspectCmd)
}
