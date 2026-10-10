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
		t.Skipf("docker client not available: %v", err)
	}
	return c
}

func TestRunner_HappyPath(t *testing.T) {
	ctx := context.Background()

	c := newTestClient(t)
	if _, err := c.Ping(ctx, client.PingOptions{}); err != nil {
		t.Skipf("docker daemon not reachable: %v", err)
	}

	runner := NewRunner(c)

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
