package vuln

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"emperror.dev/errors"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	networktypes "github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/types/v2/vulnerability"
)

func TestDecodeTrivyReportFromFileInternal_LargePayload(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "trivy-report-*.json")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Remove(tmpFile.Name()))
	}()
	defer func() {
		require.NoError(t, tmpFile.Close())
	}()

	const vulnCount = 4000
	vulns := make([]vulnerability.TrivyVulnerability, 0, vulnCount)
	for i := range vulnCount {
		vulns = append(vulns, vulnerability.TrivyVulnerability{
			VulnerabilityID:  fmt.Sprintf("CVE-2026-%04d", i),
			PkgName:          fmt.Sprintf("pkg-%d", i),
			InstalledVersion: "1.0.0",
			FixedVersion:     "1.0.1",
			Severity:         "HIGH",
		})
	}

	report := vulnerability.TrivyReport{
		SchemaVersion: 2,
		ArtifactName:  "example/image:latest",
		ArtifactType:  "container_image",
		Results: []vulnerability.TrivyResults{
			{
				Target:          "alpine:3.20",
				Class:           "os-pkgs",
				Type:            "alpine",
				Vulnerabilities: vulns,
			},
		},
	}

	encoder := json.NewEncoder(tmpFile)
	require.NoError(t, encoder.Encode(report))

	decoded, err := DecodeReportFromFile(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	require.Equal(t, report.ArtifactName, decoded.ArtifactName)
	require.Len(t, decoded.Results, 1)
	require.Len(t, decoded.Results[0].Vulnerabilities, vulnCount)
}

func TestDecodeTrivyReportFromFileInternal_RecoversFromPrefixedAndTrailingNoise(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "trivy-report-noisy-*.json")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Remove(tmpFile.Name()))
	}()
	defer func() {
		require.NoError(t, tmpFile.Close())
	}()

	report := vulnerability.TrivyReport{
		SchemaVersion: 2,
		ArtifactName:  "ghcr.io/getarcaneapp/example:latest",
		ArtifactType:  "container_image",
		Results: []vulnerability.TrivyResults{
			{Target: "alpine:3.20", Class: "os-pkgs", Type: "alpine"},
		},
	}

	reportBytes, err := json.Marshal(report)
	require.NoError(t, err)

	_, err = tmpFile.WriteString("WARN scanner warming cache\n")
	require.NoError(t, err)
	_, err = tmpFile.Write(reportBytes)
	require.NoError(t, err)
	_, err = tmpFile.WriteString("\nINFO scan completed\n")
	require.NoError(t, err)

	decoded, err := DecodeReportFromFile(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	require.Equal(t, report.ArtifactName, decoded.ArtifactName)
	require.Equal(t, report.SchemaVersion, decoded.SchemaVersion)
}

func TestDecodeTrivyReportFromFileInternal_SelectsReportFromMultipleJSONObjects(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "trivy-report-multi-json-*.json")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Remove(tmpFile.Name()))
	}()
	defer func() {
		require.NoError(t, tmpFile.Close())
	}()

	report := vulnerability.TrivyReport{
		SchemaVersion: 2,
		ArtifactName:  "ghcr.io/getarcaneapp/example:stable",
		ArtifactType:  "container_image",
		Results: []vulnerability.TrivyResults{
			{Target: "debian:12", Class: "os-pkgs", Type: "debian"},
		},
	}

	reportBytes, err := json.Marshal(report)
	require.NoError(t, err)

	_, err = tmpFile.WriteString(`{"action":"vulnerability_scan","success":false}` + "\n")
	require.NoError(t, err)
	_, err = tmpFile.Write(reportBytes)
	require.NoError(t, err)

	decoded, err := DecodeReportFromFile(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	require.Equal(t, report.ArtifactName, decoded.ArtifactName)
	require.Equal(t, report.SchemaVersion, decoded.SchemaVersion)
}

func TestDecodeTrivyReportFromFileInternal_HandlesConcatenatedJSONObjects(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "trivy-report-concatenated-*.json")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Remove(tmpFile.Name()))
	}()
	defer func() {
		require.NoError(t, tmpFile.Close())
	}()

	report := vulnerability.TrivyReport{
		SchemaVersion: 2,
		ArtifactName:  "ghcr.io/getarcaneapp/example:concat",
		ArtifactType:  "container_image",
		Results: []vulnerability.TrivyResults{
			{Target: "ubuntu:24.04", Class: "os-pkgs", Type: "ubuntu"},
		},
	}

	reportBytes, err := json.Marshal(report)
	require.NoError(t, err)

	_, err = tmpFile.Write(reportBytes)
	require.NoError(t, err)
	_, err = tmpFile.WriteString(`{"message":"done"}`)
	require.NoError(t, err)

	decoded, err := DecodeReportFromFile(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	require.Equal(t, report.ArtifactName, decoded.ArtifactName)
	require.Equal(t, report.SchemaVersion, decoded.SchemaVersion)
}

func TestCleanupTrivyLogTempFilesInternal_RemovesFiles(t *testing.T) {
	stdoutFile, err := os.CreateTemp("", "trivy-stdout-test-*")
	require.NoError(t, err)
	stderrFile, err := os.CreateTemp("", "trivy-stderr-test-*")
	require.NoError(t, err)

	stdoutPath := stdoutFile.Name()
	stderrPath := stderrFile.Name()

	_, err = stdoutFile.WriteString("stdout")
	require.NoError(t, err)
	_, err = stderrFile.WriteString("stderr")
	require.NoError(t, err)

	CleanupLogTempFiles(context.Background(), stdoutFile, stderrFile)

	_, err = os.Stat(stdoutPath)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))

	_, err = os.Stat(stderrPath)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func TestBuildTrivyHostConfig_ExcludesResourcesWhenLimitsDisabled(t *testing.T) {
	resources := containertypes.Resources{
		NanoCPUs:   int64(2_000_000_000),
		Memory:     int64(1_073_741_824),
		MemorySwap: int64(1_073_741_824),
	}

	hostConfig := BuildHostConfig(
		"cache-volume",
		nil,
		resources,
		"",
		false,
		RuntimeOptions{NetworkMode: "bridge"},
		nil,
		false,
	)
	require.Equal(t, containertypes.NetworkMode("bridge"), hostConfig.NetworkMode)
	require.Equal(t, int64(0), hostConfig.NanoCPUs)
	require.Equal(t, int64(0), hostConfig.Memory)
	require.Equal(t, int64(0), hostConfig.MemorySwap)
	require.Empty(t, hostConfig.SecurityOpt)
	require.False(t, hostConfig.Privileged)
}

func TestBuildTrivyHostConfig_IncludesResourcesWhenLimitsEnabled(t *testing.T) {
	resources := containertypes.Resources{
		NanoCPUs:   int64(2_000_000_000),
		Memory:     int64(1_073_741_824),
		MemorySwap: int64(1_073_741_824),
	}

	hostConfig := BuildHostConfig(
		"cache-volume",
		nil,
		resources,
		"",
		true,
		RuntimeOptions{NetworkMode: "arcane-external"},
		nil,
		false,
	)
	require.Equal(t, containertypes.NetworkMode("arcane-external"), hostConfig.NetworkMode)
	require.Equal(t, resources.NanoCPUs, hostConfig.NanoCPUs)
	require.Equal(t, resources.Memory, hostConfig.Memory)
	require.Equal(t, resources.MemorySwap, hostConfig.MemorySwap)
}

func TestBuildTrivyHostConfig_UsesCPUSetAndClearsNanoCPUs(t *testing.T) {
	resources := containertypes.Resources{
		NanoCPUs:   int64(2_000_000_000),
		Memory:     int64(1_073_741_824),
		MemorySwap: int64(1_073_741_824),
	}

	hostConfig := BuildHostConfig(
		"cache-volume",
		nil,
		resources,
		"0-1",
		true,
		RuntimeOptions{NetworkMode: "bridge"},
		nil,
		false,
	)
	require.Equal(t, containertypes.NetworkMode("bridge"), hostConfig.NetworkMode)
	require.Equal(t, int64(0), hostConfig.NanoCPUs)
	require.Equal(t, "0-1", hostConfig.CpusetCpus)
	require.Equal(t, resources.Memory, hostConfig.Memory)
	require.Equal(t, resources.MemorySwap, hostConfig.MemorySwap)
}

func TestBuildTrivyHostConfig_AppliesRuntimeSecurity(t *testing.T) {
	hostConfig := BuildHostConfig(
		"cache-volume",
		nil,
		containertypes.Resources{},
		"",
		false,
		RuntimeOptions{NetworkMode: "bridge"},
		[]string{"label=disable", "label=type:container_runtime_t"},
		true,
	)

	require.Equal(t, []string{"label=disable", "label=type:container_runtime_t"}, hostConfig.SecurityOpt)
	require.True(t, hostConfig.Privileged)
}

func TestBuildTrivyHostConfig_SkipsCacheMountWhenVolumeEmpty(t *testing.T) {
	hostConfig := BuildHostConfig(
		"",
		nil,
		containertypes.Resources{},
		"",
		false,
		RuntimeOptions{NetworkMode: "bridge"},
		nil,
		false,
	)

	require.Empty(t, hostConfig.Mounts)

	socketMount := mounttypes.Mount{
		Type:   mounttypes.TypeBind,
		Source: "/run/user/1000/podman/podman.sock",
		Target: "/run/user/1000/podman/podman.sock",
	}
	hostConfig = BuildHostConfig(
		"",
		nil,
		containertypes.Resources{},
		"",
		false,
		RuntimeOptions{
			NetworkMode: "bridge",
			Mounts:      []mounttypes.Mount{socketMount},
		},
		nil,
		false,
	)

	require.Len(t, hostConfig.Mounts, 1)
	require.Equal(t, socketMount.Source, hostConfig.Mounts[0].Source)
}

func TestBuildTrivyBatchHostConfig_AppliesRuntimeSecurity(t *testing.T) {
	hostConfig := BuildBatchHostConfig(
		"cache-volume",
		RuntimeOptions{NetworkMode: "arcane-external"},
		[]string{"label=disable"},
		true,
	)

	require.Equal(t, containertypes.NetworkMode("arcane-external"), hostConfig.NetworkMode)
	require.Equal(t, []string{"label=disable"}, hostConfig.SecurityOpt)
	require.True(t, hostConfig.Privileged)
	require.Len(t, hostConfig.Mounts, 1)
	require.Equal(t, "cache-volume", hostConfig.Mounts[0].Source)
}

func TestBuildTrivyBatchHostConfig_SkipsCacheMountWhenVolumeEmpty(t *testing.T) {
	hostConfig := BuildBatchHostConfig(
		"",
		RuntimeOptions{NetworkMode: "arcane-external"},
		nil,
		false,
	)

	require.Empty(t, hostConfig.Mounts)
}

func TestBuildTrivyHostConfig_IncludesInheritedSocketMount(t *testing.T) {
	hostConfig := BuildHostConfig(
		"cache-volume",
		nil,
		containertypes.Resources{},
		"",
		false,
		RuntimeOptions{
			NetworkMode: "bridge",
			Mounts: []mounttypes.Mount{
				{
					Type:   mounttypes.TypeBind,
					Source: "/run/user/1000/podman/podman.sock",
					Target: "/run/user/1000/podman/podman.sock",
				},
			},
		},
		nil,
		false,
	)

	require.Len(t, hostConfig.Mounts, 2)
	require.Equal(t, "/run/user/1000/podman/podman.sock", hostConfig.Mounts[0].Source)
	require.Equal(t, "/run/user/1000/podman/podman.sock", hostConfig.Mounts[0].Target)
	require.Equal(t, "cache-volume", hostConfig.Mounts[1].Source)
}

func TestApplyTrivyRuntimeSecurityInternal(t *testing.T) {
	hostConfig := &containertypes.HostConfig{}

	ApplyRuntimeSecurity(hostConfig, []string{"label=disable"}, true)

	require.Equal(t, []string{"label=disable"}, hostConfig.SecurityOpt)
	require.True(t, hostConfig.Privileged)
}

func TestSelectDefaultTrivyNetworkModeInternal(t *testing.T) {
	require.Equal(t, DefaultNetworkMode, SelectDefaultNetworkMode(""))
	require.Equal(t, DefaultNetworkMode, SelectDefaultNetworkMode(" \t\n "))
	require.Equal(t, "arcane-external", SelectDefaultNetworkMode("arcane-external"))
}

func TestResolveTrivyUnixSocketSourceInternal(t *testing.T) {
	t.Run("uses resolved host path", func(t *testing.T) {
		source, err := ResolveUnixSocketSource(
			context.Background(),
			"/run/user/1000/podman/podman.sock",
			func(context.Context, string) (string, error) {
				return "/host/podman/podman.sock", nil
			},
			func() bool { return true },
		)

		require.NoError(t, err)
		require.Equal(t, "/host/podman/podman.sock", source)
	})

	t.Run("fails when socket cannot be resolved in docker", func(t *testing.T) {
		_, err := ResolveUnixSocketSource(
			context.Background(),
			"/run/user/1000/podman/podman.sock",
			func(context.Context, string) (string, error) {
				return "", nil
			},
			func() bool { return true },
		)

		require.Error(t, err)
		require.Contains(t, err.Error(), "daemon-visible host path")
	})

	t.Run("falls back to original path outside docker", func(t *testing.T) {
		source, err := ResolveUnixSocketSource(
			context.Background(),
			"/var/run/docker.sock",
			func(context.Context, string) (string, error) {
				return "", errors.New("not running in docker")
			},
			func() bool { return false },
		)

		require.NoError(t, err)
		require.Equal(t, "/var/run/docker.sock", source)
	})
}

func TestSelectTrivyAutoNetworkModeInternal(t *testing.T) {
	t.Run("prefers explicit host network mode", func(t *testing.T) {
		mode := SelectAutoNetworkMode(&containertypes.InspectResponse{
			HostConfig: &containertypes.HostConfig{NetworkMode: "host"},
		})

		require.Equal(t, "host", mode)
	})

	t.Run("prefers attached custom network over bridge", func(t *testing.T) {
		mode := SelectAutoNetworkMode(&containertypes.InspectResponse{
			HostConfig: &containertypes.HostConfig{NetworkMode: "bridge"},
			NetworkSettings: &containertypes.NetworkSettings{
				Networks: map[string]*networktypes.EndpointSettings{
					"bridge":          {},
					"arcane-internal": {},
				},
			},
		})

		require.Equal(t, "arcane-internal", mode)
	})

	t.Run("falls back to bridge when no custom network is attached", func(t *testing.T) {
		mode := SelectAutoNetworkMode(&containertypes.InspectResponse{
			HostConfig: &containertypes.HostConfig{NetworkMode: "bridge"},
			NetworkSettings: &containertypes.NetworkSettings{
				Networks: map[string]*networktypes.EndpointSettings{
					"bridge": {},
				},
			},
		})

		require.Equal(t, "bridge", mode)
	})
}

func TestIsSynologyDockerHostInternal(t *testing.T) {
	// DS220+ (Kernel 4.x): single-line
	require.True(t, IsSynologyDockerHost("Synology NAS"))
	// DS925+ (Kernel 5.x): OS field contains a newline before "(containerized)"
	require.True(t, IsSynologyDockerHost("Synology NAS\n (containerized)"))
	// Variation: Windows-style line ending
	require.True(t, IsSynologyDockerHost("Synology NAS\r\n (containerized)"))
	// Case-insensitive
	require.True(t, IsSynologyDockerHost("synology nas"))
	// Padded with spaces
	require.True(t, IsSynologyDockerHost("  Synology NAS  "))
	// Empty / non-Synology
	require.False(t, IsSynologyDockerHost(""))
	require.False(t, IsSynologyDockerHost("Ubuntu 24.04.1 LTS"))
	require.False(t, IsSynologyDockerHost("Ubuntu 24.04.1 LTS\n (containerized)"))
}

func TestBuildTrivyCPUSetInternal(t *testing.T) {
	require.Empty(t, BuildCPUSet(0, 4))
	require.Equal(t, "0", BuildCPUSet(1_000_000_000, 4))
	require.Equal(t, "0-1", BuildCPUSet(2_500_000_000, 4))
	require.Equal(t, "0-1", BuildCPUSet(3_000_000_000, 2))
	require.Equal(t, "0", BuildCPUSet(1_000_000_000, 0))
}

func TestTrivyMountSummariesInternal(t *testing.T) {
	require.Nil(t, MountSummaries(nil))

	hostConfig := &containertypes.HostConfig{
		Mounts: []mounttypes.Mount{
			{
				Type:   mounttypes.TypeBind,
				Source: "/var/run/docker.sock",
				Target: "/var/run/docker.sock",
			},
			{
				Type:   mounttypes.TypeVolume,
				Source: "arcane-trivy-cache",
				Target: "/root/.cache",
			},
			{
				Type:   mounttypes.TypeTmpfs,
				Target: "/tmp",
			},
		},
	}

	require.Equal(t,
		[]string{
			"/var/run/docker.sock:/var/run/docker.sock",
			"arcane-trivy-cache:/root/.cache",
			"tmpfs:/tmp",
		},
		MountSummaries(hostConfig),
	)
}

func TestTruncateTrivyLogOutputInternal(t *testing.T) {
	require.Empty(t, TruncateLogOutput("", 32))
	require.Equal(t, "keep me", TruncateLogOutput("  keep me  ", 32))

	longOutput := strings.Repeat("x", 80)
	truncated := TruncateLogOutput(longOutput, 16)
	require.Equal(t, strings.Repeat("x", 16)+" ...<truncated>", truncated)
}

func TestTrivyContainerWaitTimeoutInternal(t *testing.T) {
	require.Equal(t, timeouts.DefaultTrivyScan+timeouts.DefaultDockerAPI, ContainerWaitTimeout(""))
	require.Equal(t, 5*time.Minute+timeouts.DefaultDockerAPI, ContainerWaitTimeout("5m"))
	require.Equal(t, timeouts.DefaultTrivyScan+timeouts.DefaultDockerAPI, ContainerWaitTimeout("not-a-duration"))
	require.Equal(t, timeouts.DefaultTrivyScan+timeouts.DefaultDockerAPI, ContainerWaitTimeout("0s"))
}

func TestAwaitContainerWaitResponseInternal_Status(t *testing.T) {
	statusCh := make(chan containertypes.WaitResponse, 1)
	errCh := make(chan error)
	statusCh <- containertypes.WaitResponse{StatusCode: 12}

	status, err := AwaitContainerWaitResponse(context.Background(), statusCh, errCh)
	require.NoError(t, err)
	require.Equal(t, int64(12), status)
}

func TestAwaitContainerWaitResponseInternal_Error(t *testing.T) {
	statusCh := make(chan containertypes.WaitResponse)
	errCh := make(chan error, 1)
	errCh <- errors.New("boom")

	status, err := AwaitContainerWaitResponse(context.Background(), statusCh, errCh)
	require.Error(t, err)
	require.Contains(t, err.Error(), "boom")
	require.Equal(t, int64(0), status)
}

func TestAwaitContainerWaitResponseInternal_ClosedErrorChannelStillReadsStatus(t *testing.T) {
	statusCh := make(chan containertypes.WaitResponse, 1)
	errCh := make(chan error)

	close(errCh)
	statusCh <- containertypes.WaitResponse{StatusCode: 7}

	status, err := AwaitContainerWaitResponse(context.Background(), statusCh, errCh)
	require.NoError(t, err)
	require.Equal(t, int64(7), status)
}

func TestAwaitContainerWaitResponseInternal_ContextDone(t *testing.T) {
	statusCh := make(chan containertypes.WaitResponse)
	errCh := make(chan error)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	status, err := AwaitContainerWaitResponse(ctx, statusCh, errCh)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, int64(0), status)
}

func TestAwaitContainerWaitResponseInternal_NoStatus(t *testing.T) {
	statusCh := make(chan containertypes.WaitResponse)
	errCh := make(chan error)
	close(statusCh)
	close(errCh)

	status, err := AwaitContainerWaitResponse(context.Background(), statusCh, errCh)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ended without status")
	require.Equal(t, int64(0), status)
}
