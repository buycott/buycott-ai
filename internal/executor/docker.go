package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"buycott/internal/model"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
)

type DockerExecutor struct {
	cli          *client.Client
	dockerSocket string
	// artifactsVolume, when set, is mounted as a named Docker volume into
	// ephemeral containers instead of bind-mounting the artifacts path. This is
	// required under socket-forwarding, where the host daemon would otherwise
	// resolve a bind source as a host path that doesn't exist inside the daemon.
	artifactsVolume string
}

func NewDockerExecutor(dockerSocket, artifactsVolume string) (*DockerExecutor, error) {
	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+dockerSocket),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &DockerExecutor{cli: cli, dockerSocket: dockerSocket, artifactsVolume: artifactsVolume}, nil
}

func (e *DockerExecutor) Run(ctx context.Context, img string, commands []string, artifactsPath string) (model.ExecResult, error) {
	start := time.Now()

	// Pull the image if not present (best-effort; if pull fails, try anyway)
	pullOut, err := e.cli.ImagePull(ctx, img, image.PullOptions{})
	if err == nil {
		_, _ = io.Copy(io.Discard, pullOut)
		pullOut.Close()
	}

	script := strings.Join(commands, "\n")

	// Choose how to share /artifacts with the ephemeral container. Under
	// socket-forwarding the host daemon interprets bind sources as host paths,
	// so a named volume is used when configured; otherwise we bind the path.
	artifactsMount := mount.Mount{
		Type:   mount.TypeBind,
		Source: artifactsPath,
		Target: "/artifacts",
	}
	if e.artifactsVolume != "" {
		artifactsMount = mount.Mount{
			Type:   mount.TypeVolume,
			Source: e.artifactsVolume,
			Target: "/artifacts",
		}
	}

	resp, err := e.cli.ContainerCreate(ctx,
		&container.Config{
			Image: img,
			Cmd:   []string{"/bin/sh", "-c", script},
		},
		&container.HostConfig{
			Mounts: []mount.Mount{
				artifactsMount,
				{
					Type:   mount.TypeBind,
					Source: e.dockerSocket,
					Target: "/var/run/docker.sock",
				},
			},
			AutoRemove: false,
		},
		nil, nil, "",
	)
	if err != nil {
		return model.ExecResult{ExitCode: -1, Duration: time.Since(start)},
			fmt.Errorf("container create: %w", err)
	}

	id := resp.ID
	defer func() {
		_ = e.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
	}()

	if err := e.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return model.ExecResult{ExitCode: -1, Duration: time.Since(start)},
			fmt.Errorf("container start: %w", err)
	}

	statusCh, errCh := e.cli.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	var exitCode int64
	select {
	case res := <-statusCh:
		exitCode = res.StatusCode
	case err := <-errCh:
		return model.ExecResult{ExitCode: -1, Duration: time.Since(start)},
			fmt.Errorf("container wait: %w", err)
	}

	logReader, err := e.cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return model.ExecResult{ExitCode: int(exitCode), Duration: time.Since(start)},
			fmt.Errorf("container logs: %w", err)
	}
	defer logReader.Close()

	var stdout, stderr bytes.Buffer
	if err := demuxLogs(logReader, &stdout, &stderr); err != nil {
		return model.ExecResult{ExitCode: int(exitCode), Duration: time.Since(start)},
			fmt.Errorf("demux logs: %w", err)
	}

	return model.ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: int(exitCode),
		Duration: time.Since(start),
	}, nil
}

// demuxLogs splits Docker's multiplexed log stream into stdout and stderr.
// Docker log format: 8-byte header [stream_type(1) | 0(3) | size(4)] followed by payload.
func demuxLogs(r io.Reader, stdout, stderr io.Writer) error {
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}
		size := int64(hdr[4])<<24 | int64(hdr[5])<<16 | int64(hdr[6])<<8 | int64(hdr[7])
		var dst io.Writer
		switch hdr[0] {
		case 1:
			dst = stdout
		case 2:
			dst = stderr
		default:
			dst = io.Discard
		}
		if _, err := io.CopyN(dst, r, size); err != nil {
			return err
		}
	}
}
