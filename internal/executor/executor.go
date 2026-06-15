package executor

import (
	"context"

	"buycott/internal/model"
)

type Executor interface {
	Run(ctx context.Context, image string, commands []string, artifactsPath string) (model.ExecResult, error)
}
