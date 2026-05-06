package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/cli/internal/config"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	cliUpdateChannelNext   = "next"
	cliUpdateChannelStable = "stable"
	cliUpdateHTTPTimeout   = 2 * time.Minute
	maxCLIChecksumSize     = 1 * 1024 * 1024
	maxCLIDownloadSize     = 256 * 1024 * 1024
	maxCLIExtractSize      = 128 * 1024 * 1024
)

var (
	cliUpdateVersion string
	cliUpdateTarget  string
	cliUpdateDryRun  bool
	jsonOutput       bool
)

type cliUpdatePlan struct {
	Channel      string `json:"channel"`
	Version      string `json:"version"`
	ArtifactName string `json:"artifactName"`
	ArtifactURL  string `json:"artifactUrl"`
	ChecksumURL  string `json:"checksumUrl"`
	ArtifactSHA  string `json:"artifactSha"`
	ExpectedSHA  string `json:"expectedSha,omitempty"`
	CurrentSHA   string `json:"currentSha"`
	TargetPath   string `json:"targetPath"`
	UpdateNeeded bool   `json:"updateNeeded"`
}

type githubLatestRelease struct {
	TagName string `json:"tag_name"`
}

// Cmd updates the arcane-cli executable.
var Cmd = &cobra.Command{
	Use:          "self-update",
	Aliases:      []string{"self", "cli"},
	Short:        "Update arcane-cli",
	SilenceUsage: true,
	RunE:         runCLIUpdateCommandInternal,
}

var selfUpdateRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Update arcane-cli using the configured channel",
	SilenceUsage: true,
	RunE:         runCLIUpdateCommandInternal,
}

var selfUpdateChannelCmd = &cobra.Command{
	Use:          "channel [stable|next]",
	Short:        "Set the CLI update channel and update arcane-cli",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setCLIUpdateChannelAndRunInternal(cmd, args[0])
	},
}

func init() {
	Cmd.AddCommand(selfUpdateRunCmd)
	Cmd.AddCommand(selfUpdateChannelCmd)
	stableCmd := newSelfUpdateChannelValueCmdInternal(cliUpdateChannelStable)
	nextCmd := newSelfUpdateChannelValueCmdInternal(cliUpdateChannelNext)
	selfUpdateChannelCmd.AddCommand(stableCmd)
	selfUpdateChannelCmd.AddCommand(nextCmd)

	registerCLIUpdateFlagsInternal(Cmd)
	registerCLIUpdateFlagsInternal(selfUpdateRunCmd)
	registerCLIUpdateFlagsInternal(selfUpdateChannelCmd)
	registerCLIUpdateFlagsInternal(stableCmd)
	registerCLIUpdateFlagsInternal(nextCmd)
}

func registerCLIUpdateFlagsInternal(cmd *cobra.Command) {
	cmd.Flags().StringVar(&cliUpdateVersion, "version", "", "Stable release tag to install (default: latest)")
	cmd.Flags().StringVar(&cliUpdateTarget, "target", "", "Binary path to replace (default: current executable)")
	cmd.Flags().BoolVar(&cliUpdateDryRun, "dry-run", false, "Check for an update without installing it")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}

func newSelfUpdateChannelValueCmdInternal(channel string) *cobra.Command {
	return &cobra.Command{
		Use:          channel,
		Short:        fmt.Sprintf("Set the CLI update channel to %s and update arcane-cli", channel),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return setCLIUpdateChannelAndRunInternal(cmd, channel)
		},
	}
}

func setCLIUpdateChannelAndRunInternal(cmd *cobra.Command, channel string) error {
	channel = normalizeCLIUpdateChannelInternal(channel)
	if channel != cliUpdateChannelNext && channel != cliUpdateChannelStable {
		return fmt.Errorf("invalid update channel %q (expected stable or next)", channel)
	}
	if err := saveCLIUpdateChannelInternal(channel); err != nil {
		return err
	}
	output.Success("Set CLI update channel to %s", channel)
	return runCLIUpdateInternal(cmd.Context(), channel)
}

func runCLIUpdateCommandInternal(cmd *cobra.Command, args []string) error {
	return runCLIUpdateInternal(cmd.Context(), "")
}

func runCLIUpdateInternal(ctx context.Context, overrideChannel string) error {
	plan, err := buildCLIUpdatePlanInternal(ctx, overrideChannel)
	if err != nil {
		return err
	}

	if jsonOutput {
		resultBytes, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(resultBytes))
		return nil
	}

	output.Header("Arcane CLI Update")
	output.KeyValue("Channel", plan.Channel)
	output.KeyValue("Version", plan.Version)
	output.KeyValue("Current SHA", plan.CurrentSHA)
	if plan.ExpectedSHA != "" {
		output.KeyValue("Latest SHA", plan.ExpectedSHA)
	}
	output.KeyValue("Artifact SHA", plan.ArtifactSHA)

	if !plan.UpdateNeeded {
		output.Success("arcane-cli is already up to date")
		return nil
	}

	if cliUpdateDryRun {
		output.Info("Update available: %s", plan.ArtifactURL)
		return nil
	}

	if err := installCLIUpdateInternal(ctx, plan); err != nil {
		return err
	}
	output.Success("arcane-cli updated successfully")
	return nil
}

func buildCLIUpdatePlanInternal(ctx context.Context, overrideChannel string) (*cliUpdatePlan, error) {
	targetPath, err := resolveCLIUpdateTargetInternal()
	if err != nil {
		return nil, err
	}

	channel := normalizeCLIUpdateChannelInternal(overrideChannel)
	if channel == "" {
		channel, err = configuredCLIUpdateChannelInternal()
		if err != nil {
			return nil, err
		}
	}
	if channel == "" {
		channel = inferCLIUpdateChannelInternal(config.Version)
	}
	if channel != cliUpdateChannelNext && channel != cliUpdateChannelStable {
		return nil, fmt.Errorf("invalid update channel %q (expected next or stable)", channel)
	}

	currentSHA, err := sha256FileInternal(targetPath)
	if err != nil {
		return nil, err
	}

	plan, err := resolveRemoteCLIUpdateInternal(ctx, channel)
	if err != nil {
		return nil, err
	}
	plan.Channel = channel
	plan.TargetPath = targetPath
	plan.CurrentSHA = currentSHA
	plan.UpdateNeeded = cliUpdateNeededInternal(channel, currentSHA, plan.ExpectedSHA, plan.Version)
	return plan, nil
}

func configuredCLIUpdateChannelInternal() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}
	channel := normalizeCLIUpdateChannelInternal(cfg.CLIUpdateChannel)
	if channel == "" {
		return "", nil
	}
	if channel != cliUpdateChannelNext && channel != cliUpdateChannelStable {
		return "", fmt.Errorf("invalid configured CLI update channel %q (expected stable or next)", cfg.CLIUpdateChannel)
	}
	return channel, nil
}

func saveCLIUpdateChannelInternal(channel string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	cfg.CLIUpdateChannel = channel
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func resolveCLIUpdateTargetInternal() (string, error) {
	if strings.TrimSpace(cliUpdateTarget) != "" {
		return filepath.Abs(strings.TrimSpace(cliUpdateTarget))
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve current executable: %w", err)
	}
	return filepath.EvalSymlinks(exe)
}

func normalizeCLIUpdateChannelInternal(channel string) string {
	return strings.ToLower(strings.TrimSpace(channel))
}

func inferCLIUpdateChannelInternal(version string) string {
	normalized := strings.ToLower(strings.TrimSpace(version))
	if strings.Contains(normalized, "next") || strings.Contains(normalized, "beta") {
		return cliUpdateChannelNext
	}
	return cliUpdateChannelStable
}

func resolveRemoteCLIUpdateInternal(ctx context.Context, channel string) (*cliUpdatePlan, error) {
	if channel == cliUpdateChannelNext {
		return resolveNextCLIUpdateInternal(ctx)
	}
	return resolveStableCLIUpdateInternal(ctx)
}

func resolveNextCLIUpdateInternal(ctx context.Context) (*cliUpdatePlan, error) {
	baseURL := strings.TrimSpace(config.CLINextBaseURL)
	if baseURL == "" {
		return nil, errors.New("next CLI update base URL is empty")
	}

	artifactName, err := cliRawArtifactNameInternal()
	if err != nil {
		return nil, err
	}
	checksumPath, err := cliRawChecksumPathInternal()
	if err != nil {
		return nil, err
	}
	checksumURL := joinURLPathInternal(baseURL, "arcane-cli_checksums.txt")
	checksums, err := fetchTextInternal(ctx, checksumURL)
	if err != nil {
		return nil, err
	}
	expectedSHA, err := findChecksumInternal(checksums, checksumPath, artifactName)
	if err != nil {
		return nil, err
	}

	return &cliUpdatePlan{
		Version:      "next",
		ArtifactName: artifactName,
		ArtifactURL:  joinURLPathInternal(baseURL, artifactName),
		ChecksumURL:  checksumURL,
		ArtifactSHA:  expectedSHA,
		ExpectedSHA:  expectedSHA,
	}, nil
}

func resolveStableCLIUpdateInternal(ctx context.Context) (*cliUpdatePlan, error) {
	version := strings.TrimSpace(cliUpdateVersion)
	if version == "" {
		latest, err := fetchLatestGitHubReleaseInternal(ctx)
		if err != nil {
			return nil, err
		}
		version = latest
	}
	if version == "" {
		return nil, errors.New("stable CLI update version is empty")
	}

	baseURL := strings.TrimSpace(config.CLIStableBaseURL)
	if baseURL == "" {
		return nil, errors.New("stable CLI update base URL is empty")
	}

	artifactName, err := cliArchiveArtifactNameInternal()
	if err != nil {
		return nil, err
	}
	checksumName := fmt.Sprintf("arcane_%s_checksums.txt", strings.TrimPrefix(version, "v"))
	versionBaseURL := joinURLPathInternal(baseURL, version)
	checksumURL := joinURLPathInternal(versionBaseURL, checksumName)
	checksums, err := fetchTextInternal(ctx, checksumURL)
	if err != nil {
		return nil, err
	}
	artifactSHA, err := findChecksumInternal(checksums, artifactName)
	if err != nil {
		return nil, err
	}

	return &cliUpdatePlan{
		Version:      version,
		ArtifactName: artifactName,
		ArtifactURL:  joinURLPathInternal(versionBaseURL, artifactName),
		ChecksumURL:  checksumURL,
		ArtifactSHA:  artifactSHA,
	}, nil
}

func cliUpdateNeededInternal(channel, currentSHA, expectedSHA, remoteVersion string) bool {
	if channel == cliUpdateChannelStable {
		return normalizeVersionInternal(config.Version) != normalizeVersionInternal(remoteVersion)
	}
	return !strings.EqualFold(currentSHA, expectedSHA)
}

func normalizeVersionInternal(version string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(version)), "v")
}

func fetchLatestGitHubReleaseInternal(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/getarcaneapp/arcane/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	var latest githubLatestRelease
	if err := doJSONInternal(req, &latest); err != nil {
		return "", fmt.Errorf("failed to fetch latest GitHub release: %w", err)
	}
	return strings.TrimSpace(latest.TagName), nil
}

func cliRawArtifactNameInternal() (string, error) {
	arch, err := cliArtifactArchInternal()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("arcane-cli_%s_%s", runtime.GOOS, arch), nil
}

func cliArchiveArtifactNameInternal() (string, error) {
	arch, err := cliArtifactArchInternal()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("arcane-cli_%s_%s.tar.gz", runtime.GOOS, arch), nil
}

func cliRawChecksumPathInternal() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return fmt.Sprintf("arcane-cli_%s_amd64_v1/arcane-cli", runtime.GOOS), nil
	case "arm64":
		return fmt.Sprintf("arcane-cli_%s_arm64_v8.0/arcane-cli", runtime.GOOS), nil
	case "386":
		return fmt.Sprintf("arcane-cli_%s_386_sse2/arcane-cli", runtime.GOOS), nil
	case "arm":
		return fmt.Sprintf("arcane-cli_%s_arm_7/arcane-cli", runtime.GOOS), nil
	default:
		return "", fmt.Errorf("unsupported CLI update architecture %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func cliArtifactArchInternal() (string, error) {
	switch runtime.GOARCH {
	case "amd64", "arm64", "386":
		return runtime.GOARCH, nil
	case "arm":
		return "armv7", nil
	default:
		return "", fmt.Errorf("unsupported CLI update architecture %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func installCLIUpdateInternal(ctx context.Context, plan *cliUpdatePlan) error {
	tmpDir, err := os.MkdirTemp("", "arcane-cli-update-*")
	if err != nil {
		return fmt.Errorf("failed to create update temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	downloadPath := filepath.Join(tmpDir, plan.ArtifactName)
	if err := downloadFileInternal(ctx, plan.ArtifactURL, downloadPath); err != nil {
		return err
	}
	if gotSHA, err := sha256FileInternal(downloadPath); err != nil {
		return err
	} else if !strings.EqualFold(gotSHA, plan.ArtifactSHA) {
		return fmt.Errorf("downloaded artifact SHA mismatch: got %s, expected %s", gotSHA, plan.ArtifactSHA)
	}

	binaryPath := downloadPath
	if strings.HasSuffix(plan.ArtifactName, ".tar.gz") {
		extractedPath := filepath.Join(tmpDir, "arcane-cli")
		if err := extractCLIFromTarGzInternal(downloadPath, extractedPath); err != nil {
			return err
		}
		binaryPath = extractedPath
	}

	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return fmt.Errorf("failed to make downloaded binary executable: %w", err)
	}
	return replaceExecutableInternal(binaryPath, plan.TargetPath)
}

func replaceExecutableInternal(sourcePath, targetPath string) error {
	targetDir := filepath.Dir(targetPath)
	tmpTarget, err := os.CreateTemp(targetDir, filepath.Base(targetPath)+".update-*")
	if err != nil {
		return fmt.Errorf("failed to create replacement file in %s: %w", targetDir, err)
	}
	tmpTargetPath := tmpTarget.Name()
	cleanup := func() { _ = os.Remove(tmpTargetPath) }

	source, err := os.Open(sourcePath)
	if err != nil {
		_ = tmpTarget.Close()
		cleanup()
		return fmt.Errorf("failed to open downloaded binary: %w", err)
	}
	defer func() { _ = source.Close() }()

	if _, err := io.Copy(tmpTarget, source); err != nil {
		_ = tmpTarget.Close()
		cleanup()
		return fmt.Errorf("failed to write replacement binary: %w", err)
	}
	if err := tmpTarget.Chmod(0o755); err != nil {
		_ = tmpTarget.Close()
		cleanup()
		return fmt.Errorf("failed to chmod replacement binary: %w", err)
	}
	if err := tmpTarget.Sync(); err != nil {
		_ = tmpTarget.Close()
		cleanup()
		return fmt.Errorf("failed to sync replacement binary: %w", err)
	}
	if err := tmpTarget.Close(); err != nil {
		cleanup()
		return fmt.Errorf("failed to close replacement binary: %w", err)
	}
	if err := os.Rename(tmpTargetPath, targetPath); err != nil {
		cleanup()
		return fmt.Errorf("failed to replace %s: %w", targetPath, err)
	}

	dirFile, err := os.Open(targetDir)
	if err != nil {
		return fmt.Errorf("failed to open target directory for sync: %w", err)
	}
	defer func() { _ = dirFile.Close() }()
	if err := dirFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync target directory: %w", err)
	}
	return nil
}

func extractCLIFromTarGzInternal(archivePath, outputPath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer func() { _ = file.Close() }()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to read gzip archive: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if path.Base(header.Name) != "arcane-cli" {
			continue
		}
		if header.Size < 0 || header.Size > maxCLIExtractSize {
			return fmt.Errorf("arcane-cli archive entry has invalid size %d", header.Size)
		}

		out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return fmt.Errorf("failed to create extracted binary: %w", err)
		}
		if _, err := io.CopyN(out, tarReader, header.Size); err != nil {
			_ = out.Close()
			return fmt.Errorf("failed to extract CLI binary: %w", err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("failed to close extracted binary: %w", err)
		}
		return nil
	}
	return errors.New("archive did not contain arcane-cli")
}

func fetchTextInternal(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/plain")

	client := &http.Client{Timeout: cliUpdateHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("failed to fetch %s: HTTP %s", url, resp.Status)
	}
	limitedBody := &io.LimitedReader{R: resp.Body, N: maxCLIChecksumSize + 1}
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", url, err)
	}
	if len(body) > maxCLIChecksumSize {
		return "", fmt.Errorf("failed to read %s: checksum response exceeds maximum size of %d bytes", url, maxCLIChecksumSize)
	}
	return string(body), nil
}

func downloadFileInternal(ctx context.Context, url, outputPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: cliUpdateHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("failed to download %s: HTTP %s", url, resp.Status)
	}

	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create download file: %w", err)
	}
	limitedBody := &io.LimitedReader{R: resp.Body, N: maxCLIDownloadSize + 1}
	written, err := io.Copy(out, limitedBody)
	if err != nil {
		_ = out.Close()
		return fmt.Errorf("failed to write download file: %w", err)
	}
	if written > maxCLIDownloadSize {
		_ = out.Close()
		return fmt.Errorf("downloaded artifact exceeds maximum size of %d bytes", maxCLIDownloadSize)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("failed to close download file: %w", err)
	}
	return nil
}

func doJSONInternal(req *http.Request, out any) error {
	client := &http.Client{Timeout: cliUpdateHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	return nil
}

func sha256FileInternal(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open %s for SHA-256: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to hash %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func findChecksumInternal(checksums string, artifactNames ...string) (string, error) {
	wanted := make(map[string]struct{}, len(artifactNames))
	for _, artifactName := range artifactNames {
		artifactName = normalizeChecksumPathInternal(artifactName)
		if artifactName != "" {
			wanted[artifactName] = struct{}{}
		}
	}

	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		checksumPath := normalizeChecksumPathInternal(fields[len(fields)-1])
		if checksumPathMatchesInternal(checksumPath, wanted) {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found", strings.Join(artifactNames, " or "))
}

func normalizeChecksumPathInternal(value string) string {
	return strings.TrimPrefix(path.Clean(strings.TrimSpace(value)), "./")
}

func checksumPathMatchesInternal(checksumPath string, wanted map[string]struct{}) bool {
	if _, ok := wanted[checksumPath]; ok {
		return true
	}
	if _, ok := wanted[path.Base(checksumPath)]; ok {
		return true
	}
	for wantedPath := range wanted {
		if strings.HasSuffix(checksumPath, "/"+wantedPath) {
			return true
		}
	}
	return false
}

func joinURLPathInternal(baseURL string, parts ...string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	joined := path.Join(parts...)
	if joined == "." || joined == "" {
		return baseURL
	}
	return baseURL + "/" + strings.TrimLeft(joined, "/")
}
