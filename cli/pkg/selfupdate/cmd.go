package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	"github.com/getarcaneapp/arcane/cli/v3/internal/selfupdate"
)

const (
	cliUpdateChannelNext   = "next"
	cliUpdateChannelStable = "stable"
	cliUpdateHTTPTimeout   = 2 * time.Minute
	maxCLIChecksumSize     = 1 * 1024 * 1024
	maxCLIBundleSize       = 1 * 1024 * 1024
	maxCLIDownloadSize     = 256 * 1024 * 1024
	maxCLIExtractSize      = 128 * 1024 * 1024
)

var errCLIUpdateNotFound = errors.New("HTTP 404 Not Found")

type cliUpdateOptions struct {
	version, target string
	dryRun, verbose bool
}

type cliUpdatePlan struct {
	Channel           string `json:"channel"`
	Version           string `json:"version"`
	ArtifactName      string `json:"artifactName"`
	ArtifactURL       string `json:"artifactUrl"`
	ChecksumURL       string `json:"checksumUrl"`
	BundleURL         string `json:"bundleUrl,omitempty"`
	SignatureVerified bool   `json:"signatureVerified"`
	ArtifactSHA       string `json:"artifactSha"`
	ExpectedSHA       string `json:"expectedSha,omitempty"`
	CurrentSHA        string `json:"currentSha"`
	TargetPath        string `json:"targetPath"`
	UpdateNeeded      bool   `json:"updateNeeded"`
}

type githubLatestRelease struct {
	TagName string `json:"tag_name"`
}

// Command updates the arcane-cli executable.
func Command() *cobra.Command {
	var opts cliUpdateOptions
	cmd := &cobra.Command{
		Annotations: map[string]string{"arcane:scope": cmdutil.ScopeGlobal},
		Use:         "self-update",
		Aliases:     []string{"self", "cli"},
		Short:       "Update arcane-cli",
		Long: `Update arcane-cli using the configured release channel.

Enable Sigstore verification with arcane-cli config set previews.newSelfUpdater true.
This preview fetches trust data from tuf-repo-cdn.sigstore.dev and caches it under
the user cache directory at arcane-cli/sigstore, usable offline until expiry.
Releases before v1.18.0 are unsigned. Valid signatures do not prevent replay of older releases.`,
		Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error { return opts.runCLIUpdate(cmd, "") },
	}
	cmd.AddCommand(newSelfUpdateRunCmd(), newSelfUpdateChannelCmd())
	registerCLIUpdateFlags(cmd, &opts)
	return cmd
}

func newSelfUpdateRunCmd() *cobra.Command {
	var opts cliUpdateOptions
	cmd := &cobra.Command{
		Use: "run", Short: "Update arcane-cli using the configured channel",
		Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error { return opts.runCLIUpdate(cmd, "") },
	}
	registerCLIUpdateFlags(cmd, &opts)
	return cmd
}

func newSelfUpdateChannelCmd() *cobra.Command {
	var opts cliUpdateOptions
	cmd := &cobra.Command{
		Use: "channel <stable|next>", Short: "Set the CLI update channel and update arcane-cli",
		Args: cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error { return opts.setCLIUpdateChannelAndRun(cmd, args[0]) },
	}
	cmd.AddCommand(newSelfUpdateChannelValueCmd(cliUpdateChannelStable), newSelfUpdateChannelValueCmd(cliUpdateChannelNext))
	registerCLIUpdateFlags(cmd, &opts)
	return cmd
}

func newSelfUpdateChannelValueCmd(channel string) *cobra.Command {
	var opts cliUpdateOptions
	cmd := &cobra.Command{
		Use: channel, Short: fmt.Sprintf("Set the CLI update channel to %s and update arcane-cli", channel),
		Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error { return opts.setCLIUpdateChannelAndRun(cmd, channel) },
	}
	registerCLIUpdateFlags(cmd, &opts)
	return cmd
}

func registerCLIUpdateFlags(cmd *cobra.Command, opts *cliUpdateOptions) {
	cmd.Flags().StringVar(&opts.version, "version", "", "Stable release tag to install (default: latest)")
	cmd.Flags().StringVar(&opts.target, "target", "", "Binary path to replace (default: current executable)")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Check for an update without installing it")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "Print self-update resolution details")
}

func (opts cliUpdateOptions) setCLIUpdateChannelAndRun(cmd *cobra.Command, channel string) error {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel != cliUpdateChannelNext && channel != cliUpdateChannelStable {
		return fmt.Errorf("invalid update channel %q (expected stable or next)", channel)
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	cfg.CLIUpdateChannel = channel
	if saveErr := config.Save(cmd.Context(), cfg); saveErr != nil {
		return fmt.Errorf("failed to save config: %w", saveErr)
	}

	if !cmdutil.JSONOutputEnabled(cmd) {
		output.Success("Set CLI update channel to %s", channel)
	}
	return opts.runCLIUpdate(cmd, channel)
}

func (opts cliUpdateOptions) runCLIUpdate(cmd *cobra.Command, overrideChannel string) error {
	ctx := cmd.Context()
	jsonMode := cmdutil.JSONOutputEnabled(cmd)
	loading := output.StartTracker(ctx, output.Stderr(), !jsonMode, "Resolving update")
	loading.NextStep()
	defer loading.Stop()
	plan, err := opts.buildCLIUpdatePlan(ctx, overrideChannel)
	if err != nil {
		return err
	}

	loading.Finish("", err)
	loading.Stop()
	if jsonMode {
		return cmdutil.PrintJSON(plan)
	}

	output.Header("Arcane CLI Update")
	output.KeyValue("Channel", plan.Channel)
	output.KeyValue("Version", plan.Version)
	output.KeyValue("Current SHA", plan.CurrentSHA)
	if plan.ExpectedSHA != "" {
		output.KeyValue("Latest SHA", plan.ExpectedSHA)
	}
	output.KeyValue("Artifact SHA", plan.ArtifactSHA)
	if plan.SignatureVerified {
		output.KeyValue("Signature", "verified")
	}

	if !plan.UpdateNeeded {
		output.Success("arcane-cli is already up to date")
		return nil
	}

	if opts.dryRun {
		output.Info("Update available: %s", plan.ArtifactURL)
		return nil
	}

	if installCLIUpdateErr := opts.installCLIUpdate(ctx, plan, jsonMode); installCLIUpdateErr != nil {
		return installCLIUpdateErr
	}
	output.Success("arcane-cli updated successfully")
	return nil
}

func (opts cliUpdateOptions) buildCLIUpdatePlan(ctx context.Context, overrideChannel string) (*cliUpdatePlan, error) {
	targetPath, err := opts.resolveCLIUpdateTarget()
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	channel := strings.ToLower(strings.TrimSpace(overrideChannel))
	if channel == "" {
		channel = strings.ToLower(strings.TrimSpace(cfg.CLIUpdateChannel))
		if channel != "" && channel != cliUpdateChannelNext && channel != cliUpdateChannelStable {
			return nil, fmt.Errorf("invalid configured CLI update channel %q (expected stable or next)", cfg.CLIUpdateChannel)
		}
	}
	if channel == "" {
		normalized := strings.ToLower(strings.TrimSpace(config.Version))
		channel = kit.Ternary(strings.Contains(normalized, "next") || strings.Contains(normalized, "beta"), cliUpdateChannelNext, cliUpdateChannelStable)
	}
	if channel != cliUpdateChannelNext && channel != cliUpdateChannelStable {
		return nil, fmt.Errorf("invalid update channel %q (expected next or stable)", channel)
	}
	opts.verboseCLIUpdate("channel=%s target=%s platform=%s/%s", channel, targetPath, runtime.GOOS, runtime.GOARCH)

	currentSHA, err := sha256File(targetPath)
	if err != nil {
		return nil, err
	}
	var verifier *selfupdate.Verifier
	if cfg.Previews.NewSelfUpdater {
		verifier, err = selfupdate.New(ctx)
		if err != nil {
			return nil, err
		}
	}

	resolve := kit.Ternary(channel == cliUpdateChannelNext, opts.resolveNextCLIUpdate, opts.resolveStableCLIUpdate)
	plan, err := resolve(ctx, verifier)
	if err != nil {
		return nil, err
	}
	plan.Channel = channel
	plan.TargetPath = targetPath
	plan.CurrentSHA = currentSHA
	plan.UpdateNeeded = cliUpdateNeeded(channel, currentSHA, plan.ExpectedSHA, plan.Version)
	return plan, nil
}

func (opts cliUpdateOptions) resolveCLIUpdateTarget() (string, error) {
	if strings.TrimSpace(opts.target) != "" {
		return filepath.Abs(strings.TrimSpace(opts.target))
	}
	if pathTarget, err := exec.LookPath("arcane-cli"); err == nil {
		absTarget, absErr := filepath.Abs(pathTarget)
		if absErr != nil {
			return "", fmt.Errorf("failed to resolve arcane-cli from PATH: %w", absErr)
		}
		return filepath.EvalSymlinks(absTarget)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve current executable: %w", err)
	}
	return filepath.EvalSymlinks(exe)
}

func (opts cliUpdateOptions) resolveNextCLIUpdate(ctx context.Context, verifier *selfupdate.Verifier) (*cliUpdatePlan, error) {
	baseURL := strings.TrimSpace(config.CLINextBaseURL)
	if baseURL == "" {
		return nil, errors.New("next CLI update base URL is empty")
	}

	platformName, err := cliPlatformName()
	if err != nil {
		return nil, err
	}
	artifactName := "arcane-cli_" + platformName
	checksumURL := joinURLPath(baseURL, "arcane-cli_checksums.txt")
	artifactURL := joinURLPath(baseURL, artifactName)
	opts.verboseCLIUpdate("next base URL: %s", baseURL)
	opts.verboseCLIUpdate("next artifact URL: %s", artifactURL)
	opts.verboseCLIUpdate("next checksum URL: %s", checksumURL)
	opts.verboseCLIUpdate("next checksum candidates: %s", artifactName+", "+platformName)

	checksums, bundleURL, err := opts.fetchCLIChecksums(ctx, checksumURL, verifier)
	if err != nil {
		return nil, err
	}
	opts.verboseCLIUpdate("next checksum entries: %s", strings.Join(checksumEntryNames(checksums), ", "))
	expectedSHA, err := findChecksum(checksums, artifactName, platformName)
	if err != nil {
		return nil, err
	}

	return &cliUpdatePlan{
		Version:           "next",
		ArtifactName:      artifactName,
		ArtifactURL:       artifactURL,
		ChecksumURL:       checksumURL,
		BundleURL:         bundleURL,
		SignatureVerified: verifier != nil,
		ArtifactSHA:       expectedSHA,
		ExpectedSHA:       expectedSHA,
	}, nil
}

func (opts cliUpdateOptions) resolveStableCLIUpdate(ctx context.Context, verifier *selfupdate.Verifier) (*cliUpdatePlan, error) {
	version := strings.TrimSpace(opts.version)
	if version == "" {
		latest, err := fetchLatestGitHubRelease(ctx)
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

	arch, err := cliArtifactArch()
	if err != nil {
		return nil, err
	}
	artifactName := fmt.Sprintf("arcane-cli_%s_%s.tar.gz", runtime.GOOS, arch)
	checksumName := fmt.Sprintf("arcane_%s_checksums.txt", strings.TrimPrefix(version, "v"))
	versionBaseURL := joinURLPath(baseURL, version)
	checksumURL := joinURLPath(versionBaseURL, checksumName)
	artifactURL := joinURLPath(versionBaseURL, artifactName)
	opts.verboseCLIUpdate("stable version: %s", version)
	opts.verboseCLIUpdate("stable artifact URL: %s", artifactURL)
	opts.verboseCLIUpdate("stable checksum URL: %s", checksumURL)
	opts.verboseCLIUpdate("stable checksum candidates: %s", artifactName)

	checksums, bundleURL, err := opts.fetchCLIChecksums(ctx, checksumURL, verifier)
	if err != nil {
		return nil, err
	}
	opts.verboseCLIUpdate("stable checksum entries: %s", strings.Join(checksumEntryNames(checksums), ", "))
	artifactSHA, err := findChecksum(checksums, artifactName)
	if err != nil {
		return nil, err
	}

	return &cliUpdatePlan{
		Version:           version,
		ArtifactName:      artifactName,
		ArtifactURL:       artifactURL,
		ChecksumURL:       checksumURL,
		BundleURL:         bundleURL,
		SignatureVerified: verifier != nil,
		ArtifactSHA:       artifactSHA,
	}, nil
}

func cliUpdateNeeded(channel, currentSHA, expectedSHA, remoteVersion string) bool {
	if channel == cliUpdateChannelStable {
		current := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(config.Version)), "v")
		remote := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(remoteVersion)), "v")
		return current != remote
	}
	return strings.TrimSpace(expectedSHA) == "" || !strings.EqualFold(currentSHA, expectedSHA)
}

func fetchLatestGitHubRelease(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/getarcaneapp/arcane/releases/latest", http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: cliUpdateHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest GitHub release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("failed to fetch latest GitHub release: HTTP %s", resp.Status)
	}

	var latest githubLatestRelease
	if unmarshalReadErr := json.UnmarshalRead(resp.Body, &latest); unmarshalReadErr != nil {
		return "", fmt.Errorf("failed to fetch latest GitHub release: %w", unmarshalReadErr)
	}
	return strings.TrimSpace(latest.TagName), nil
}

func cliPlatformName() (string, error) {
	arch, err := cliArtifactArch()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%s", runtime.GOOS, arch), nil
}

func cliArtifactArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64", "arm64", "386", "riscv64":
		return runtime.GOARCH, nil
	case "arm":
		return "armv7", nil
	default:
		return "", fmt.Errorf("unsupported CLI update architecture %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func (opts cliUpdateOptions) installCLIUpdate(ctx context.Context, plan *cliUpdatePlan, jsonMode bool) error {
	tmpDir, err := os.MkdirTemp("", "arcane-cli-update-*")
	if err != nil {
		return fmt.Errorf("failed to create update temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	loading := output.StartTracker(ctx, output.Stderr(), !jsonMode, "Downloading artifact", "Verifying artifact", "Installing executable")
	loading.NextStep()
	defer loading.Stop()
	downloadPath := filepath.Join(tmpDir, plan.ArtifactName)
	if downloadFileErr := opts.downloadFile(ctx, plan.ArtifactURL, downloadPath, jsonMode); downloadFileErr != nil {
		return downloadFileErr
	}
	loading.NextStep()
	if gotSHA, sha256FileErr := sha256File(downloadPath); sha256FileErr != nil {
		return sha256FileErr
	} else if !strings.EqualFold(gotSHA, plan.ArtifactSHA) {
		return fmt.Errorf("downloaded artifact SHA mismatch: got %s, expected %s", gotSHA, plan.ArtifactSHA)
	}

	loading.NextStep()
	binaryPath := downloadPath
	if strings.HasSuffix(plan.ArtifactName, ".tar.gz") {
		extractedPath := filepath.Join(tmpDir, "arcane-cli")
		if extractCLIFromTarGzErr := extractCLIFromTarGz(downloadPath, extractedPath); extractCLIFromTarGzErr != nil {
			return extractCLIFromTarGzErr
		}
		binaryPath = extractedPath
	}

	if chmodErr := os.Chmod(binaryPath, 0o755); chmodErr != nil {
		return fmt.Errorf("failed to make downloaded binary executable: %w", chmodErr)
	}
	return replaceExecutable(binaryPath, plan.TargetPath)
}

func replaceExecutable(sourcePath, targetPath string) error {
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

	if _, copyErr := io.Copy(tmpTarget, source); copyErr != nil {
		_ = tmpTarget.Close()
		cleanup()
		return fmt.Errorf("failed to write replacement binary: %w", copyErr)
	}
	if chmodErr := tmpTarget.Chmod(0o755); chmodErr != nil {
		_ = tmpTarget.Close()
		cleanup()
		return fmt.Errorf("failed to chmod replacement binary: %w", chmodErr)
	}
	if syncErr := tmpTarget.Sync(); syncErr != nil {
		_ = tmpTarget.Close()
		cleanup()
		return fmt.Errorf("failed to sync replacement binary: %w", syncErr)
	}
	if closeErr := tmpTarget.Close(); closeErr != nil {
		cleanup()
		return fmt.Errorf("failed to close replacement binary: %w", closeErr)
	}
	if renameErr := os.Rename(tmpTargetPath, targetPath); renameErr != nil {
		cleanup()
		return fmt.Errorf("failed to replace %s: %w", targetPath, renameErr)
	}

	dirFile, err := os.Open(targetDir)
	if err != nil {
		return fmt.Errorf("failed to open target directory for sync: %w", err)
	}
	defer func() { _ = dirFile.Close() }()
	if syncDirectoryErr := dirFile.Sync(); syncDirectoryErr != nil {
		return fmt.Errorf("failed to sync target directory: %w", syncDirectoryErr)
	}
	return nil
}

func extractCLIFromTarGz(archivePath, outputPath string) error {
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
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("failed to read tar archive: %w", nextErr)
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

		out, nextErr := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if nextErr != nil {
			return fmt.Errorf("failed to create extracted binary: %w", nextErr)
		}
		if _, copyNErr := io.CopyN(out, tarReader, header.Size); copyNErr != nil {
			_ = out.Close()
			return fmt.Errorf("failed to extract CLI binary: %w", copyNErr)
		}
		if closeErr := out.Close(); closeErr != nil {
			return fmt.Errorf("failed to close extracted binary: %w", closeErr)
		}
		return nil
	}
	return errors.New("archive did not contain arcane-cli")
}

func (opts cliUpdateOptions) fetchCLIChecksums(ctx context.Context, checksumURL string, verifier *selfupdate.Verifier) (string, string, error) {
	checksums, err := fetchBytes(ctx, checksumURL, "text/plain", "checksum", maxCLIChecksumSize)
	if err != nil {
		return "", "", err
	}
	if verifier == nil {
		return string(checksums), "", nil
	}

	bundleURL := checksumURL + ".sigstore.json"
	opts.verboseCLIUpdate("checksum bundle URL: %s", bundleURL)
	bundleJSON, err := fetchBytes(ctx, bundleURL, "application/json", "bundle", maxCLIBundleSize)
	if errors.Is(err, errCLIUpdateNotFound) {
		return "", "", fmt.Errorf("failed to fetch %s: releases before v1.18.0 are unsigned; run `arcane-cli config set previews.newSelfUpdater false` to install them", bundleURL)
	}
	if err != nil {
		return "", "", err
	}
	if verifyErr := verifier.Verify(checksums, bundleJSON); verifyErr != nil {
		return "", "", verifyErr
	}
	opts.verboseCLIUpdate("checksum signature verified: %s", bundleURL)
	return string(checksums), bundleURL, nil
}

func fetchBytes(ctx context.Context, url, accept, kind string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)

	client := &http.Client{Timeout: cliUpdateHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("failed to fetch %s: %w", url, errCLIUpdateNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to fetch %s: HTTP %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", url, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("failed to read %s: %s response exceeds maximum size of %d bytes", url, kind, limit)
	}
	return body, nil
}

func (opts cliUpdateOptions) downloadFile(ctx context.Context, url, outputPath string, jsonMode bool) error {
	opts.verboseCLIUpdate("downloading %s to %s", url, outputPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
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
	written, err := output.Copy(ctx, out, limitedBody, resp.ContentLength, "Downloading artifact", !jsonMode)
	if err != nil {
		_ = out.Close()
		return fmt.Errorf("failed to write download file: %w", err)
	}
	if written > maxCLIDownloadSize {
		_ = out.Close()
		return fmt.Errorf("downloaded artifact exceeds maximum size of %d bytes", maxCLIDownloadSize)
	}
	if closeErr := out.Close(); closeErr != nil {
		return fmt.Errorf("failed to close download file: %w", closeErr)
	}
	return nil
}

//nolint:goprintffuncname // internal verbose-logging helper; printf-style by design
func (opts cliUpdateOptions) verboseCLIUpdate(format string, args ...any) {
	if !opts.verbose {
		return
	}
	_, _ = fmt.Fprintf(output.CoordinatedWriter(output.Stderr()), "self-update: "+format+"\n", args...)
}

func sha256File(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open %s for SHA-256: %w", filePath, err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, copyErr := io.Copy(hash, file); copyErr != nil {
		return "", fmt.Errorf("failed to hash %s: %w", filePath, copyErr)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func findChecksum(checksums string, artifactNames ...string) (string, error) {
	wanted := make(map[string]struct{}, len(artifactNames))
	for _, artifactName := range artifactNames {
		artifactName = strings.TrimPrefix(path.Clean(strings.TrimSpace(artifactName)), "./")
		if artifactName != "" {
			wanted[artifactName] = struct{}{}
		}
	}

	for line := range strings.SplitSeq(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		checksumPath := strings.TrimPrefix(path.Clean(strings.TrimSpace(fields[len(fields)-1])), "./")
		if _, ok := wanted[checksumPath]; ok {
			return strings.ToLower(fields[0]), nil
		}
		if _, ok := wanted[path.Base(checksumPath)]; ok {
			return strings.ToLower(fields[0]), nil
		}
		for wantedPath := range wanted {
			if strings.HasSuffix(checksumPath, "/"+wantedPath) {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	return "", fmt.Errorf("checksum for %s not found", strings.Join(artifactNames, " or "))
}

func checksumEntryNames(checksums string) []string {
	names := make([]string, 0)
	for line := range strings.SplitSeq(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		names = append(names, strings.TrimPrefix(path.Clean(strings.TrimSpace(fields[len(fields)-1])), "./"))
	}
	return names
}

func joinURLPath(baseURL string, parts ...string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	joined := path.Join(parts...)
	if joined == "." || joined == "" {
		return baseURL
	}
	return baseURL + "/" + strings.TrimLeft(joined, "/")
}
