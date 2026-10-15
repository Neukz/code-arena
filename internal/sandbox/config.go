package sandbox

import (
	"github.com/moby/moby/api/types/container"
)

// Resources describes the resource limits applied to a sandbox container.
type Resources struct {
	MemoryMB  int
	CPU       float64
	PidsLimit int
}

// hostConfig builds a locked-down Docker HostConfig for sandbox execution.
// The caller is expected to add any bind-mounts (e.g. the source code file)
// to the Mounts field before creating the container.
func hostConfig(res Resources) *container.HostConfig {
	pidsLimit := int64(res.PidsLimit)
	return &container.HostConfig{
		NetworkMode:    "none",
		ReadonlyRootfs: true,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges:true"},
		Tmpfs: map[string]string{
			"/tmp":     "noexec,nosuid,size=128m",
			"/sandbox": "uid=1001,gid=1001,mode=0700,exec,size=512m",
		},
		Memory:     int64(res.MemoryMB) * 1024 * 1024,
		MemorySwap: int64(res.MemoryMB) * 1024 * 1024, // Equal to memory disables swap.
		NanoCPUs:   int64(res.CPU * 1e9),
		PidsLimit:  &pidsLimit,
	}
}
