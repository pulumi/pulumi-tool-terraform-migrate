package pulumix

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
)

type NewTempStackOptions struct {
	ProjectName string
	StackName   string
	TempDir     string
	Runtime     string
}

func NewTempStack(ctx context.Context, opts NewTempStackOptions) (auto.Stack, error) {
	// Create the Pulumi.yaml project file
	project := workspace.Project{
		Name:    tokens.PackageName(opts.ProjectName),
		Runtime: workspace.NewProjectRuntimeInfo(opts.Runtime, nil),
	}

	// Initialize a new stack with file-based state backend
	// Configure the workspace to use a local filestate backend in the temp directory
	stateDir := filepath.Join(opts.TempDir, ".pulumi")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		os.RemoveAll(opts.TempDir)
		return auto.Stack{}, fmt.Errorf("failed to create state directory: %w", err)
	}

	return auto.NewStackLocalSource(ctx, "dev", opts.TempDir,
		auto.Project(project),
		auto.EnvVars(map[string]string{
			"PULUMI_BACKEND_URL":       fmt.Sprintf("file://%s", stateDir),
			"PULUMI_CONFIG_PASSPHRASE": "test",
		}),
	)
}
