package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

func newTestClient(t *testing.T) client.APIClient {
	t.Helper()
	c, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client not available: %v", err)
	}
	return c
}

// newTestRunner creates a Runner and fails the test if Docker is not reachable.
func newTestRunner(t *testing.T) *Runner {
	t.Helper()
	c := newTestClient(t)
	ctx := context.Background()
	if _, err := c.Ping(ctx, client.PingOptions{}); err != nil {
		t.Fatalf("docker daemon not reachable: %v", err)
	}
	return NewRunner(c)
}

func TestRunner_HappyPath(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t)

	tests := []struct {
		name        string
		image       string
		sandboxPath string
		compileCmd  []string
		runCmd      []string
		source      string
		input       string
		want        string
	}{
		{
			name:        "python",
			image:       "codearena/executor-python:latest",
			sandboxPath: "/sandbox/main.py",
			runCmd:      []string{"python3", "/sandbox/main.py"},
			source:      "print('echo:', input())",
			input:       "hello\n",
			want:        "echo: hello",
		},
		{
			name:        "javascript",
			image:       "codearena/executor-javascript:latest",
			sandboxPath: "/sandbox/main.js",
			runCmd:      []string{"node", "/sandbox/main.js"},
			source:      "const data = require('fs').readFileSync(0, 'utf-8'); process.stdout.write('echo: ' + data.trim());",
			input:       "hello\n",
			want:        "echo: hello",
		},
		{
			name:        "go",
			image:       "codearena/executor-go:latest",
			sandboxPath: "/sandbox/main.go",
			compileCmd:  []string{"go", "build", "-o", "/sandbox/main", "/sandbox/main.go"},
			runCmd:      []string{"/sandbox/main"},
			source: `package main
import (
	"bufio"
	"fmt"
	"os"
)
func main() {
	s, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Print("echo: ", s)
}`,
			input: "hello\n",
			want:  "echo: hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := runner.Run(ctx, RunRequest{
				Image:       tt.image,
				SandboxPath: tt.sandboxPath,
				CompileCmd:  tt.compileCmd,
				RunCmd:      tt.runCmd,
				SourceCode:  tt.source,
				TestCases:   []TestCase{{Input: tt.input}},
				Timeout:     30 * time.Second,
				Resources: Resources{
					MemoryMB:  512,
					CPU:       1.0,
					PidsLimit: 256,
				},
			})
			if err != nil {
				t.Fatalf("run failed: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}
			if results[0].ExitCode != 0 {
				t.Fatalf("expected exit code 0, got %d, stderr: %s", results[0].ExitCode, results[0].Stderr)
			}
			got := strings.TrimSpace(results[0].Stdout)
			want := strings.TrimSpace(tt.want)
			if got != want {
				t.Fatalf("stdout mismatch: got %q, want %q", got, want)
			}
		})
	}
}

// defaultPythonRequest returns a baseline RunRequest for the Python image.
// Callers override SourceCode, Timeout, and Resources as needed.
func defaultPythonRequest(source string) RunRequest {
	return RunRequest{
		Image:       "codearena/executor-python:latest",
		SandboxPath: "/sandbox/main.py",
		RunCmd:      []string{"python3", "/sandbox/main.py"},
		SourceCode:  source,
		TestCases:   []TestCase{{Input: ""}},
		Timeout:     10 * time.Second,
		Resources: Resources{
			MemoryMB:  64,
			CPU:       1.0,
			PidsLimit: 64,
		},
	}
}

// requireSingleResult checks that exactly one result was returned and returns it.
func requireSingleResult(t *testing.T, results []TestResult, err error) TestResult {
	t.Helper()
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	return results[0]
}

func TestHostConfig(t *testing.T) {
	res := Resources{MemoryMB: 128, CPU: 0.5, PidsLimit: 32}
	cfg := hostConfig(res)

	if cfg.NetworkMode != "none" {
		t.Errorf("NetworkMode = %q, want none", cfg.NetworkMode)
	}
	if !cfg.ReadonlyRootfs {
		t.Error("ReadonlyRootfs = false, want true")
	}
	wantMemory := int64(128 * 1024 * 1024)
	if cfg.Memory != wantMemory {
		t.Errorf("Memory = %d, want %d", cfg.Memory, wantMemory)
	}
	if cfg.MemorySwap != wantMemory {
		t.Errorf("MemorySwap = %d, want %d (equal to memory to disable swap)", cfg.MemorySwap, wantMemory)
	}
	wantNanoCPUs := int64(0.5 * 1e9)
	if cfg.NanoCPUs != wantNanoCPUs {
		t.Errorf("NanoCPUs = %d, want %d", cfg.NanoCPUs, wantNanoCPUs)
	}
	if cfg.PidsLimit == nil {
		t.Fatal("PidsLimit is nil")
	}
	if *cfg.PidsLimit != 32 {
		t.Errorf("PidsLimit = %d, want 32", *cfg.PidsLimit)
	}
	if len(cfg.CapDrop) != 1 || cfg.CapDrop[0] != "ALL" {
		t.Errorf("CapDrop = %v, want [ALL]", cfg.CapDrop)
	}
	if len(cfg.SecurityOpt) != 1 || cfg.SecurityOpt[0] != "no-new-privileges:true" {
		t.Errorf("SecurityOpt = %v, want [no-new-privileges:true]", cfg.SecurityOpt)
	}
	if cfg.Tmpfs == nil {
		t.Fatal("Tmpfs is nil")
	}
	if cfg.Tmpfs["/tmp"] == "" {
		t.Error("missing /tmp tmpfs mount")
	}
	if cfg.Tmpfs["/sandbox"] == "" {
		t.Error("missing /sandbox tmpfs mount")
	}
}

func Test_Sandbox_TimeoutSecurity(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t)

	req := defaultPythonRequest("while True:\n    pass\n")
	req.Timeout = 2 * time.Second

	results, err := runner.Run(ctx, req)
	res := requireSingleResult(t, results, err)
	if !res.TimeLimitExceeded {
		t.Fatalf("expected TimeLimitExceeded, got exit code %d, duration %v, stderr: %s", res.ExitCode, res.Duration, res.Stderr)
	}
}

func Test_Sandbox_ForkBombSecurity(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t)

	// Spawn far more concurrent children than PidsLimit allows. Children sleep
	// so their PIDs stay occupied; the parent counts how many it managed to
	// create before hitting the cgroup limit.
	req := defaultPythonRequest(`import os, sys, time
children = []
try:
    for i in range(200):
        pid = os.fork()
        if pid == 0:
            time.sleep(10)
            sys.exit(0)
        children.append(pid)
except OSError:
    pass
if len(children) > 100:
    sys.exit(1)
`)
	req.Timeout = 5 * time.Second

	results, err := runner.Run(ctx, req)
	res := requireSingleResult(t, results, err)
	if res.ExitCode != 0 {
		t.Fatalf("fork bomb containment failed: exit code %d, stderr: %s", res.ExitCode, res.Stderr)
	}
}

func Test_Sandbox_NetworkSecurity(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t)

	req := defaultPythonRequest(`import socket
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.settimeout(2)
s.connect(("8.8.8.8", 53))
`)
	results, err := runner.Run(ctx, req)
	res := requireSingleResult(t, results, err)
	if res.ExitCode == 0 {
		t.Fatal("network connection succeeded; expected failure under NetworkMode none")
	}
	stderr := strings.ToLower(res.Stderr)
	if !strings.Contains(stderr, "unreachable") && !strings.Contains(stderr, "no route to host") && !strings.Contains(stderr, "name or service not known") {
		t.Fatalf("expected network-unreachable error, got stderr: %s", res.Stderr)
	}
}

func Test_Sandbox_FilesystemSecurity(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t)

	// Attempt to write to the read-only root filesystem.
	req := defaultPythonRequest(`with open("/etc/pwned.txt", "w") as f:
    f.write("pwned")
`)
	results, err := runner.Run(ctx, req)
	res := requireSingleResult(t, results, err)
	if res.ExitCode == 0 {
		t.Fatal("write to rootfs succeeded; expected read-only filesystem")
	}
	if !strings.Contains(res.Stderr, "Read-only file system") && !strings.Contains(res.Stderr, "Permission denied") {
		t.Fatalf("expected read-only filesystem error, got stderr: %s", res.Stderr)
	}
}

func Test_Sandbox_MemorySecurity(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t)

	// Allocate 1 MB strings until the cgroup memory limit is hit.
	req := defaultPythonRequest(`a = []
while True:
    a.append("x" * (1024 * 1024))
`)
	results, err := runner.Run(ctx, req)
	res := requireSingleResult(t, results, err)
	if res.ExitCode == 0 {
		t.Fatalf("memory-hog exited cleanly; expected OOM kill, got stderr: %s", res.Stderr)
	}
}
