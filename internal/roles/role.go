package roles

import (
	"context"

	"buycott/internal/model"
)

type TaskOutput struct {
	Narrative   string
	Files       map[string]string
	RunCommands []string
	RunImage    string
	// SubTask, when non-nil, asks the pipeline to spawn a blocking sub-task
	// assigned to the given role. The current task pauses until the sub-task
	// completes, then resumes with the sub-task output injected into context.
	SubTask *model.SubTaskRequest
}

type Role interface {
	Name() string
	SystemPrompt() string
	ProcessTask(ctx context.Context, task *model.Task) (TaskOutput, error)
}

// PMRole extends Role with PM-specific capabilities.
type PMRole interface {
	Role
	GenerateTasks(ctx context.Context, direction string, projectState map[string]any) ([]*model.Task, error)
	ReviewTask(ctx context.Context, task *model.Task, result model.ExecResult) (approved bool, feedback string, err error)
	// CheckRelease asks the PM whether the project is ready to cut a release.
	// Returns ready=true plus a semver version string and release notes when approved.
	CheckRelease(ctx context.Context, projectState map[string]any) (ready bool, version string, notes string, err error)
}

// ReviewerRole extends Role with the ability to review completed engineer work.
// The pipeline calls ReviewCode after each implementation attempt; engineers
// must address all change requests before the work proceeds to the PM.
type ReviewerRole interface {
	Role
	ReviewCode(ctx context.Context, task *model.Task, output TaskOutput, result model.ExecResult) (approved bool, feedback string, err error)
}

// SecurityReviewerRole runs after the code-review loop. It receives static-
// analysis scan results collected by the pipeline (via the executor) alongside
// the task context so the LLM can synthesise tool findings with deeper
// contextual analysis. Returning approved=false re-queues the task with the
// findings injected into the engineer's conversation history.
type SecurityReviewerRole interface {
	Role
	ReviewSecurity(
		ctx context.Context,
		task *model.Task,
		output TaskOutput,
		execResult model.ExecResult,
		scanResults []model.ScanResult,
	) (approved bool, findings string, err error)
}

type Registry struct {
	roles map[string]Role
}

func NewRegistry() *Registry {
	return &Registry{roles: make(map[string]Role)}
}

func (r *Registry) Register(role Role) {
	r.roles[role.Name()] = role
}

func (r *Registry) Get(name string) (Role, bool) {
	role, ok := r.roles[name]
	return role, ok
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.roles))
	for n := range r.roles {
		names = append(names, n)
	}
	return names
}
