package pipeline

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"buycott/internal/model"
)

const (
	stateKeyTasksSinceCheck   = "tasks_since_release_check"
	stateKeyLastReleaseCheckAt = "last_release_check_at"
)

// maybeCheckRelease increments the completed-task counter and triggers a
// release check when the counter reaches p.releaseInterval.
// Called after every task is marked DONE. Safe to call with interval == 0 (no-op).
func (p *Pipeline) maybeCheckRelease(ctx context.Context) {
	if p.releaseInterval <= 0 {
		return
	}

	countStr, _ := p.tasks.GetPipelineState(stateKeyTasksSinceCheck)
	count := 0
	fmt.Sscanf(countStr, "%d", &count)
	count++

	if count < p.releaseInterval {
		_ = p.tasks.SetPipelineState(stateKeyTasksSinceCheck, fmt.Sprintf("%d", count))
		return
	}

	// Threshold reached — reset counter and run check.
	_ = p.tasks.SetPipelineState(stateKeyTasksSinceCheck, "0")

	if err := p.performReleaseCheck(ctx); err != nil {
		log.Printf("release check error: %v", err)
	}
}

func (p *Pipeline) performReleaseCheck(ctx context.Context) error {
	p.emit("release.check_started", nil)

	// Gather tasks completed since the last check time.
	lastCheckStr, _ := p.tasks.GetPipelineState(stateKeyLastReleaseCheckAt)
	var lastCheckAt time.Time
	if lastCheckStr != "" {
		lastCheckAt, _ = time.Parse(time.RFC3339, lastCheckStr)
	}

	recentTasks, err := p.tasks.ListDoneSince(lastCheckAt)
	if err != nil {
		return fmt.Errorf("list done tasks: %w", err)
	}

	// Record this check time before calling the PM.
	_ = p.tasks.SetPipelineState(stateKeyLastReleaseCheckAt, time.Now().UTC().Format(time.RFC3339))

	// Build a compact summary of recently completed tasks for the PM.
	type taskSummary struct {
		Title       string `json:"title"`
		AssignedRole string `json:"assigned_role"`
	}
	summaries := make([]taskSummary, 0, len(recentTasks))
	for _, t := range recentTasks {
		summaries = append(summaries, taskSummary{Title: t.Title, AssignedRole: t.AssignedRole})
	}

	stats, _ := p.tasks.Stats()
	latestRelease, _ := p.releases.Latest()

	currentVersion := "none"
	if latestRelease != nil {
		currentVersion = latestRelease.Version
	}

	projectState := map[string]any{
		"direction":                 p.direction,
		"current_version":           currentVersion,
		"task_stats":                stats,
		"tasks_completed_this_cycle": summaries,
	}

	ready, version, notes, err := p.pm.CheckRelease(ctx, projectState)
	if err != nil {
		p.emit("release.check_error", map[string]any{"error": err.Error()})
		return err
	}

	if !ready {
		p.emit("release.check_deferred", map[string]any{
			"next_check_after_tasks": p.releaseInterval,
		})
		return nil
	}

	return p.createRelease(ctx, version, notes)
}

func (p *Pipeline) createRelease(ctx context.Context, version, notes string) error {
	// Normalize: ensure the version directory starts with "v".
	dirVersion := version
	if !strings.HasPrefix(dirVersion, "v") {
		dirVersion = "v" + dirVersion
	}

	releaseDir := filepath.Join(p.artifacts, "releases", dirVersion)
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		return fmt.Errorf("create release dir: %w", err)
	}

	if err := snapshotArtifacts(p.artifacts, releaseDir); err != nil {
		return fmt.Errorf("snapshot artifacts: %w", err)
	}

	releaseDoc := fmt.Sprintf("# Release %s\n\nCreated: %s\n\n%s\n",
		dirVersion, time.Now().UTC().Format(time.RFC3339), notes)
	if err := os.WriteFile(filepath.Join(releaseDir, "RELEASE.md"), []byte(releaseDoc), 0644); err != nil {
		return fmt.Errorf("write RELEASE.md: %w", err)
	}

	rel := &model.Release{
		Version:   dirVersion,
		Notes:     notes,
		Path:      releaseDir,
		CreatedAt: time.Now(),
	}
	if err := p.releases.Save(rel); err != nil {
		return fmt.Errorf("save release record: %w", err)
	}

	p.emit("release.created", map[string]any{
		"version": dirVersion,
		"path":    releaseDir,
	})
	log.Printf("release created: %s at %s", dirVersion, releaseDir)
	return nil
}

// snapshotArtifacts copies everything under src into dst, skipping:
//   - the .buycott/ state directory
//   - the releases/ directory itself (to avoid recursive copies)
func snapshotArtifacts(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		// Skip excluded top-level directories.
		top := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if top == ".buycott" || top == "releases" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dstPath := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		return copyFile(path, dstPath, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
