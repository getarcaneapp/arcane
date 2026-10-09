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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/features"
	"github.com/getarcaneapp/arcane/types/v2/imagepatch"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/moby/moby/client"
	"github.com/opencontainers/image-spec/specs-go/v1"
	copacommon "github.com/project-copacetic/copacetic/pkg/common"
	"github.com/project-copacetic/copacetic/pkg/patch"
	"github.com/project-copacetic/copacetic/pkg/types"
	"github.com/samber/mo"
	"go.getarcane.app/acfs"

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
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/logging"
)

// scheduledPatchUser is the actor recorded for scheduled patches and their verification scans.
var (
	scheduledPatchUser = usertypes.Actor{Username: "System"}
	// errPatchInterrupted fails a patch cut off mid-run; its record keeps the message so it never reruns.
	errPatchInterrupted = errors.New("image patch was interrupted before its result was recorded; start a new patch")
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
	if os.Getenv("DOCKER_HOST") == "" && dockerService != nil {
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
func (s *Service) PatchImage(ctx context.Context, envID, imageID string, opts imagepatch.PatchOptions, user usertypes.Actor) (*imagepatch.PatchRecord, error) {
	if opts.ScanID != "" {
		if err := s.settingsService.RequireFeature(ctx, features.VulnerabilityManagement); err != nil {
			return nil, err
		}
	}
	record, run, err := s.startPatch(utils.ActivityRuntimeContext(ctx, nil), envID, imageID, opts, user, "")
	if err != nil {
		return nil, err
	}
	go func() { _ = run() }()
	dto := record.ToDto()
	return &dto, nil
}

// PatchTarget patches one scanned image and waits for the result. recordID keys
// the patch so a redelivered task resumes its own record instead of patching twice.
func (s *Service) PatchTarget(ctx context.Context, envID string, target Target, recordID string) error {
	_, run, err := s.startPatch(ctx, envID, target.ImageID, imagepatch.PatchOptions{ScanID: target.ImageID}, scheduledPatchUser, recordID)
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
	opts imagepatch.PatchOptions,
	user usertypes.Actor,
	recordID string,
) (*ImagePatchRecord, func() error, error) {
	var existing ImagePatchRecord
	if recordID != "" {
		if err := s.db.WithContext(ctx).Where("id = ?", recordID).Limit(1).Find(&existing).Error; err != nil {
			return nil, nil, fmt.Errorf("failed to load image patch record: %w", err)
		}
		if existing.Status == string(imagepatch.PatchStatusCompleted) {
			return &existing, nil, nil
		}
		// A record still patching was cut off mid-run, so whether Copacetic already replaced its tag is unknown.
		// Its failure keeps that message, so no later delivery runs the patch again either.
		if existing.Status == string(imagepatch.PatchStatusPatching) || mo.PointerToOption(existing.Error).OrEmpty() == errPatchInterrupted.Error() {
			s.finishPatchRecord(ctx, &existing, imagepatch.PatchStatusFailed, errPatchInterrupted.Error(), nil, 0)
			return nil, nil, errPatchInterrupted
		}
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}
	if storeErr := requireContainerdImageStore(ctx, dockerClient); storeErr != nil {
		return nil, nil, storeErr
	}
	imageInspect, err := dockerClient.ImageInspect(ctx, imageID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to inspect image: %w", err)
	}
	if len(imageInspect.RepoTags) == 0 {
		return nil, nil, common.ErrImageUntagged
	}
	imageRef := imageInspect.RepoTags[0]
	if existing.ID != "" {
		// A replay patches the source its record names, so the output matches the saved PatchedRef.
		if !slices.Contains(imageInspect.RepoTags, existing.OriginalRef) {
			return nil, nil, fmt.Errorf("image is no longer tagged %s; start a new patch", existing.OriginalRef)
		}
		imageRef = existing.OriginalRef
	}
	// Never pulled or pushed: BuildKit cannot fetch the image to patch it.
	if len(imageInspect.RepoDigests) == 0 {
		return nil, nil, common.ErrImageLocalOnly
	}

	mode := imagepatch.PatchModeUpdateAll
	var reportData []byte
	if opts.ScanID != "" {
		// Scan records are keyed by image ID; refuse a scan of a different image than the route selected.
		if opts.ScanID != imageInspect.ID {
			return nil, nil, common.ErrPatchScanImageMismatch
		}
		var report vulnerability.VulnerabilityReportRecord
		if reportErr := s.db.WithContext(ctx).First(&report, "image_id = ?", opts.ScanID).Error; reportErr != nil || report.Data == "" {
			return nil, nil, common.ErrPatchScanReportUnavailable
		}
		reportData = []byte(report.Data)
		mode = imagepatch.PatchModeReport
	}

	suffix := cmp.Or(strings.TrimSpace(opts.Suffix), strings.TrimSpace(s.settingsService.GetSettingsConfig().ImagePatchSuffix.Value), "patched")
	patchedRef, err := resolvePatchedRef(imageRef, opts.PatchedTag, suffix)
	if err != nil {
		return nil, nil, err
	}

	// Copa reads registry credentials from the docker config; refresh it before any registry lookups.
	s.writeRegistryAuthConfig(ctx)

	// Unless the all-platforms setting is on, pin update-all patches to this image's platform manifest so copa
	// patches one platform and keeps the plain patched tag. Report-mode patches are always single-platform.
	copaImageRef := imageRef
	if mode == imagepatch.PatchModeUpdateAll && !s.settingsService.GetSettingsConfig().ImagePatchAllPlatforms.IsTrue() {
		copaImageRef = cmp.Or(platformPinnedRef(ctx, imageRef, v1.Platform{
			OS:           imageInspect.Os,
			Architecture: imageInspect.Architecture,
			Variant:      imageInspect.Variant,
		}), imageRef)
	}

	activityID := ""
	if s.activityService != nil {
		// Each attempt gets its own activity, since a finished activity never reopens.
		started, startErr := s.activityService.StartActivity(ctx, activity.StartActivityRequest{
			EnvironmentID: envID,
			Type:          activitytypes.TypeImagePatch,
			ResourceType:  new("image"),
			ResourceID:    &imageID,
			ResourceName:  &imageRef,
			StartedBy:     &user,
			Progress:      new(0),
			LatestMessage: "Image patch queued",
		})
		if startErr != nil {
			slog.WarnContext(ctx, "failed to create image patch activity", "error", startErr, "imageRef", imageRef)
		} else {
			activityID = started.ID
		}
	}
	runCtx := s.activityService.Track(ctx, activityID)

	record := &existing
	var saveErr error
	if record.ID == "" {
		record = &ImagePatchRecord{
			EnvironmentID:   envID,
			OriginalImageID: imageInspect.ID,
			OriginalRef:     imageRef,
			OriginalDigest:  imageInspect.RepoDigests[0],
			PatchedRef:      patchedRef,
			Mode:            string(mode),
			Status:          string(imagepatch.PatchStatusPatching),
			ActivityID:      mo.EmptyableToOption(activityID).ToPointer(),
		}
		record.ID = recordID
		saveErr = s.db.WithContext(ctx).Create(record).Error
	} else {
		// A retried record runs again under this attempt's activity, without the earlier attempt's error.
		record.Status, record.Error, record.ActivityID = string(imagepatch.PatchStatusPatching), nil, mo.EmptyableToOption(activityID).ToPointer()
		saveErr = s.db.WithContext(ctx).Model(record).Select("status", "error", "activity_id").Updates(record).Error
	}
	if saveErr != nil {
		// No worker will run, so settle the activity and release its cancel registration here.
		saveErr = fmt.Errorf("failed to save image patch record: %w", saveErr)
		s.completePatchActivity(runCtx, activityID, false, saveErr.Error())
		return nil, nil, saveErr
	}

	slog.InfoContext(ctx, "image patch queued", "environmentId", envID, "imageRef", imageRef, "patchedRef", patchedRef, "mode", mode, "patchTarget", copaImageRef)
	return record, func() error { return s.runPatch(runCtx, record, opts, reportData, copaImageRef, activityID) }, nil
}

// runPatch executes one recorded patch with Copacetic and settles its record and activity.
func (s *Service) runPatch(ctx context.Context, record *ImagePatchRecord, opts imagepatch.PatchOptions, reportData []byte, copaImageRef, activityID string) error {
	fail := func(err error, durationMs int64) error {
		s.finishPatchRecord(ctx, record, imagepatch.PatchStatusFailed, err.Error(), nil, durationMs)
		s.completePatchActivity(ctx, activityID, false, err.Error())
		return err
	}
	select {
	case s.patchSlot <- struct{}{}:
		defer func() { <-s.patchSlot }()
	case <-ctx.Done():
		return fail(ctx.Err(), 0)
	}

	if record.Mode == string(imagepatch.PatchModeReport) {
		if err := s.settingsService.RequireFeature(ctx, features.VulnerabilityManagement); err != nil {
			return fail(err, 0)
		}
	}

	// Copa consumes the scan report as a file; materialize the stored report
	// into a temp file for the duration of the run.
	reportPath := ""
	if len(reportData) > 0 {
		reportDir, err := os.MkdirTemp("", "arcane-patch-report")
		if err == nil {
			reportPath = filepath.Join(reportDir, "report.json")
			err = os.WriteFile(reportPath, reportData, 0o600)
			defer func() {
				if cleanupErr := os.RemoveAll(reportDir); cleanupErr != nil {
					slog.WarnContext(ctx, "Failed to remove image patch temporary directory", "directory", reportDir, "error", cleanupErr)
				}
			}()
		}
		if err != nil {
			return fail(err, 0)
		}
	}

	startTime := time.Now()

	timeoutSeconds := opts.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = s.settingsService.GetSettingsConfig().ImagePatchTimeoutSec.AsInt()
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 600
	}

	copaOpts := &types.Options{
		Image:  copaImageRef,
		Report: reportPath,
		// The final tag was already resolved into the record; pass it verbatim
		// so copa produces exactly the recorded reference.
		PatchedTag:  record.PatchedRef[strings.LastIndex(record.PatchedRef, ":")+1:],
		Timeout:     time.Duration(timeoutSeconds) * time.Second,
		Progress:    progressui.QuietMode,
		BkAddr:      "docker://",
		IgnoreError: opts.IgnoreErrors,
	}

	var vexOutputPath string
	if reportPath != "" {
		copaOpts.Scanner = "trivy"
		copaOpts.PkgTypes = "os"
		// VEX output is only produced when patching from a report; use it to
		// count how many packages were actually updated.
		if vexDir, err := os.MkdirTemp("", "arcane-patch-vex"); err == nil {
			vexOutputPath = filepath.Join(vexDir, "vex.json")
			copaOpts.Format = "openvex"
			copaOpts.Output = vexOutputPath
			defer func() {
				if cleanupErr := os.RemoveAll(vexDir); cleanupErr != nil {
					slog.WarnContext(ctx, "Failed to remove image patch temporary directory", "directory", vexDir, "error", cleanupErr)
				}
			}()
		}
	}

	// Mirror copa's log output into the activity while the patch runs; BuildKit
	// step progress stays quiet until copa exposes a progress writer upstream.
	s.appendPatchActivity(ctx, activityID, 30, "Patching image packages via BuildKit")
	patchOut := activitylib.NewWriter(ctx, s.activityService, activityID, nil, "Patching image")
	removeMirror := logging.AddLogrusMirror(patchOut)
	patchErr := patch.Patch(ctx, copaOpts)
	removeMirror()
	activitylib.FlushWriter(patchOut)
	durationMs := time.Since(startTime).Milliseconds()

	if patchErr != nil {
		if errors.Is(patchErr, types.ErrNoUpdatesFound) {
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
	var packagesUpdated *int
	if vexOutputPath != "" {
		if data, err := acfs.ReadFile(ctx, filepath.Dir(vexOutputPath), "/"+filepath.Base(vexOutputPath)); err == nil {
			var doc struct {
				Statements []jsontext.Value `json:"statements"`
			}
			if unmarshalErr := json.Unmarshal(data, &doc); unmarshalErr == nil {
				count := len(doc.Statements)
				packagesUpdated = &count
			}
		}
	}

	s.verifyPatchedImage(ctx, record, activityID)

	s.finishPatchRecord(ctx, record, imagepatch.PatchStatusCompleted, "", packagesUpdated, durationMs)
	slog.InfoContext(ctx, "image patch completed", "environmentId", record.EnvironmentID, "imageRef", record.OriginalRef, "patchedRef", record.PatchedRef, "durationMs", durationMs)
	s.completePatchActivity(ctx, activityID, true, "")
	return nil
}

// verifyPatchedImage warns when the expected tag is missing (copa may arch-suffix
// multi-platform tags) and otherwise re-scans it to show whether the patch worked.
func (s *Service) verifyPatchedImage(ctx context.Context, record *ImagePatchRecord, activityID string) {
	if dockerClient, err := s.dockerService.GetClient(ctx); err == nil {
		if patchedInspect, inspectErr := dockerClient.ImageInspect(ctx, record.PatchedRef); inspectErr != nil {
			slog.WarnContext(ctx, "patched image tag not found after patching", "patchedRef", record.PatchedRef, "error", inspectErr)
			s.appendPatchActivity(ctx, activityID, 95, "Patched image was created but the expected tag "+record.PatchedRef+" was not found; check the image list")
		} else if s.vulnerabilityService != nil && s.settingsService.IsFeatureEnabled(ctx, features.VulnerabilityManagement) {
			s.appendPatchActivity(ctx, activityID, 95, "Re-scanning patched image to verify the patch")
			if _, scanErr := s.vulnerabilityService.ScanImage(context.WithoutCancel(ctx), record.EnvironmentID, patchedInspect.ID, scheduledPatchUser); scanErr != nil {
				slog.WarnContext(ctx, "failed to start verification scan of patched image", "patchedRef", record.PatchedRef, "error", scanErr)
			}
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
	desc, err := remote.Get(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
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
	if configDir == "" || s.registryService == nil {
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

func (s *Service) finishPatchRecord(ctx context.Context, record *ImagePatchRecord, status imagepatch.PatchStatus, errMessage string, packagesUpdated *int, durationMs int64) {
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
	if err := s.db.WithContext(utils.ActivityRuntimeContext(ctx, nil)).
		Model(&ImagePatchRecord{}).
		Where("id = ?", record.ID).
		Updates(updates).Error; err != nil {
		slog.WarnContext(ctx, "failed to update image patch record", "error", err, "patchId", record.ID)
	}
}

// ListPatches returns the paginated patch history for an environment.
func (s *Service) ListPatches(ctx context.Context, envID string, params pagination.QueryParams) ([]imagepatch.PatchRecord, pagination.Response, error) {
	var records []ImagePatchRecord
	q := s.db.WithContext(ctx).Model(&ImagePatchRecord{}).Where("environment_id = ?", envID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where("original_ref LIKE ? OR patched_ref LIKE ?", searchPattern, searchPattern)
	}
	q = pagination.ApplyFilter(q, "status", params.Filters["status"])

	params.Sort = cmp.Or(params.Sort, "createdAt")

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &records)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate image patches: %w", err)
	}

	dtos := make([]imagepatch.PatchRecord, 0, len(records))
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
		Where("environment_id = ? AND status = ?", envID, string(imagepatch.PatchStatusCompleted)).
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

// ListPatchTargets returns scanned images with their fixable-vulnerability
// counts and latest patch run, for the security page's patching view. Images
// that are themselves patch outputs are folded into their original's row: they
// are excluded from the list and surface as that row's LastPatchScan instead.
func (s *Service) ListPatchTargets(ctx context.Context, envID string, params pagination.QueryParams) ([]imagepatch.PatchTarget, pagination.Response, error) {
	if err := s.settingsService.RequireFeature(ctx, features.VulnerabilityManagement); err != nil {
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

	params.Sort = cmp.Or(params.Sort, "scanTime")
	var scans []vulnerability.VulnerabilityScanRecord
	paginationResp, err := pagination.PaginateAndSortDB(params, q, &scans)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list patch targets: %w", err)
	}

	if len(scans) == 0 {
		return []imagepatch.PatchTarget{}, paginationResp, nil
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

	targets := make([]imagepatch.PatchTarget, 0, len(scans))
	for i := range scans {
		scan := &scans[i]
		target := imagepatch.PatchTarget{
			ImageID:      scan.ID,
			ImageRef:     scan.ImageName,
			FixableCount: mo.PointerToOption(scan.FixableCount).OrEmpty(),
			TotalCount:   scan.TotalCount,
			ScanTime:     scan.ScanTime,
		}
		if digests, ok := repoDigestsByID[scan.ID]; ok {
			target.LocalOnly = true
			for _, digest := range digests {
				if digest != "<none>@<none>" {
					target.LocalOnly = false
					break
				}
			}
		}

		if lastPatch, ok := lastPatchByImageID[scan.ID]; ok {
			dto := lastPatch.ToDto()
			target.LastPatch = &dto
			target.LastPatchScan = lastPatchScanByImageID[scan.ID]
		}

		targets = append(targets, target)
	}

	return targets, paginationResp, nil
}

// latestPatchesByImage loads the most recent patch run per original
// image in two batched queries, plus the scan of each completed patch's output
// image so the original's row can show whether the patch worked.
func (s *Service) latestPatchesByImage(ctx context.Context, envID string, imageIDs []string) (map[string]*ImagePatchRecord, map[string]*imagepatch.PatchScanSummary, error) {
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
		if row.Status != string(imagepatch.PatchStatusCompleted) {
			continue
		}
		names := []string{row.PatchedRef}
		if named, err := reference.ParseNormalizedNamed(row.PatchedRef); err == nil {
			names = append(names, reference.FamiliarString(named))
		}
		patchedNamesByImageID[row.OriginalImageID] = names
		patchedNames = append(patchedNames, names...)
	}

	lastPatchScanByImageID := make(map[string]*imagepatch.PatchScanSummary, len(patchedNamesByImageID))
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
			lastPatchScanByImageID[imageID] = &imagepatch.PatchScanSummary{
				Status:       latest.Status,
				FixableCount: mo.PointerToOption(latest.FixableCount).OrEmpty(),
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
	if err := s.settingsService.RequireFeature(ctx, features.VulnerabilityManagement); err != nil {
		return nil, err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}
	if storeErr := requireContainerdImageStore(ctx, dockerClient); storeErr != nil {
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
				envID, scan.ID, []string{string(imagepatch.PatchStatusCompleted), string(imagepatch.PatchStatusPatching)}, scan.ScanTime).
			Count(&recent).Error; countErr == nil && recent > 0 {
			continue
		}
		targets = append(targets, Target{ImageID: scan.ID, ImageName: scan.ImageName})
	}
	return targets, nil
}

// Copa needs BuildKit's docker exporter, which dockerd only offers with the containerd image store.
func requireContainerdImageStore(ctx context.Context, dockerClient *client.Client) error {
	info, err := dockerClient.Info(ctx, client.InfoOptions{})
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
	if s.activityService == nil || activityID == "" {
		return
	}
	if _, err := s.activityService.AppendMessage(ctx, activityID, activity.AppendActivityMessageRequest{
		Level:    activitytypes.MessageLevelInfo,
		Message:  message,
		Progress: &progress,
	}); err != nil {
		slog.DebugContext(ctx, "failed to append image patch activity message", "activityId", activityID, "error", err)
	}
}

func (s *Service) completePatchActivity(ctx context.Context, activityID string, success bool, errMessage string) {
	if s.activityService == nil || activityID == "" {
		return
	}

	status := activitytypes.StatusSuccess
	message := "Image patch completed"
	var errorPtr *string
	switch {
	case success:
	case activitylib.CancelledByContext(ctx):
		status, message = activitytypes.StatusCancelled, "Image patch cancelled"
	default:
		status, message = activitytypes.StatusFailed, cmp.Or(errMessage, "Image patch failed")
		errorPtr = mo.EmptyableToOption(errMessage).ToPointer()
	}

	if _, err := s.activityService.CompleteActivity(utils.ActivityRuntimeContext(ctx, nil), activityID, status, message, errorPtr); err != nil {
		slog.DebugContext(ctx, "failed to complete image patch activity", "activityId", activityID, "error", err)
	}
}
