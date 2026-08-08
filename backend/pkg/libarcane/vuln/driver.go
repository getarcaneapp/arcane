package vuln

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"
	cerrdefs "github.com/containerd/errdefs"
	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/moby/moby/api/pkg/stdcopy"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/types/v2/vulnerability"
)

const (
	// DefaultNetworkMode is the network the scan container joins when nothing
	// more specific is configured or detected.
	DefaultNetworkMode = "bridge"
	// CacheMountTarget is where the shared Trivy cache volume is mounted inside
	// the scan container.
	CacheMountTarget = "/root/.cache"
	// RegistryConfigDir is where the generated registry auth config lands inside
	// the scan container.
	RegistryConfigDir = "/tmp/arcane-registry-auth"

	// NanoCPUsPerCore and BytesPerMB convert configured limits to Docker units.
	NanoCPUsPerCore = int64(1_000_000_000)
	BytesPerMB      = int64(1024 * 1024)

	outputPathPrefixInternal       = "/tmp/arcane-trivy-result-"
	registryConfigCopyDestInternal = "/tmp"
	registryConfigTarNameInternal  = "arcane-registry-auth/config.json"
	errorExcerptSizeInternal       = int64(32 * 1024)
)

type RuntimeOptions struct {
	ContainerEnv []string
	Mounts       []mounttypes.Mount
	NetworkMode  string
}

func ResolveUnixSocketSource(
	ctx context.Context,
	socketPath string,
	discoverHostPath func(context.Context, string) (string, error),
	isRunningInDocker func() bool,
) (string, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return "", errors.New("unix docker host is missing a socket path")
	}

	if discoverHostPath != nil {
		hostPath, err := discoverHostPath(ctx, socketPath)
		if err == nil && strings.TrimSpace(hostPath) != "" {
			return strings.TrimSpace(hostPath), nil
		}
		if err != nil && (isRunningInDocker == nil || isRunningInDocker()) {
			return "", errors.WrapIff(err, "failed to resolve socket path %q", socketPath)
		}
	}

	if isRunningInDocker != nil && isRunningInDocker() {
		return "", errors.Errorf("failed to resolve socket path %q to a daemon-visible host path", socketPath)
	}

	return socketPath, nil
}

func SelectAutoNetworkMode(inspect *containertypes.InspectResponse) string {
	if inspect == nil {
		return DefaultNetworkMode
	}

	if inspect.HostConfig != nil {
		networkMode := strings.TrimSpace(string(inspect.HostConfig.NetworkMode))
		if networkMode != "" && networkMode != "default" && networkMode != DefaultNetworkMode &&
			!containertypes.NetworkMode(networkMode).IsContainer() {
			return networkMode
		}
	}

	if inspect.NetworkSettings != nil && len(inspect.NetworkSettings.Networks) > 0 {
		networkNames := make([]string, 0, len(inspect.NetworkSettings.Networks))
		for networkName := range inspect.NetworkSettings.Networks {
			networkName = strings.TrimSpace(networkName)
			if networkName != "" {
				networkNames = append(networkNames, networkName)
			}
		}
		sort.Strings(networkNames)

		for _, networkName := range networkNames {
			if !dockerutils.IsDefaultNetwork(networkName) {
				return networkName
			}
		}

		for _, networkName := range networkNames {
			if containertypes.NetworkMode(networkName).IsContainer() {
				continue
			}
			if networkName == "host" || networkName == "none" || networkName == DefaultNetworkMode {
				return networkName
			}
		}
	}

	if inspect.HostConfig != nil {
		networkMode := strings.TrimSpace(string(inspect.HostConfig.NetworkMode))
		if networkMode != "" && networkMode != "default" && !containertypes.NetworkMode(networkMode).IsContainer() {
			return networkMode
		}
	}

	return DefaultNetworkMode
}

func NewOutputPath() string {
	return outputPathPrefixInternal + strconv.FormatInt(time.Now().UnixNano(), 10) + ".json"
}

func CleanupTempFiles(ctx context.Context, tempFiles []string) {
	for _, f := range tempFiles {
		if err := os.Remove(f); err != nil {
			slog.WarnContext(ctx, "failed to remove trivy temp file", "path", f, "error", err)
		}
	}
}

func BuildBatchHostConfig(
	cacheVolume string,
	runtimeOptions RuntimeOptions,
	securityOpts []string,
	privileged bool,
) *containertypes.HostConfig {
	mounts := append([]mounttypes.Mount{}, runtimeOptions.Mounts...)
	if strings.TrimSpace(cacheVolume) != "" {
		mounts = append(mounts, mounttypes.Mount{
			Type:   mounttypes.TypeVolume,
			Source: cacheVolume,
			Target: CacheMountTarget,
		})
	}

	hostConfig := &containertypes.HostConfig{
		NetworkMode: containertypes.NetworkMode(SelectDefaultNetworkMode(runtimeOptions.NetworkMode)),
		Mounts:      mounts,
	}

	ApplyRuntimeSecurity(hostConfig, securityOpts, privileged)
	return hostConfig
}

func BuildHostConfig(
	cacheVolume string,
	tempFiles []string,
	resources containertypes.Resources,
	cpuSet string,
	applyLimits bool,
	runtimeOptions RuntimeOptions,
	securityOpts []string,
	privileged bool,
) *containertypes.HostConfig {
	mounts := append([]mounttypes.Mount{}, runtimeOptions.Mounts...)
	if strings.TrimSpace(cacheVolume) != "" {
		mounts = append(mounts, mounttypes.Mount{
			Type:   mounttypes.TypeVolume,
			Source: cacheVolume,
			Target: CacheMountTarget,
		})
	}

	hostConfig := &containertypes.HostConfig{
		NetworkMode: containertypes.NetworkMode(SelectDefaultNetworkMode(runtimeOptions.NetworkMode)),
		// Keep single-scan containers until explicit cleanup so we can reliably
		// wait for completion and collect logs across Docker variants.
		AutoRemove: false,
		Mounts:     mounts,
	}

	ApplyRuntimeSecurity(hostConfig, securityOpts, privileged)
	ApplyContainerResources(hostConfig, resources, cpuSet, applyLimits)

	addTempFileMountsInternal(hostConfig, tempFiles)
	return hostConfig
}

func SelectDefaultNetworkMode(networkMode string) string {
	networkMode = strings.TrimSpace(networkMode)
	if networkMode == "" {
		return DefaultNetworkMode
	}
	return networkMode
}

func ApplyRuntimeSecurity(hostConfig *containertypes.HostConfig, securityOpts []string, privileged bool) {
	if hostConfig == nil {
		return
	}

	if len(securityOpts) > 0 {
		hostConfig.SecurityOpt = append([]string(nil), securityOpts...)
	}
	hostConfig.Privileged = privileged
}

func ApplyContainerResources(hostConfig *containertypes.HostConfig, resources containertypes.Resources, cpuSet string, applyLimits bool) {
	if hostConfig == nil || !applyLimits {
		return
	}

	if cpuSet != "" {
		resources.NanoCPUs = 0
		resources.CpusetCpus = cpuSet
	}

	hostConfig.Resources = resources
}

func addTempFileMountsInternal(hostConfig *containertypes.HostConfig, tempFiles []string) {
	for _, tempFile := range tempFiles {
		switch {
		case strings.Contains(tempFile, "trivy-config"):
			hostConfig.Mounts = append(hostConfig.Mounts, mounttypes.Mount{
				Type:     mounttypes.TypeBind,
				Source:   tempFile,
				Target:   "/tmp/trivy-config.yaml",
				ReadOnly: true,
			})
		case strings.Contains(tempFile, "trivy-ignore"):
			hostConfig.Mounts = append(hostConfig.Mounts, mounttypes.Mount{
				Type:     mounttypes.TypeBind,
				Source:   tempFile,
				Target:   "/tmp/trivy-ignore",
				ReadOnly: true,
			})
		}
	}
}

func CreateLogTempFile(prefix string) (*os.File, error) {
	// Try the default system temp dir first.
	file, primaryErr := os.CreateTemp("", prefix)
	if primaryErr == nil {
		return file, nil
	}
	// /tmp is not writable (e.g. hardened container). Fall back to a
	// per-user cache directory so Trivy scans can still run.
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, errors.WrapIff(err, "failed to create trivy temp file (primary: %s) and user cache dir unavailable", primaryErr.Error())
	}
	fallbackDir := filepath.Join(cacheDir, "arcane", "trivy-tmp")
	if err := os.MkdirAll(fallbackDir, 0o700); err != nil {
		return nil, errors.WrapIff(err, "failed to create trivy fallback temp dir %s (primary: %s)", fallbackDir, primaryErr.Error())
	}
	fallbackFile, err := os.CreateTemp(fallbackDir, prefix)
	if err != nil {
		return nil, errors.WrapIff(err, "failed to create trivy temp file in fallback dir %s (primary: %s)", fallbackDir, primaryErr.Error())
	}
	return fallbackFile, nil
}

func CleanupLogTempFiles(ctx context.Context, files ...*os.File) {
	for _, file := range files {
		if file == nil {
			continue
		}

		path := file.Name()
		if err := file.Close(); err != nil {
			slog.WarnContext(ctx, "failed to close trivy temp file", "path", path, "error", err)
		}

		if path == "" {
			continue
		}

		if err := os.Remove(path); err != nil {
			slog.WarnContext(ctx, "failed to remove trivy temp file", "path", path, "error", err)
		}
	}
}

func MountSummaries(hostConfig *containertypes.HostConfig) []string {
	if hostConfig == nil || len(hostConfig.Mounts) == 0 {
		return nil
	}

	summaries := make([]string, 0, len(hostConfig.Mounts))
	for _, mount := range hostConfig.Mounts {
		source := mount.Source
		if source == "" {
			source = string(mount.Type)
		}
		summaries = append(summaries, fmt.Sprintf("%s:%s", source, mount.Target))
	}

	return summaries
}

func LogContainerStartupRequest(ctx context.Context, scope string, config *containertypes.Config, hostConfig *containertypes.HostConfig) {
	if config == nil {
		return
	}

	resources := containertypes.Resources{}
	autoRemove := false
	var mounts []string
	networkMode := ""
	var securityOpts []string
	privileged := false
	if hostConfig != nil {
		resources = hostConfig.Resources
		autoRemove = hostConfig.AutoRemove
		mounts = MountSummaries(hostConfig)
		networkMode = string(hostConfig.NetworkMode)
		securityOpts = append([]string(nil), hostConfig.SecurityOpt...)
		privileged = hostConfig.Privileged
	}

	slog.DebugContext(ctx,
		"preparing trivy container startup",
		"scope", scope,
		"image", config.Image,
		"cmd", config.Cmd,
		"entrypoint", config.Entrypoint,
		"networkMode", networkMode,
		"securityOpts", securityOpts,
		"privileged", privileged,
		"autoRemove", autoRemove,
		"mounts", mounts,
		"nanoCPUs", resources.NanoCPUs,
		"cpuset", resources.CpusetCpus,
		"memory", resources.Memory,
		"memorySwap", resources.MemorySwap,
	)
}

func TruncateLogOutput(content string, maxBytes int64) string {
	content = strings.TrimSpace(content)
	if content == "" || maxBytes <= 0 {
		return content
	}

	raw := []byte(content)
	if int64(len(raw)) <= maxBytes {
		return content
	}

	return strings.TrimSpace(string(raw[:maxBytes])) + " ...<truncated>"
}

func ReadStartupLogs(ctx context.Context, dockerClient *client.Client, containerID string) (string, string) {
	if dockerClient == nil || containerID == "" {
		return "", ""
	}

	logsCtx, logsCancel := context.WithTimeout(ctx, timeouts.GetDuration(0, timeouts.DefaultDockerAPI))
	defer logsCancel()

	logs, err := dockerClient.ContainerLogs(logsCtx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "200",
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to read trivy startup logs", "containerId", containerID, "error", err)
		return "", ""
	}
	defer func() {
		if closeErr := logs.Close(); closeErr != nil {
			slog.WarnContext(ctx, "failed to close trivy startup logs stream", "containerId", containerID, "error", closeErr)
		}
	}()

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, logs); err != nil && !dockerutils.IsExpectedStreamEndError(err) {
		slog.DebugContext(ctx, "failed to decode trivy startup logs", "containerId", containerID, "error", err)
	}

	stdoutLog := TruncateLogOutput(stdoutBuf.String(), errorExcerptSizeInternal)
	stderrLog := TruncateLogOutput(stderrBuf.String(), errorExcerptSizeInternal)
	return stdoutLog, stderrLog
}

func RemoveContainer(ctx context.Context, dockerClient *client.Client, containerID, warningMessage string) {
	if dockerClient == nil || containerID == "" {
		return
	}

	cleanupCtx := context.WithoutCancel(ctx)
	cleanupCtx, cleanupCancel := context.WithTimeout(cleanupCtx, timeouts.GetDuration(0, timeouts.DefaultDockerAPI))
	defer cleanupCancel()

	if _, err := dockerClient.ContainerRemove(cleanupCtx, containerID, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		slog.WarnContext(cleanupCtx, warningMessage, "containerId", containerID, "error", err)
	}
}

func CopyRegistryConfigToContainer(ctx context.Context, dockerClient *client.Client, containerID string, configJSON []byte) error {
	if dockerClient == nil || containerID == "" || len(configJSON) == 0 {
		return nil
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	if err := tw.WriteHeader(&tar.Header{Name: "arcane-registry-auth/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: registryConfigTarNameInternal, Mode: 0o644, Size: int64(len(configJSON))}); err != nil {
		return err
	}
	if _, err := tw.Write(configJSON); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}

	copyCtx, copyCancel := context.WithTimeout(ctx, timeouts.GetDuration(0, timeouts.DefaultDockerAPI))
	defer copyCancel()

	_, err := dockerClient.CopyToContainer(copyCtx, containerID, client.CopyToContainerOptions{
		DestinationPath: registryConfigCopyDestInternal,
		Content:         &buf,
	})
	return err
}

func ParseCPULimit(raw string, fallback float64) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}

	limit, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}

	if limit < 0 {
		return 0
	}

	return limit
}

func ParseMemoryLimitMB(raw string, fallback int64) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}

	limit, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}

	if limit < 0 {
		return 0
	}

	return limit
}

func ContainerWaitTimeout(trivyTimeoutArg string) time.Duration {
	trivyTimeoutArg = strings.TrimSpace(trivyTimeoutArg)
	if trivyTimeoutArg == "" {
		return timeouts.DefaultTrivyScan + timeouts.DefaultDockerAPI
	}

	timeout, err := time.ParseDuration(trivyTimeoutArg)
	if err != nil || timeout <= 0 {
		return timeouts.DefaultTrivyScan + timeouts.DefaultDockerAPI
	}

	return timeout + timeouts.DefaultDockerAPI
}

func IsSynologyDockerHost(operatingSystem string) bool {
	// Docker info may return a multi-line OS string on some Synology models.
	// e.g. DS925+ (Kernel 5.x) returns "Synology NAS\n (containerized)".
	// Check each line independently so a match on any line is sufficient.
	for line := range strings.SplitSeq(operatingSystem, "\n") {
		if strings.Contains(strings.ToLower(strings.TrimSpace(line)), "synology") {
			return true
		}
	}
	return false
}

func BuildCPUSet(nanoCPUs int64, hostCPUs int) string {
	if nanoCPUs <= 0 {
		return ""
	}

	if hostCPUs <= 0 {
		hostCPUs = 1
	}

	requestedCPUs := min(max(int(math.Floor(float64(nanoCPUs)/float64(NanoCPUsPerCore))), 1), hostCPUs)

	if requestedCPUs == 1 {
		return "0"
	}

	return fmt.Sprintf("0-%d", requestedCPUs-1)
}

func TempFileSize(file *os.File) int64 {
	if file == nil {
		return 0
	}

	stat, err := file.Stat()
	if err != nil {
		return 0
	}

	return stat.Size()
}

func ReadTempFileExcerpt(file *os.File) string {
	if file == nil {
		return ""
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ""
	}

	content, err := io.ReadAll(io.LimitReader(file, errorExcerptSizeInternal))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(content))
}

func ReadOutputFromContainerFile(ctx context.Context, dockerClient *client.Client, containerID, outputPath string) ([]byte, error) {
	if dockerClient == nil {
		return nil, errors.New("docker client is nil")
	}
	if containerID == "" {
		return nil, errors.New("container id is empty")
	}
	if strings.TrimSpace(outputPath) == "" {
		return nil, errors.New("trivy output path is empty")
	}

	copyCtx, copyCancel := context.WithTimeout(ctx, timeouts.GetDuration(0, timeouts.DefaultDockerAPI))
	defer copyCancel()

	copyResult, err := dockerClient.CopyFromContainer(copyCtx, containerID, client.CopyFromContainerOptions{SourcePath: outputPath})
	if err != nil {
		return nil, errors.WrapIf(err, "copy trivy output file")
	}
	archiveReader := copyResult.Content
	defer func() {
		if closeErr := archiveReader.Close(); closeErr != nil && !dockerutils.IsExpectedStreamEndError(closeErr) {
			slog.WarnContext(ctx,
				"failed to close trivy output archive stream",
				"containerId", containerID,
				"outputPath", outputPath,
				"error", closeErr,
			)
		}
	}()

	rawOutput, err := extractFileFromContainerArchiveInternal(archiveReader)
	if err != nil {
		return nil, errors.WrapIf(err, "extract trivy output file")
	}

	if len(bytes.TrimSpace(rawOutput)) == 0 {
		return nil, io.EOF
	}

	return rawOutput, nil
}

func CleanupOutputFileInContainer(ctx context.Context, dockerClient *client.Client, containerID, outputPath string) {
	if dockerClient == nil || containerID == "" || strings.TrimSpace(outputPath) == "" {
		return
	}

	execCtx, execCancel := context.WithTimeout(ctx, timeouts.GetDuration(0, timeouts.DefaultDockerAPI))
	defer execCancel()

	execResp, err := dockerClient.ExecCreate(execCtx, containerID, client.ExecCreateOptions{
		Cmd: []string{"rm", "-f", outputPath},
	})
	if err != nil {
		slog.DebugContext(ctx,
			"failed to create cleanup exec for trivy output file",
			"containerId", containerID,
			"outputPath", outputPath,
			"error", err,
		)
		return
	}

	if _, err := dockerClient.ExecStart(execCtx, execResp.ID, client.ExecStartOptions{}); err != nil {
		slog.DebugContext(ctx,
			"failed to start cleanup exec for trivy output file",
			"containerId", containerID,
			"outputPath", outputPath,
			"error", err,
		)
		return
	}
}

func extractFileFromContainerArchiveInternal(archiveReader io.Reader) ([]byte, error) {
	if archiveReader == nil {
		return nil, errors.New("container archive reader is nil")
	}

	tarReader := tar.NewReader(archiveReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.WrapIf(err, "read tar header")
		}

		if header == nil || header.Typeflag == tar.TypeDir {
			continue
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		data, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, errors.WrapIf(err, "read tar file content")
		}

		return data, nil
	}

	return nil, errors.New("container archive did not include a file")
}

func DecodeReportFromFile(file *os.File) (*vulnerability.TrivyReport, error) {
	if file == nil {
		return nil, errors.New("trivy output file is nil")
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, errors.WrapIf(err, "failed to seek trivy output file")
	}

	rawOutput, err := io.ReadAll(file)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to read trivy output file")
	}

	return DecodeReportFromBytes(rawOutput)
}

func DecodeReportFromBytes(rawOutput []byte) (*vulnerability.TrivyReport, error) {
	trimmedOutput := bytes.TrimSpace(rawOutput)
	if len(trimmedOutput) == 0 {
		return nil, io.EOF
	}

	report, strictErr := decodeSingleReportInternal(trimmedOutput)
	if strictErr == nil {
		return report, nil
	}

	// Trivy output can occasionally include non-JSON log lines or additional JSON
	// objects around the report depending on Docker variant / stream behavior.
	// Extract top-level JSON objects and return the first valid Trivy report.
	jsonCandidates := extractTopLevelJSONObjectCandidatesInternal(trimmedOutput)
	if len(jsonCandidates) == 0 {
		return nil, strictErr
	}

	lastErr := strictErr
	for _, candidate := range jsonCandidates {
		report, err := decodeSingleReportInternal(candidate)
		if err == nil {
			return report, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

func decodeSingleReportInternal(rawOutput []byte) (*vulnerability.TrivyReport, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(rawOutput))

	var report vulnerability.TrivyReport
	if err := json.UnmarshalDecode(decoder, &report); err != nil {
		return nil, err
	}

	if !isLikelyReportInternal(&report) {
		return nil, errors.New("decoded JSON object is not a trivy report")
	}

	if hasTrailingNonWhitespaceInternal(rawOutput, decoder.InputOffset()) {
		return nil, errors.New("trivy output contains trailing data")
	}

	return &report, nil
}

func hasTrailingNonWhitespaceInternal(rawOutput []byte, offset int64) bool {
	if offset < 0 {
		return len(bytes.TrimSpace(rawOutput)) > 0
	}

	if offset >= int64(len(rawOutput)) {
		return false
	}

	return len(bytes.TrimSpace(rawOutput[offset:])) > 0
}

func isLikelyReportInternal(report *vulnerability.TrivyReport) bool {
	if report == nil {
		return false
	}

	return report.SchemaVersion > 0 ||
		strings.TrimSpace(report.ArtifactName) != "" ||
		strings.TrimSpace(report.ArtifactType) != "" ||
		len(report.Results) > 0
}

func extractTopLevelJSONObjectCandidatesInternal(rawOutput []byte) [][]byte {
	if len(rawOutput) == 0 {
		return nil
	}

	candidates := make([][]byte, 0, 4)
	start := -1
	depth := 0
	inString := false
	escaped := false

	for i, b := range rawOutput {
		if start == -1 {
			if b == '{' {
				start = i
				depth = 1
				inString = false
				escaped = false
			}
			continue
		}

		if inString {
			if escaped {
				escaped = false
				continue
			}

			switch b {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch b {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				candidate := bytes.TrimSpace(rawOutput[start : i+1])
				if len(candidate) > 0 {
					candidates = append(candidates, candidate)
				}
				start = -1
			}
		}
	}

	return candidates
}

func FormatExitError(exitCode int64, errMsg, imageName string) error {
	errMsg = strings.TrimSpace(errMsg)

	if exitCode == 137 {
		if errMsg == "" || errMsg == fmt.Sprintf("exit status %d", exitCode) {
			return errors.Errorf("trivy scan failed: process killed with exit status 137 (likely out of memory while scanning %s)", imageName)
		}
		return errors.Errorf("trivy scan failed: %s (process killed with exit status 137; likely out of memory)", errMsg)
	}

	if strings.Contains(strings.ToLower(errMsg), "deadline exceeded") {
		return errors.Errorf("trivy scan timed out for %s (increase TRIVY_SCAN_TIMEOUT or trivyScanTimeout setting)", imageName)
	}

	return errors.Errorf("trivy scan failed: %s", errMsg)
}

func AwaitContainerWaitResponse(
	ctx context.Context,
	statusCh <-chan containertypes.WaitResponse,
	errCh <-chan error,
) (int64, error) {
	for statusCh != nil || errCh != nil {
		select {
		case waitResp, ok := <-statusCh:
			if !ok {
				statusCh = nil
				continue
			}
			return waitResp.StatusCode, nil
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				return 0, err
			}
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}

	return 0, errors.New("trivy container wait ended without status")
}
