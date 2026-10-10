package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/moby/moby/client"
)

// Runner orchestrates sandbox container execution for a single submission.
type Runner struct {
	client client.APIClient
	mgr    *containerManager
}

// NewRunner creates a Runner from an existing Docker API client.
func NewRunner(client client.APIClient) *Runner {
	return &Runner{
		client: client,
		mgr:    newContainerManager(client),
	}
}

// TestCase describes one test input to run against the submitted code.
type TestCase struct {
	Input string
}

// TestResult captures the raw output of running one test case inside the sandbox.
// The worker compares Stdout with the expected output to determine the verdict.
type TestResult struct {
	Stdout            string
	Stderr            string
	ExitCode          int
	Duration          time.Duration
	TimeLimitExceeded bool
}

// RunRequest describes a single submission run.
type RunRequest struct {
	Image       string
	SandboxPath string   // Target path inside the container for the source file.
	CompileCmd  []string // Optional; executed once before test cases.
	RunCmd      []string // Executed once per test case with stdin feeding.
	SourceCode  string
	TestCases   []TestCase
	Timeout     time.Duration
	Resources   Resources
}

// Run creates a fresh sandbox container, compiles the source once if a compile
// command is provided, executes every test case sequentially, and removes the
// container. It returns partial results if a test case times out.
func (r *Runner) Run(ctx context.Context, req RunRequest) ([]TestResult, error) {
	tmpDir, err := os.MkdirTemp("", "codearena-sandbox-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	sourcePath := filepath.Join(tmpDir, filepath.Base(req.SandboxPath))
	// World-readable so the sandbox user can read the bind-mounted
	// source file regardless of the host user's UID.
	// #nosec G306
	if err := os.WriteFile(sourcePath, []byte(req.SourceCode), 0444); err != nil {
		return nil, fmt.Errorf("write source file: %w", err)
	}

	cid, err := r.mgr.create(ctx, req.Image, sourcePath, req.SandboxPath, req.Resources)
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}
	defer r.mgr.remove(context.WithoutCancel(ctx), cid)

	if err := r.mgr.start(ctx, cid); err != nil {
		return nil, fmt.Errorf("start sandbox: %w", err)
	}

	// Compile once, then reuse the artifact for every test case.
	if len(req.CompileCmd) > 0 {
		compileRes, err := r.mgr.exec(ctx, cid, req.CompileCmd, "", req.Timeout)
		if err != nil {
			return nil, fmt.Errorf("compile: %w", err)
		}
		if compileRes.ExitCode != 0 {
			return nil, fmt.Errorf("compile failed: %s", compileRes.Stderr)
		}
	}

	results := make([]TestResult, 0, len(req.TestCases))
	for _, tc := range req.TestCases {
		res, err := r.mgr.exec(ctx, cid, req.RunCmd, tc.Input, req.Timeout)
		if err != nil {
			return results, fmt.Errorf("exec test case: %w", err)
		}
		results = append(results, res)
		if res.TimeLimitExceeded {
			return results, nil
		}
	}
	return results, nil
}
