package sandbox

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

const (
	sandboxUID     = 1001 // Matches the sandbox user created in executor images.
	sandboxGID     = 1001
	maxOutputBytes = 64 * 1024
)

// containerManager wraps the Docker client to create/start/exec/remove sandbox containers.
type containerManager struct {
	client client.APIClient
}

func newContainerManager(client client.APIClient) *containerManager {
	return &containerManager{client: client}
}

// create builds and creates a sandbox container. It does not start it.
func (m *containerManager) create(ctx context.Context, image, sourcePath, sandboxPath string, res Resources) (string, error) {
	hostCfg := hostConfig(res)
	hostCfg.Mounts = []mount.Mount{
		{
			Type:     mount.TypeBind,
			Source:   sourcePath,
			Target:   sandboxPath,
			ReadOnly: true,
		},
	}

	cfg := &container.Config{
		Image: image,
		// Keep the container alive so multiple execs can reuse it.
		Entrypoint: []string{"tail", "-f", "/dev/null"},
		Cmd:        []string{},
		User:       fmt.Sprintf("%d:%d", sandboxUID, sandboxGID),
		WorkingDir: "/sandbox",
	}

	createResp, err := m.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     cfg,
		HostConfig: hostCfg,
	})
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}
	return createResp.ID, nil
}

// start starts an already-created container.
func (m *containerManager) start(ctx context.Context, id string) error {
	if _, err := m.client.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start container: %w", err)
	}
	return nil
}

// remove force-removes a container. Errors are ignored so cleanup always happens.
func (m *containerManager) remove(ctx context.Context, id string) {
	_, _ = m.client.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
}

// exec runs the given command inside a running container once, feeding input via
// stdin and capturing stdout/stderr (capped at maxOutputBytes each).
func (m *containerManager) exec(ctx context.Context, id string, cmd []string, input string, timeout time.Duration) (TestResult, error) {
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	execCfg := client.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          false,
	}

	createResp, err := m.client.ExecCreate(execCtx, id, execCfg)
	if err != nil {
		return TestResult{}, fmt.Errorf("create exec: %w", err)
	}

	attachResp, err := m.client.ExecAttach(execCtx, createResp.ID, client.ExecAttachOptions{})
	if err != nil {
		return TestResult{}, fmt.Errorf("attach exec: %w", err)
	}
	defer attachResp.Close()

	// Write test input to the exec's stdin on a separate goroutine.
	inputErrCh := make(chan error, 1)
	go func() {
		defer func() { _ = attachResp.CloseWrite() }()
		_, err := io.WriteString(attachResp.Conn, input)
		inputErrCh <- err
	}()

	var stdoutBuf, stderrBuf bytes.Buffer
	stdoutLimit := &limitWriter{w: &stdoutBuf, n: maxOutputBytes}
	stderrLimit := &limitWriter{w: &stderrBuf, n: maxOutputBytes}

	// Demux stdout/stderr in the background while watching for timeout.
	copyDone := make(chan error, 1)
	start := time.Now()
	go func() {
		copyDone <- demuxOutput(attachResp.Reader, stdoutLimit, stderrLimit)
	}()

	select {
	case <-execCtx.Done():
		return TestResult{
			Duration:          time.Since(start),
			TimeLimitExceeded: true,
		}, nil
	case err := <-copyDone:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return TestResult{}, fmt.Errorf("copy output: %w", err)
		}
	}

	// Drain the input writer goroutine so it does not leak.
	<-inputErrCh

	inspect, err := m.client.ExecInspect(context.Background(), createResp.ID, client.ExecInspectOptions{})
	if err != nil {
		return TestResult{}, fmt.Errorf("inspect exec: %w", err)
	}

	return TestResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: inspect.ExitCode,
		Duration: time.Since(start),
	}, nil
}

// limitWriter drops writes after n bytes have been written, silently truncating
// the stream without returning an error.
type limitWriter struct {
	w io.Writer
	n int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.w.Write(p)
	l.n -= int64(n)
	return len(p), err
}

// streamType is the Docker multiplexed stream header byte.
type streamType byte

const (
	stdoutStream streamType = 1
	stderrStream streamType = 2
)

// demuxOutput reads Docker's multiplexed attach stream and copies stdout frames
// to out and stderr frames to errOut. Each frame is an 8-byte header
// (stream type, padding, 4-byte big-endian length) followed by payload.
func demuxOutput(r io.Reader, out, errOut io.Writer) error {
	hdr := make([]byte, 8)
	for {
		_, err := io.ReadFull(r, hdr)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		typ := streamType(hdr[0])
		length := binary.BigEndian.Uint32(hdr[4:])
		if length == 0 {
			continue
		}

		var w io.Writer
		switch typ {
		case stdoutStream:
			w = out
		case stderrStream:
			w = errOut
		default:
			// Unknown stream type; discard the payload.
			if _, err := io.CopyN(io.Discard, r, int64(length)); err != nil {
				return err
			}
			continue
		}

		if _, err := io.CopyN(w, r, int64(length)); err != nil {
			return err
		}
	}
}
