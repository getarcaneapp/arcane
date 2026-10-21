package patch

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/moby/moby/client"
	"github.com/opencontainers/image-spec/specs-go/v1"
	copacommon "github.com/project-copacetic/copacetic/pkg/common"
	"github.com/project-copacetic/copacetic/pkg/patch"
	copatypes "github.com/project-copacetic/copacetic/pkg/types"
	"github.com/samber/mo"
	"go.getarcane.app/acfs"
	"go.getarcane.app/kit/pkg"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/vuln"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/logging"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

// scheduledPatchUser is the actor recorded for scheduled patches and their verification scans.
var (
	scheduledPatchUser = types.Actor{Username: "System"}
	// errPatchInterrupted fails a patch cut off mid-run; its record keeps the message so it never reruns.
	errPatchInterrupted = errors.New("image patch was interrupted before its result was recorded; start a new patch")
	// registryTransport traces platform-manifest lookups; ggcr adds its retry wrappers on top.
	registryTransport = otelhttp.NewTransport(remote.DefaultTransport)
)

// Target is one image a scheduled patch run covers.
type Target struct {
	ImageID   string `json:"imageId"`
	ImageName string `json:"imageName"`
}

// Service patches image OS packages in place using the Copacetic
// library and records each run in the image_patches table.
type Service struct {
	db                   *database.DB
	dockerService        *docker.DockerClientService
	settingsService      *settings.SettingsService
	activityService      *activity.ActivityService
	registryService      *registry.ContainerRegistryService
	vulnerabilityService *vulnerability.VulnerabilityService

	// patchSlot serializes patch executions: one BuildKit patch at a time, and
	// the process-wide logrus mirror only ever carries one run's output.
	patchSlot chan struct{}
}

func NewService(
	db *database.DB,
	dockerService *docker.DockerClientService,
	settingsService *settings.SettingsService,
	activityService *activity.ActivityService,
	registryService *registry.ContainerRegistryService,
	vulnerabilityService *vulnerability.VulnerabilityService,
) *Service {
	// Copa logs through the global logrus logger; route it into slog so its
	// output follows Arcane's log format.
	logging.InstallLogrusBridge()

	// Copa resolves the daemon for BuildKit dialing, manifest discovery, and
	// image loading from DOCKER_HOST; without it, its docker connhelper falls
	// back to shelling out to the docker CLI, which Arcane deployments lack.
	if os.Getenv("DOCKER_HOST") == "" {
		if host := strings.TrimSpace(dockerService.DockerHost()); host != "" {
			if err := os.Setenv("DOCKER_HOST", host); err != nil {
				slog.WarnContext(context.Background(), "failed to set DOCKER_HOST for image patching", "error", err) //nolint:forbidigo // Service construction has no request context.
			}
		}
	}

	return &Service{
		db:                   db,
		dockerService:        dockerService,
		settingsService:      settingsService,
		activityService:      activityService,
		registryService:      registryService,
		vulnerabilityService: vulnerabilityService,
		patchSlot:            make(chan struct{}, 1),
	}
}

// PatchImage starts a background patch run for the given image and returns the
// pending record (carrying the activity ID) immediately.
func (s *Service) PatchImage(ctx context.Context, envID, imageID string, opts types.PatchOptions, user types.Actor) (*types.PatchRecord, error) {
	if opts.ScanID != "" {
		if err := s.settingsService.RequireFeature(ctx, types.FeatureVulnerabilityManagement); err != nil {
			return nil, err
		}
	}
	record, run, err := s.startPatch(utils.ActivityRuntimeContext(ctx, nil), envID, imageID, opts, user, "")
	if err != nil {
		return nil, err
	}
	go func() { _ = run() }()
	return new(record.ToDto()), nil
}

// PatchTarget patches one scanned image and waits for the result. recordID keys
// the patch so a redelivered task resumes its own record instead of patching twice.
func (s *Service) PatchTarget(ctx context.Context, envID string, target Target, recordID string) error {
	_, run, err := s.startPatch(ctx, envID, target.ImageID, types.PatchOptions{ScanID: target.ImageID}, scheduledPatchUser, recordID)
	if err != nil || run == nil {
		return err
	}
	return run()
}

// startPatch validates an image, records its patch as running, and returns the
// function that performs it. A completed record under recordID returns no function.
func (s *Service) startPatch(
	ctx context.Context,
	envID, imageID string,
	opts types.PatchOptions,
	user types.Actor,
	recordID string,
) (*ImagePatchRecord, func() error, error) {
	var existing ImagePatchRecord
	if recordID != "" {
		if err := s.db.WithContext(ctx).Where("id = ?", recordID).Limit(1).Find(&existing).Error; err != nil {
			return nil, nil, fmt.Errorf("failed to load image patch record: %w", err)
		}
		if existing.Status == string(types.PatchStatusCompleted) {
			return &existing, nil, nil
		}
		// A record still patching was cut off mid-run, so whether Copacetic already replaced its tag is unknown.
		// Its failure keeps that message, so no later delivery runs the patch again either.
		if existing.Status == string(types.PatchStatusPatching) || kit.FromPtr(existing.Error) == errPatchInterrupted.Error() {
			s.finishPatchRecord(ctx, &existing, types.PatchStatusFailed, errPatchInterrupted.Error(), nil, 0)
			return nil, nil, errPatchInterrupted
		}
	}

	if storeErr := s.requireContainerdImageStore(ctx); storeErr != nil {
		return nil, nil, storeErr
	}
	imageInspect, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
		return dockerClient.ImageInspect(ctx, imageID)
	})
	if err != nil {
		return nil, nil, common.ClassifyDockerError(fmt.Errorf("failed to inspect image: %w", err))
	}
	if len(imageInspect.RepoTags) == 0 {
		return nil, nil, common.ErrImageUntagged
	}
	// A replay patches the source its record names, so the output matches the saved PatchedRef.
	if existing.ID != "" && !slices.Contains(imageInspect.RepoTags, existing.OriginalRef) {
		return nil, nil, fmt.Errorf("image is no longer tagged %s; start a new patch", existing.OriginalRef)
	}
	imageRef := cmp.Or(existing.OriginalRef, imageInspect.RepoTags[0])
	// Never pulled or pushed: BuildKit cannot fetch the image to patch it.
	if len(imageInspect.RepoDigests) == 0 {
		return nil, nil, common.ErrImageLocalOnly
	}

	// Scan records are keyed by image ID; refuse a scan of a different image than the route selected.
	if opts.ScanID != "" && opts.ScanID != imageInspect.ID {
		return nil, nil, common.ErrPatchScanImageMismatch
	}
	var report vulnerability.VulnerabilityReportRecord
	if opts.ScanID != "" {
		if reportErr := s.db.WithContext(ctx).First(&report, "image_id = ?", opts.ScanID).Error; reportErr != nil || report.Data == "" {
			return nil, nil, common.ErrPatchScanReportUnavailable
		}
	}
	mode := kit.Ternary(opts.ScanID != "", types.PatchModeReport, types.PatchModeUpdateAll)

	suffix := cmp.Or(strings.TrimSpace(opts.Suffix), strings.TrimSpace(s.settingsService.GetSettingsConfig().ImagePatchSuffix.Value), "patched")
	patchedRef, err := resolvePatchedRef(imageRef, opts.PatchedTag, suffix)
	if err != nil {
		return nil, nil, err
	}

	// Copa reads registry credentials from the docker config; refresh it before any registry lookups.
	s.writeRegistryAuthConfig(ctx)

	// Unless the all-platforms setting is on, pin update-all patches to this image's platform manifest so copa
	// patches one platform and keeps the plain patched tag. Report-mode patches are always single-platform.
	copaImageRef := func() string {
		if mode == types.PatchModeUpdateAll && !s.settingsService.GetSettingsConfig().ImagePatchAllPlatforms.IsTrue() {
			return cmp.Or(platformPinnedRef(ctx, imageRef, v1.Platform{
				OS:           imageInspect.Os,
				Architecture: imageInspect.Architecture,
				Variant:      imageInspect.Variant,
			}), imageRef)
		}
		return imageRef
	}()

	// Each attempt gets its own activity, since a finished activity never reopens.
	started, startErr := s.activityService.StartActivity(ctx, activity.StartActivityRequest{
		EnvironmentID: envID,
		Type:          types.ActivityTypeImagePatch,
		ResourceType:  new("image"),
		ResourceID:    &imageID,
		ResourceName:  &imageRef,
		StartedBy:     &user,
		Progress:      new(0),
		LatestMessage: "Image patch queued",
	})
	if startErr != nil {
		slog.WarnContext(ctx, "failed to create image patch activity", "error", startErr, "imageRef", imageRef)
	}
	activityID := kit.Ternary(startErr == nil, kit.FromPtr(started).ID, "")
	runCtx := s.activityService.Track(ctx, activityID)

	record, saveErr := func() (*ImagePatchRecord, error) {
		if existing.ID == "" {
			created := &ImagePatchRecord{
				ID:              recordID,
				EnvironmentID:   envID,
				OriginalImageID: imageInspect.ID,
				OriginalRef:     imageRef,
				OriginalDigest:  imageInspect.RepoDigests[0],
				PatchedRef:      patchedRef,
				Mode:            string(mode),
				Status:          string(types.PatchStatusPatching),
				ActivityID:      mo.EmptyableToOption(activityID).ToPointer(),
			}
			return created, s.db.WithContext(ctx).Create(created).Error
		}
		// A retried record runs again under this attempt's activity, without the earlier attempt's error.
		retried := &ImagePatchRecord{ //exhaustruct:enforce
			BaseModel:       existing.BaseModel,
			EnvironmentID:   existing.EnvironmentID,
			OriginalImageID: existing.OriginalImageID,
			OriginalRef:     existing.OriginalRef,
			OriginalDigest:  existing.OriginalDigest,
			PatchedRef:      existing.PatchedRef,
			Mode:            existing.Mode,
			Status:          string(types.PatchStatusPatching),
			PackagesUpdated: existing.PackagesUpdated,
			Error:           nil,
			ActivityID:      mo.EmptyableToOption(activityID).ToPointer(),
			DurationMs:      existing.DurationMs,
		}
		return retried, s.db.WithContext(ctx).Model(retried).Select("status", "error", "activity_id").Updates(retried).Error
	}()
	if saveErr != nil {
		// No worker will run, so settle the activity and release its cancel registration here.
		saveErr = fmt.Errorf("failed to save image patch record: %w", saveErr)
		s.completePatchActivity(runCtx, activityID, false, saveErr.Error())
		return nil, nil, saveErr
	}

	slog.InfoContext(ctx, "image patch queued", "environmentId", envID, "imageRef", imageRef, "patchedRef", patchedRef, "mode", mode, "patchTarget", copaImageRef)
	return record, func() error { return s.runPatch(runCtx, record, opts, []byte(report.Data), copaImageRef, activityID) }, nil
}

// runPatch executes one recorded patch with Copacetic and settles its record and activity.
func (s *Service) runPatch(ctx context.Context, record *ImagePatchRecord, opts types.PatchOptions, reportData []byte, copaImageRef, activityID string) (runErr error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "image.patch", trace.WithAttributes(
		attribute.String("arcane.environment.id", record.EnvironmentID),
		attribute.String("arcane.image.patch_id", record.ID),
		attribute.String("arcane.image.ref", record.OriginalRef),
		attribute.String("arcane.image.patched_ref", record.PatchedRef),
		attribute.String("arcane.image.patch_mode", record.Mode),
		attribute.String("arcane.activity.id", activityID),
	))
	defer func() { tracing.End(span, runErr) }()
	fail := func(err error, durationMs int64) error {
		s.finishPatchRecord(ctx, record, types.PatchStatusFailed, err.Error(), nil, durationMs)
		s.completePatchActivity(ctx, activityID, false, err.Error())
		return err
	}
	select {
	case s.patchSlot <- struct{}{}:
		defer func() { <-s.patchSlot }()
	case <-ctx.Done():
		return fail(ctx.Err(), 0)
	}

	if record.Mode == string(types.PatchModeReport) {
		if err := s.settingsService.RequireFeature(ctx, types.FeatureVulnerabilityManagement); err != nil {
			return fail(err, 0)
		}
	}

	// Copa consumes the scan report as a file; materialize the stored report
	// into a temp file for the duration of the run.
	reportPath, err := func() (string, error) {
		if len(reportData) == 0 {
			return "", nil
		}
		reportDir, mkdirErr := os.MkdirTemp("", "arcane-patch-report")
		if mkdirErr != nil {
			return "", mkdirErr
		}
		return filepath.Join(reportDir, "report.json"), nil
	}()
	if err != nil {
		return fail(err, 0)
	}
	if reportPath != "" {
		defer func() {
			if cleanupErr := os.RemoveAll(filepath.Dir(reportPath)); cleanupErr != nil {
				slog.WarnContext(ctx, "Failed to remove image patch temporary directory", "directory", filepath.Dir(reportPath), "error", cleanupErr)
			}
		}()
		if writeErr := os.WriteFile(reportPath, reportData, 0o600); writeErr != nil {
			return fail(writeErr, 0)
		}
	}

	startTime := time.Now()

	// VEX output is only produced when patching from a report; use it to
	// count how many packages were actually updated.
	vexOutputPath := func() string {
		if reportPath == "" {
			return ""
		}
		vexDir, mkdirErr := os.MkdirTemp("", "arcane-patch-vex")
		if mkdirErr != nil {
			return ""
		}
		return filepath.Join(vexDir, "vex.json")
	}()
	if vexOutputPath != "" {
		defer func() {
			if cleanupErr := os.RemoveAll(filepath.Dir(vexOutputPath)); cleanupErr != nil {
				slog.WarnContext(ctx, "Failed to remove image patch temporary directory", "directory", filepath.Dir(vexOutputPath), "error", cleanupErr)
			}
		}()
	}

	// Mirror copa's log output into the activity while the patch runs; BuildKit
	// step progress stays quiet until copa exposes a progress writer upstream.
	s.appendPatchActivity(ctx, activityID, 30, "Patching image packages via BuildKit")
	patchOut := activitylib.NewWriter(ctx, s.activityService, activityID, nil, "Patching image")
	removeMirror := logging.AddLogrusMirror(patchOut)
	patchErr := patch.Patch(ctx, &copatypes.Options{
		Image:  copaImageRef,
		Report: reportPath,
		// The final tag was already resolved into the record; pass it verbatim
		// so copa produces exactly the recorded reference.
		PatchedTag:  record.PatchedRef[strings.LastIndex(record.PatchedRef, ":")+1:],
		Timeout:     time.Duration(cmp.Or(max(opts.TimeoutSeconds, 0), max(s.settingsService.GetSettingsConfig().ImagePatchTimeoutSec.AsInt(), 0), 600)) * time.Second,
		Scanner:     kit.Ternary(reportPath != "", "trivy", ""),
		Format:      kit.Ternary(vexOutputPath != "", "openvex", ""),
		Output:      vexOutputPath,
		Progress:    progressui.QuietMode,
		BkAddr:      "docker://",
		IgnoreError: opts.IgnoreErrors,
		PkgTypes:    kit.Ternary(reportPath != "", "os", ""),
	})
	removeMirror()
	activitylib.FlushWriter(patchOut)
	durationMs := time.Since(startTime).Milliseconds()

	if patchErr != nil {
		if errors.Is(patchErr, copatypes.ErrNoUpdatesFound) {
			patchErr = errors.New("no OS package updates available for this image")
		}
		if strings.Contains(patchErr.Error(), `exporter "docker" could not be found`) {
			patchErr = common.ErrPatchRequiresContainerdImageStore
		}
		// Interrupted mid-run, Copacetic may already have replaced the tag, so the record must never rerun.
		if ctx.Err() != nil {
			patchErr = errPatchInterrupted
		}
		slog.WarnContext(ctx, "image patch failed", "environmentId", record.EnvironmentID, "imageRef", record.OriginalRef, "durationMs", durationMs, "error", patchErr)
		return fail(patchErr, durationMs)
	}

	// Count VEX statements as the number of patched packages, when available.
	packagesUpdated := func() *int {
		if vexOutputPath == "" {
			return nil
		}
		data, readErr := acfs.ReadFile(ctx, filepath.Dir(vexOutputPath), "/"+filepath.Base(vexOutputPath))
		if readErr != nil {
			return nil
		}
		var doc struct {
			Statements []jsontext.Value `json:"statements"`
		}
		if unmarshalErr := json.Unmarshal(data, &doc); unmarshalErr != nil {
			return nil
		}
		return new(len(doc.Statements))
	}()

	s.verifyPatchedImage(ctx, record, activityID)

	s.finishPatchRecord(ctx, record, types.PatchStatusCompleted, "", packagesUpdated, durationMs)
	slog.InfoContext(ctx, "image patch completed", "environmentId", record.EnvironmentID, "imageRef", record.OriginalRef, "patchedRef", record.PatchedRef, "durationMs", durationMs)
	s.completePatchActivity(ctx, activityID, true, "")
	return nil
}

// verifyPatchedImage warns when the expected tag is missing (copa may arch-suffix
// multi-platform tags) and otherwise re-scans it to show whether the patch worked.
func (s *Service) verifyPatchedImage(ctx context.Context, record *ImagePatchRecord, activityID string) {
	if patchedInspect, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
		return dockerClient.ImageInspect(ctx, record.PatchedRef)
	}); inspectErr != nil {
		slog.WarnContext(ctx, "patched image tag not found after patching", "patchedRef", record.PatchedRef, "error", inspectErr)
		s.appendPatchActivity(ctx, activityID, 95, "Patched image was created but the expected tag "+record.PatchedRef+" was not found; check the image list")
	} else if s.settingsService.IsFeatureEnabled(ctx, types.FeatureVulnerabilityManagement) {
		s.appendPatchActivity(ctx, activityID, 95, "Re-scanning patched image to verify the patch")
		if _, scanErr := s.vulnerabilityService.ScanImage(context.WithoutCancel(ctx), record.EnvironmentID, patchedInspect.ID, scheduledPatchUser); scanErr != nil {
			slog.WarnContext(ctx, "failed to start verification scan of patched image", "patchedRef", record.PatchedRef, "error", scanErr)
		}
	}
}

// platformPinnedRef resolves a tag reference to the manifest digest of
// the given platform when the registry serves a multi-platform index. This
// makes copa patch a single platform (keeping the plain patched tag) instead
// of every platform in the index. Returns "" when the reference is not a
// multi-platform index or cannot be resolved, in which case the plain tag is
// used and copa's own discovery decides.
func platformPinnedRef(ctx context.Context, imageRef string, target v1.Platform) string {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return ""
	}
	desc, err := remote.Get(ref, remote.WithContext(ctx), remote.WithTransport(registryTransport), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil || !desc.MediaType.IsIndex() {
		return ""
	}
	idx, err := desc.ImageIndex()
	if err != nil {
		return ""
	}
	manifest, err := idx.IndexManifest()
	if err != nil {
		return ""
	}

	want := platforms.Normalize(target)
	for i := range manifest.Manifests {
		m := &manifest.Manifests[i]
		if m.Platform == nil {
			continue
		}
		got := platforms.Normalize(v1.Platform{
			OS:           m.Platform.OS,
			Architecture: m.Platform.Architecture,
			Variant:      m.Platform.Variant,
		})
		if got.OS == want.OS && got.Architecture == want.Architecture && got.Variant == want.Variant {
			return ref.Context().Name() + "@" + m.Digest.String()
		}
	}
	return ""
}

// resolvePatchedRef computes the patched reference with the exact same
// resolution copa applies internally, so the recorded ref matches the result.
func resolvePatchedRef(imageRef, patchedTag, suffix string) (string, error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}
	localName, tag, err := copacommon.ResolvePatchedImageName(named, patchedTag, suffix)
	if err != nil {
		return "", fmt.Errorf("failed to resolve patched image name: %w", err)
	}
	return localName + ":" + tag, nil
}

// writeRegistryAuthConfig merges Arcane's registry credentials into the
// docker config file copa reads for BuildKit registry auth. It only touches the
// config when DOCKER_CONFIG is set, i.e. the deployment (normally Arcane's own
// startup) owns that directory — a user's ~/.docker/config.json is never modified.
func (s *Service) writeRegistryAuthConfig(ctx context.Context) {
	configDir := strings.TrimSpace(os.Getenv("DOCKER_CONFIG"))
	if configDir == "" {
		return
	}

	authConfigs, err := s.registryService.GetAllRegistryAuthConfigs(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to load registry credentials for image patching", "error", err)
		return
	}
	arcaneConfig, err := vuln.BuildDockerConfigJSON(authConfigs)
	if err != nil || len(arcaneConfig) == 0 {
		return
	}

	var arcane struct {
		Auths map[string]jsontext.Value `json:"auths"`
	}
	if unmarshalErr := json.Unmarshal(arcaneConfig, &arcane); unmarshalErr != nil {
		return
	}

	if mkdirAllErr := acfs.MkdirAll(ctx, filepath.Dir(configDir), "/"+filepath.Base(configDir), 0o700); mkdirAllErr != nil {
		slog.WarnContext(ctx, "failed to create docker config directory for image patching", "path", configDir, "error", mkdirAllErr)
		return
	}

	merged := map[string]jsontext.Value{}
	if existing, readFileErr := acfs.ReadFile(ctx, configDir, "/config.json"); readFileErr == nil {
		if decodeExistingConfigErr := json.Unmarshal(existing, &merged); decodeExistingConfigErr != nil {
			slog.WarnContext(ctx, "existing docker config is not valid JSON; leaving it untouched", "path", configDir, "error", decodeExistingConfigErr)
			return
		}
	}

	auths := map[string]jsontext.Value{}
	if raw, ok := merged["auths"]; ok {
		if decodeAuthConfigErr := json.Unmarshal(raw, &auths); decodeAuthConfigErr != nil {
			auths = map[string]jsontext.Value{}
		}
	}
	maps.Copy(auths, arcane.Auths)
	authsRaw, err := json.Marshal(auths)
	if err != nil {
		return
	}
	merged["auths"] = authsRaw

	payload, err := json.Marshal(merged)
	if err != nil {
		return
	}
	if writeErr := acfs.Write(ctx, configDir, "/config.json", payload, acfs.WriteOptions{Mode: 0o600}); writeErr != nil {
		slog.WarnContext(ctx, "failed to write docker config for image patching", "path", configDir, "error", writeErr)
	}
}

func (s *Service) finishPatchRecord(ctx context.Context, record *ImagePatchRecord, status types.PatchStatus, errMessage string, packagesUpdated *int, durationMs int64) {
	updates := map[string]any{
		"status":      string(status),
		"duration_ms": durationMs,
	}
	if errMessage != "" {
		updates["error"] = errMessage
	}
	if packagesUpdated != nil {
		updates["packages_updated"] = *packagesUpdated
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.db.WithContext(writeCtx).
		Model(&ImagePatchRecord{}).
		Where("id = ?", record.ID).
		Updates(updates).Error; err != nil {
		slog.WarnContext(ctx, "failed to update image patch record", "error", err, "patchId", record.ID)
	}
}

func (s *Service) RecoverInterruptedPatches(ctx context.Context) error {
	if err := s.db.WithContext(ctx).
		Model(&ImagePatchRecord{}).
		Where("status = ?", string(types.PatchStatusPatching)).
		Updates(map[string]any{
			"status": string(types.PatchStatusFailed),
			"error":  errPatchInterrupted.Error(),
		}).Error; err != nil {
		return fmt.Errorf("failed to fail interrupted image patches: %w", err)
	}
	return nil
}

// ListPatches returns the paginated patch history for an environment.
func (s *Service) ListPatches(ctx context.Context, envID string, params pagination.QueryParams) ([]types.PatchRecord, pagination.Response, error) {
	var records []ImagePatchRecord
	q := s.db.WithContext(ctx).Model(&ImagePatchRecord{}).Where("environment_id = ?", envID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where("original_ref LIKE ? OR patched_ref LIKE ?", searchPattern, searchPattern)
	}
	q = pagination.ApplyFilter(q, "status", params.Filters["status"])

	paginationResp, err := pagination.PaginateAndSortDB(pagination.QueryParams{ //exhaustruct:enforce
		SearchQuery: params.SearchQuery,
		SortParams: pagination.SortParams{ //exhaustruct:enforce
			Sort:  cmp.Or(params.Sort, "createdAt"),
			Order: params.Order,
		},
		Params:  params.Params,
		Filters: params.Filters,
	}, q, &records)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate image patches: %w", err)
	}

	dtos := make([]types.PatchRecord, 0, len(records))
	for i := range records {
		dtos = append(dtos, records[i].ToDto())
	}
	return dtos, paginationResp, nil
}

// PatchedRefs returns every name a completed patch output can be scanned under
// (normalized and familiar), used to keep patch outputs from being patched again.
func (s *Service) PatchedRefs(ctx context.Context, envID string) (map[string]struct{}, error) {
	var refs []string
	if err := s.db.WithContext(ctx).
		Model(&ImagePatchRecord{}).
		Where("environment_id = ? AND status = ?", envID, string(types.PatchStatusCompleted)).
		Distinct().
		Pluck("patched_ref", &refs).Error; err != nil {
		return nil, fmt.Errorf("failed to list patched image refs: %w", err)
	}
	set := make(map[string]struct{}, len(refs)*2)
	for _, ref := range refs {
		set[ref] = struct{}{}
		if named, err := reference.ParseNormalizedNamed(ref); err == nil {
			set[reference.FamiliarString(named)] = struct{}{}
		}
	}
	return set, nil
}

func (s *Service) AggregatePatchTargets(ctx context.Context, params pagination.QueryParams) (types.Paginated[types.PatchTarget], error) {
	collection, err := httpx.CollectEnvironments[types.Paginated[types.PatchTarget]](ctx, url.Values{"start": {"0"}, "limit": {"-1"}})
	if err != nil {
		return types.Paginated[types.PatchTarget]{}, fmt.Errorf("collect environment patch targets: %w", err)
	}
	pages := make([]pagination.EnvironmentPage[types.PatchTarget], 0, len(collection.Results))
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[types.PatchTarget]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
	}
	items, resp := pagination.MergeEnvironmentPages(pagination.Config[types.PatchTarget]{
		SortBindings: []pagination.SortBinding[types.PatchTarget]{
			{
				Key: "scanTime",
				Fn: func(a, b types.PatchTarget) int {
					return cmp.Or(a.ScanTime.Compare(b.ScanTime), strings.Compare(a.ImageID, b.ImageID))
				},
				DescFn: func(a, b types.PatchTarget) int {
					return cmp.Or(b.ScanTime.Compare(a.ScanTime), strings.Compare(a.ImageID, b.ImageID))
				},
			},
			{Key: "imageName", Fn: func(a, b types.PatchTarget) int { return strings.Compare(a.ImageRef, b.ImageRef) }},
			{Key: "fixableCount", Fn: func(a, b types.PatchTarget) int { return cmp.Compare(a.FixableCount, b.FixableCount) }},
		},
	}, pages, params)
	return types.Paginated[types.PatchTarget]{
		Success:            true,
		Data:               items,
		Pagination:         handlerutil.PaginationResponse(resp),
		FailedEnvironments: collection.Failures,
	}, nil
}

// ListPatchTargets returns scanned images with their fixable-vulnerability
// counts and latest patch run, for the security page's patching view. Images
// that are themselves patch outputs are folded into their original's row: they
// are excluded from the list and surface as that row's LastPatchScan instead.
func (s *Service) ListPatchTargets(ctx context.Context, envID string, params pagination.QueryParams) ([]types.PatchTarget, pagination.Response, error) {
	if err := s.settingsService.RequireFeature(ctx, types.FeatureVulnerabilityManagement); err != nil {
		return nil, pagination.Response{}, err
	}
	patchedRefs, err := s.PatchedRefs(ctx, envID)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	excludedNames := slices.Collect(maps.Keys(patchedRefs))

	// Only list images that are actionable (fixable vulnerabilities) or carry
	// patch history.
	patchedImageIDs := s.db.Model(&ImagePatchRecord{}).
		Distinct().
		Select("original_image_id").
		Where("environment_id = ?", envID)
	reportImageIDs := s.db.Model(&vulnerability.VulnerabilityReportRecord{}).Select("image_id")
	q := s.db.WithContext(ctx).
		Model(&vulnerability.VulnerabilityScanRecord{}).
		Where("status = ?", vulnerability.ScanStatusCompleted).
		Where("id IN (?)", reportImageIDs).
		Where("(fixable_count > 0 OR id IN (?))", patchedImageIDs).
		Where("image_name NOT LIKE 'sha256:%' AND image_name NOT LIKE '%<none>%' AND image_name <> id")
	if len(excludedNames) > 0 {
		q = q.Where("image_name NOT IN ?", excludedNames)
	}
	q = pagination.ApplyLikeSearch(q, params.Search, "image_name LIKE ?")

	var scans []vulnerability.VulnerabilityScanRecord
	paginationResp, err := pagination.PaginateAndSortDB(pagination.QueryParams{ //exhaustruct:enforce
		SearchQuery: params.SearchQuery,
		SortParams: pagination.SortParams{ //exhaustruct:enforce
			Sort:  cmp.Or(params.Sort, "scanTime"),
			Order: params.Order,
		},
		Params:  params.Params,
		Filters: params.Filters,
	}, q, &scans)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list patch targets: %w", err)
	}

	if len(scans) == 0 {
		return []types.PatchTarget{}, paginationResp, nil
	}

	// Best-effort: mark images without a registry source so the UI can explain
	// why they cannot be patched.
	repoDigestsByID := map[string][]string{}
	if images, listImagesErr := s.dockerService.ListImages(ctx); listImagesErr == nil {
		for i := range images {
			repoDigestsByID[images[i].ID] = images[i].RepoDigests
		}
	} else {
		slog.WarnContext(ctx, "failed to list images for patch targets", "error", listImagesErr)
	}

	imageIDs := make([]string, 0, len(scans))
	for i := range scans {
		imageIDs = append(imageIDs, scans[i].ID)
	}
	lastPatchByImageID, lastPatchScanByImageID, err := s.latestPatchesByImage(ctx, envID, imageIDs)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	targets := make([]types.PatchTarget, 0, len(scans))
	for i := range scans {
		scan := &scans[i]
		targets = append(targets, types.PatchTarget{
			ImageID:      scan.ID,
			ImageRef:     scan.ImageName,
			FixableCount: kit.FromPtr(scan.FixableCount),
			TotalCount:   scan.TotalCount,
			ScanTime:     scan.ScanTime,
			LocalOnly: func() bool {
				digests, ok := repoDigestsByID[scan.ID]
				return ok && !slices.ContainsFunc(digests, func(digest string) bool { return digest != "<none>@<none>" })
			}(),
			LastPatch: func() *types.PatchRecord {
				lastPatch, ok := lastPatchByImageID[scan.ID]
				if !ok {
					return nil
				}
				return new(lastPatch.ToDto())
			}(),
			LastPatchScan: lastPatchScanByImageID[scan.ID],
		})
	}

	return targets, paginationResp, nil
}

// latestPatchesByImage loads the most recent patch run per original
// image in two batched queries, plus the scan of each completed patch's output
// image so the original's row can show whether the patch worked.
func (s *Service) latestPatchesByImage(ctx context.Context, envID string, imageIDs []string) (map[string]*ImagePatchRecord, map[string]*types.PatchScanSummary, error) {
	var patchRows []ImagePatchRecord
	if err := s.db.WithContext(ctx).
		Where("environment_id = ? AND original_image_id IN ?", envID, imageIDs).
		Order("original_image_id, created_at DESC, id").
		Find(&patchRows).Error; err != nil {
		return nil, nil, fmt.Errorf("failed to load latest image patches: %w", err)
	}
	lastPatchByImageID := make(map[string]*ImagePatchRecord, len(imageIDs))
	patchedNamesByImageID := make(map[string][]string, len(imageIDs))
	var patchedNames []string
	for i := range patchRows {
		row := &patchRows[i]
		if _, seen := lastPatchByImageID[row.OriginalImageID]; seen {
			continue
		}
		lastPatchByImageID[row.OriginalImageID] = row
		if row.Status != string(types.PatchStatusCompleted) {
			continue
		}
		names := []string{row.PatchedRef}
		if named, err := reference.ParseNormalizedNamed(row.PatchedRef); err == nil {
			names = append(names, reference.FamiliarString(named))
		}
		patchedNamesByImageID[row.OriginalImageID] = names
		patchedNames = append(patchedNames, names...)
	}

	lastPatchScanByImageID := make(map[string]*types.PatchScanSummary, len(patchedNamesByImageID))
	if len(patchedNames) == 0 {
		return lastPatchByImageID, lastPatchScanByImageID, nil
	}
	var patchScans []vulnerability.VulnerabilityScanRecord
	if err := s.db.WithContext(ctx).
		Select("image_name", "status", "fixable_count", "total_count", "scan_time").
		Where("image_name IN ?", patchedNames).
		Order("scan_time DESC").
		Find(&patchScans).Error; err != nil {
		return nil, nil, fmt.Errorf("failed to load patched image scans: %w", err)
	}
	latestScanByName := make(map[string]*vulnerability.VulnerabilityScanRecord, len(patchScans))
	for i := range patchScans {
		if _, seen := latestScanByName[patchScans[i].ImageName]; !seen {
			latestScanByName[patchScans[i].ImageName] = &patchScans[i]
		}
	}
	for imageID, names := range patchedNamesByImageID {
		var latest *vulnerability.VulnerabilityScanRecord
		for _, name := range names {
			if candidate, ok := latestScanByName[name]; ok && (latest == nil || candidate.ScanTime.After(latest.ScanTime)) {
				latest = candidate
			}
		}
		if latest != nil {
			lastPatchScanByImageID[imageID] = &types.PatchScanSummary{
				Status:       latest.Status,
				FixableCount: kit.FromPtr(latest.FixableCount),
				TotalCount:   latest.TotalCount,
				ScanTime:     latest.ScanTime,
			}
		}
	}
	return lastPatchByImageID, lastPatchScanByImageID, nil
}

// Targets lists images whose latest completed scan found fixable vulnerabilities
// and kept a report, skipping patch outputs and images patched since that scan.
func (s *Service) Targets(ctx context.Context, envID string) ([]Target, error) {
	if err := s.settingsService.RequireFeature(ctx, types.FeatureVulnerabilityManagement); err != nil {
		return nil, err
	}
	if storeErr := s.requireContainerdImageStore(ctx); storeErr != nil {
		return nil, storeErr
	}
	var scans []vulnerability.VulnerabilityScanRecord
	if scansErr := s.db.WithContext(ctx).
		Where("status = ? AND fixable_count > 0", vulnerability.ScanStatusCompleted).
		Where("id IN (?)", s.db.Model(&vulnerability.VulnerabilityReportRecord{}).Select("image_id")).
		Where("image_name NOT LIKE 'sha256:%' AND image_name NOT LIKE '%<none>%' AND image_name <> id").
		Find(&scans).Error; scansErr != nil {
		return nil, fmt.Errorf("failed to list vulnerability scans: %w", scansErr)
	}
	patchedRefs, err := s.PatchedRefs(ctx, envID)
	if err != nil {
		return nil, err
	}

	targets := make([]Target, 0, len(scans))
	for i := range scans {
		scan := &scans[i]
		if _, isPatchOutput := patchedRefs[scan.ImageName]; isPatchOutput {
			continue
		}
		// Skip images already patched, or being patched, since their latest scan.
		var recent int64
		if countErr := s.db.WithContext(ctx).
			Model(&ImagePatchRecord{}).
			Where("environment_id = ? AND original_image_id = ? AND status IN ? AND created_at >= ?",
				envID, scan.ID, []string{string(types.PatchStatusCompleted), string(types.PatchStatusPatching)}, scan.ScanTime).
			Count(&recent).Error; countErr == nil && recent > 0 {
			continue
		}
		targets = append(targets, Target{ImageID: scan.ID, ImageName: scan.ImageName})
	}
	return targets, nil
}

// Copa needs BuildKit's docker exporter, which dockerd only offers with the containerd image store.
func (s *Service) requireContainerdImageStore(ctx context.Context) error {
	info, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SystemInfoResult, error) {
		return dockerClient.Info(ctx, client.InfoOptions{})
	})
	if err != nil {
		return fmt.Errorf("failed to inspect Docker: %w", err)
	}
	for _, status := range info.Info.DriverStatus {
		if status[0] == "driver-type" && status[1] == "io.containerd.snapshotter.v1" {
			return nil
		}
	}
	return common.ErrPatchRequiresContainerdImageStore
}

func (s *Service) appendPatchActivity(ctx context.Context, activityID string, progress int, message string) {
	if activityID == "" {
		return
	}
	if _, err := s.activityService.AppendMessage(ctx, activityID, activity.AppendActivityMessageRequest{
		Level:    types.ActivityMessageLevelInfo,
		Message:  message,
		Progress: new(progress),
	}); err != nil {
		slog.DebugContext(ctx, "failed to append image patch activity message", "activityId", activityID, "error", err)
	}
}

func (s *Service) completePatchActivity(ctx context.Context, activityID string, success bool, errMessage string) {
	if activityID == "" {
		return
	}

	status, message, errorPtr := func() (types.ActivityStatus, string, *string) {
		switch {
		case success:
			return types.ActivityStatusSuccess, "Image patch completed", nil
		case activitylib.CancelledByContext(ctx):
			return types.ActivityStatusCancelled, "Image patch cancelled", nil
		default:
			return types.ActivityStatusFailed, cmp.Or(errMessage, "Image patch failed"), mo.EmptyableToOption(errMessage).ToPointer()
		}
	}()

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, err := s.activityService.CompleteActivity(writeCtx, activityID, status, message, errorPtr); err != nil {
		slog.DebugContext(ctx, "failed to complete image patch activity", "activityId", activityID, "error", err)
	}
}
