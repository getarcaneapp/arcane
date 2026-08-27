package imagepatch

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/moby/buildkit/util/progress/progressui"
	ispec "github.com/opencontainers/image-spec/specs-go/v1"
	copacommon "github.com/project-copacetic/copacetic/pkg/common"
	copapatch "github.com/project-copacetic/copacetic/pkg/patch"
	copatypes "github.com/project-copacetic/copacetic/pkg/types"
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
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/imagepatch"
)

// ImagePatchService patches image OS packages in place using the Copacetic
// library and records each run in the image_patches table.
type ImagePatchService struct {
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

func NewImagePatchService(db *database.DB, dockerService *docker.DockerClientService, settingsService *settings.SettingsService, activityService *activity.ActivityService, registryService *registry.ContainerRegistryService, vulnerabilityService *vulnerability.VulnerabilityService) *ImagePatchService {
	// Copa logs through the global logrus logger; route it into slog so its
	// output follows Arcane's log format.
	logging.InstallLogrusBridge()

	// Copa resolves the daemon for BuildKit dialing, manifest discovery, and
	// image loading from DOCKER_HOST; without it, its docker connhelper falls
	// back to shelling out to the docker CLI, which Arcane deployments lack.
	if os.Getenv("DOCKER_HOST") == "" && dockerService != nil {
		if host := strings.TrimSpace(dockerService.DockerHost()); host != "" {
			if err := os.Setenv("DOCKER_HOST", host); err != nil {
				slog.Warn("failed to set DOCKER_HOST for image patching", "error", err)
			}
		}
	}

	return &ImagePatchService{
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
func (s *ImagePatchService) PatchImage(ctx context.Context, envID, imageID string, opts imagepatch.PatchOptions, user common.User) (*imagepatch.PatchRecord, error) {
	runCtx := utils.ActivityRuntimeContext(ctx, nil)

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	imageInspect, err := dockerClient.ImageInspect(runCtx, imageID)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to inspect image")
	}
	if len(imageInspect.RepoTags) == 0 {
		return nil, common.ErrImageUntagged
	}
	imageRef := imageInspect.RepoTags[0]

	// Never pulled or pushed: BuildKit cannot fetch the image to patch it.
	if len(imageInspect.RepoDigests) == 0 {
		return nil, common.ErrImageLocalOnly
	}
	originalDigest := imageInspect.RepoDigests[0]

	// Resolve the stored scan report when patching from a scan.
	mode := imagepatch.PatchModeUpdateAll
	var reportData []byte
	if strings.TrimSpace(opts.ScanID) != "" {
		// Scan records are keyed by image ID; refuse a scan belonging to a
		// different image than the one selected by the route.
		if opts.ScanID != imageInspect.ID {
			return nil, common.ErrPatchScanImageMismatch
		}
		var report vulnerability.VulnerabilityReportRecord
		if err := s.db.WithContext(runCtx).First(&report, "image_id = ?", opts.ScanID).Error; err != nil || len(report.Data) == 0 {
			return nil, common.ErrPatchScanReportUnavailable
		}
		reportData = []byte(report.Data)
		mode = imagepatch.PatchModeReport
	}

	suffix := strings.TrimSpace(opts.Suffix)
	if suffix == "" {
		suffix = strings.TrimSpace(s.settingsService.GetSettingsConfig().ImagePatchSuffix.Value)
	}
	if suffix == "" {
		suffix = "patched"
	}
	patchedRef, err := resolvePatchedRef(imageRef, opts.PatchedTag, suffix)
	if err != nil {
		return nil, err
	}

	// Copa reads registry credentials from the docker config; refresh it before
	// any registry lookups below.
	s.writeRegistryAuthConfigInternal(runCtx)

	// Unless the all-platforms setting is enabled, pin the patch target to the
	// platform-specific manifest digest matching this image so copa patches a
	// single platform and keeps the plain patched tag. Report-mode patches are
	// always single-platform in copa, so no pinning is needed there.
	copaImageRef := imageRef
	if mode == imagepatch.PatchModeUpdateAll && !s.settingsService.GetSettingsConfig().ImagePatchAllPlatforms.IsTrue() {
		if pinned := platformPinnedRefInternal(runCtx, imageRef, ispec.Platform{
			OS:           imageInspect.Os,
			Architecture: imageInspect.Architecture,
			Variant:      imageInspect.Variant,
		}); pinned != "" {
			copaImageRef = pinned
		}
	}

	activityID := s.startPatchActivityInternal(runCtx, envID, imageID, imageRef, &user)
	runCtx = s.activityService.Track(runCtx, activityID)

	record := &ImagePatchRecord{
		EnvironmentID:   envID,
		OriginalImageID: imageInspect.ID,
		OriginalRef:     imageRef,
		OriginalDigest:  originalDigest,
		PatchedRef:      patchedRef,
		Mode:            string(mode),
		Status:          string(imagepatch.PatchStatusPatching),
		ActivityID:      mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
	}
	if err := s.db.WithContext(runCtx).Create(record).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to create image patch record")
	}

	slog.InfoContext(runCtx, "image patch queued",
		"environmentId", envID,
		"imageRef", imageRef,
		"patchedRef", patchedRef,
		"mode", mode,
		"patchTarget", copaImageRef,
	)

	go s.patchInBackgroundInternal(runCtx, record, opts, reportData, copaImageRef, activityID)

	dto := record.ToDto()
	return &dto, nil
}

func (s *ImagePatchService) patchInBackgroundInternal(ctx context.Context, record *ImagePatchRecord, opts imagepatch.PatchOptions, reportData []byte, copaImageRef, activityID string) {
	select {
	case s.patchSlot <- struct{}{}:
		defer func() { <-s.patchSlot }()
	case <-ctx.Done():
		s.finishPatchRecordInternal(ctx, record, imagepatch.PatchStatusFailed, ctx.Err().Error(), nil, 0)
		s.completePatchActivityInternal(ctx, activityID, false, ctx.Err().Error())
		return
	}

	// Copa consumes the scan report as a file; materialize the stored report
	// into a temp file for the duration of the run.
	reportPath := ""
	if len(reportData) > 0 {
		reportDir, err := os.MkdirTemp("", "arcane-patch-report")
		if err == nil {
			reportPath = filepath.Join(reportDir, "report.json")
			err = os.WriteFile(reportPath, reportData, 0o600)
			defer os.RemoveAll(reportDir)
		}
		if err != nil {
			s.finishPatchRecordInternal(ctx, record, imagepatch.PatchStatusFailed, err.Error(), nil, 0)
			s.completePatchActivityInternal(ctx, activityID, false, err.Error())
			return
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

	copaOpts := &copatypes.Options{
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
			defer os.RemoveAll(vexDir)
		}
	}

	// Mirror copa's log output into the activity while the patch runs; BuildKit
	// step progress stays quiet until copa exposes a progress writer upstream.
	s.appendPatchActivityInternal(ctx, activityID, 30, "Patching image packages via BuildKit")
	patchOut := activitylib.NewWriter(ctx, s.activityService, activityID, nil, "Patching image")
	removeMirror := logging.AddLogrusMirror(patchOut)
	patchErr := copapatch.Patch(ctx, copaOpts)
	removeMirror()
	activitylib.FlushWriter(patchOut)
	durationMs := time.Since(startTime).Milliseconds()

	if patchErr != nil {
		if errors.Is(patchErr, copatypes.ErrNoUpdatesFound) {
			patchErr = errors.New("no OS package updates available for this image")
		}
		s.finishPatchRecordInternal(ctx, record, imagepatch.PatchStatusFailed, patchErr.Error(), nil, durationMs)
		slog.WarnContext(ctx, "image patch failed",
			"environmentId", record.EnvironmentID,
			"imageRef", record.OriginalRef,
			"durationMs", durationMs,
			"error", patchErr,
		)
		s.completePatchActivityInternal(ctx, activityID, false, patchErr.Error())
		return
	}

	// Count VEX statements as the number of patched packages, when available.
	var packagesUpdated *int
	if vexOutputPath != "" {
		if data, err := acfs.ReadFile(ctx, filepath.Dir(vexOutputPath), "/"+filepath.Base(vexOutputPath)); err == nil {
			var doc struct {
				Statements []json.RawMessage `json:"statements"`
			}
			if err := json.Unmarshal(data, &doc); err == nil {
				count := len(doc.Statements)
				packagesUpdated = &count
			}
		}
	}

	// Warn when the expected patched tag is missing from the daemon (e.g. copa
	// took the multi-platform path and arch-suffixed the tag). When it exists,
	// re-scan it so the security page can show whether the patch worked.
	if dockerClient, err := s.dockerService.GetClient(ctx); err == nil {
		if patchedInspect, err := dockerClient.ImageInspect(ctx, record.PatchedRef); err != nil {
			slog.WarnContext(ctx, "patched image tag not found after patching", "patchedRef", record.PatchedRef, "error", err)
			s.appendPatchActivityInternal(ctx, activityID, 95, "Patched image was created but the expected tag "+record.PatchedRef+" was not found; check the image list")
		} else if s.vulnerabilityService != nil {
			s.appendPatchActivityInternal(ctx, activityID, 95, "Re-scanning patched image to verify the patch")
			if _, err := s.vulnerabilityService.ScanImage(context.WithoutCancel(ctx), record.EnvironmentID, patchedInspect.ID, common.User{Username: "System"}); err != nil {
				slog.WarnContext(ctx, "failed to start verification scan of patched image", "patchedRef", record.PatchedRef, "error", err)
			}
		}
	}

	s.finishPatchRecordInternal(ctx, record, imagepatch.PatchStatusCompleted, "", packagesUpdated, durationMs)
	slog.InfoContext(ctx, "image patch completed",
		"environmentId", record.EnvironmentID,
		"imageRef", record.OriginalRef,
		"patchedRef", record.PatchedRef,
		"durationMs", durationMs,
	)
	s.completePatchActivityInternal(ctx, activityID, true, "")
}

// platformPinnedRefInternal resolves a tag reference to the manifest digest of
// the given platform when the registry serves a multi-platform index. This
// makes copa patch a single platform (keeping the plain patched tag) instead
// of every platform in the index. Returns "" when the reference is not a
// multi-platform index or cannot be resolved, in which case the plain tag is
// used and copa's own discovery decides.
func platformPinnedRefInternal(ctx context.Context, imageRef string, target ispec.Platform) string {
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
		got := platforms.Normalize(ispec.Platform{
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
		return "", errors.WrapIf(err, "failed to parse image reference")
	}
	name, tag, err := copacommon.ResolvePatchedImageName(named, patchedTag, suffix)
	if err != nil {
		return "", errors.WrapIf(err, "failed to resolve patched image name")
	}
	return name + ":" + tag, nil
}

// writeRegistryAuthConfigInternal merges Arcane's registry credentials into the
// docker config file copa reads for BuildKit registry auth. It only touches the
// config when DOCKER_CONFIG is set, i.e. the deployment (normally Arcane's own
// startup) owns that directory — a user's ~/.docker/config.json is never modified.
func (s *ImagePatchService) writeRegistryAuthConfigInternal(ctx context.Context) {
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
		Auths map[string]json.RawMessage `json:"auths"`
	}
	if err := json.Unmarshal(arcaneConfig, &arcane); err != nil {
		return
	}

	if err := acfs.MkdirAll(ctx, filepath.Dir(configDir), "/"+filepath.Base(configDir), 0o700); err != nil {
		slog.WarnContext(ctx, "failed to create docker config directory for image patching", "path", configDir, "error", err)
		return
	}

	merged := map[string]json.RawMessage{}
	if existing, err := acfs.ReadFile(ctx, configDir, "/config.json"); err == nil {
		if err := json.Unmarshal(existing, &merged); err != nil {
			slog.WarnContext(ctx, "existing docker config is not valid JSON; leaving it untouched", "path", configDir, "error", err)
			return
		}
	}

	auths := map[string]json.RawMessage{}
	if raw, ok := merged["auths"]; ok {
		if err := json.Unmarshal(raw, &auths); err != nil {
			auths = map[string]json.RawMessage{}
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
	if err := acfs.WriteFile(ctx, configDir, "/config.json", payload, 0o600); err != nil {
		slog.WarnContext(ctx, "failed to write docker config for image patching", "path", configDir, "error", err)
	}
}

func (s *ImagePatchService) finishPatchRecordInternal(ctx context.Context, record *ImagePatchRecord, status imagepatch.PatchStatus, errMessage string, packagesUpdated *int, durationMs int64) {
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
func (s *ImagePatchService) ListPatches(ctx context.Context, envID string, params pagination.QueryParams) ([]imagepatch.PatchRecord, pagination.Response, error) {
	var records []ImagePatchRecord
	q := s.db.WithContext(ctx).Model(&ImagePatchRecord{}).Where("environment_id = ?", envID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where("original_ref LIKE ? OR patched_ref LIKE ?", searchPattern, searchPattern)
	}
	q = pagination.ApplyFilter(q, "status", params.Filters["status"])

	if params.Sort == "" {
		params.Sort = "createdAt"
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &records)
	if err != nil {
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to paginate image patches")
	}

	dtos := make([]imagepatch.PatchRecord, 0, len(records))
	for i := range records {
		dtos = append(dtos, records[i].ToDto())
	}
	return dtos, paginationResp, nil
}

// PatchedRefs returns the set of patched image references recorded for an
// environment, used to keep patch outputs from being patched again.
func (s *ImagePatchService) PatchedRefs(ctx context.Context, envID string) (map[string]struct{}, error) {
	var refs []string
	if err := s.db.WithContext(ctx).
		Model(&ImagePatchRecord{}).
		Where("environment_id = ? AND status = ?", envID, string(imagepatch.PatchStatusCompleted)).
		Distinct().
		Pluck("patched_ref", &refs).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to list patched image refs")
	}
	set := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		set[ref] = struct{}{}
	}
	return set, nil
}

// ListPatchTargets returns scanned images with their fixable-vulnerability
// counts and latest patch run, for the security page's patching view. Images
// that are themselves patch outputs are folded into their original's row: they
// are excluded from the list and surface as that row's LastPatchScan instead.
func (s *ImagePatchService) ListPatchTargets(ctx context.Context, envID string, params pagination.QueryParams) ([]imagepatch.PatchTarget, pagination.Response, error) {
	patchedRefs, err := s.PatchedRefs(ctx, envID)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	excludedNames := make([]string, 0, len(patchedRefs)*2)
	for ref := range patchedRefs {
		excludedNames = append(excludedNames, ref)
		if named, err := reference.ParseNormalizedNamed(ref); err == nil {
			excludedNames = append(excludedNames, reference.FamiliarString(named))
		}
	}

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

	if params.Sort == "" {
		params.Sort = "scanTime"
	}
	var scans []vulnerability.VulnerabilityScanRecord
	paginationResp, err := pagination.PaginateAndSortDB(params, q, &scans)
	if err != nil {
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to list patch targets")
	}

	// Best-effort: mark images without a registry source so the UI can explain
	// why they cannot be patched.
	dockerClient, dockerErr := s.dockerService.GetClient(ctx)

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
		if dockerErr == nil {
			if inspect, err := dockerClient.ImageInspect(ctx, scan.ID); err == nil {
				target.LocalOnly = len(inspect.RepoDigests) == 0
			}
		}

		var lastPatch ImagePatchRecord
		if err := s.db.WithContext(ctx).
			Where("environment_id = ? AND original_image_id = ?", envID, scan.ID).
			Order("created_at DESC").
			First(&lastPatch).Error; err == nil {
			dto := lastPatch.ToDto()
			target.LastPatch = &dto
			target.LastPatchScan = s.patchedImageScanSummaryInternal(ctx, &lastPatch)
		}

		targets = append(targets, target)
	}

	return targets, paginationResp, nil
}

// patchedImageScanSummaryInternal looks up the scan of a completed patch's
// output image so the original's row can show whether the patch worked.
func (s *ImagePatchService) patchedImageScanSummaryInternal(ctx context.Context, lastPatch *ImagePatchRecord) *imagepatch.PatchScanSummary {
	if lastPatch.Status != string(imagepatch.PatchStatusCompleted) {
		return nil
	}
	names := []string{lastPatch.PatchedRef}
	if named, err := reference.ParseNormalizedNamed(lastPatch.PatchedRef); err == nil {
		names = append(names, reference.FamiliarString(named))
	}

	var scan vulnerability.VulnerabilityScanRecord
	if err := s.db.WithContext(ctx).
		Where("image_name IN ?", names).
		Order("scan_time DESC").
		First(&scan).Error; err != nil {
		return nil
	}
	return &imagepatch.PatchScanSummary{
		Status:       scan.Status,
		FixableCount: mo.PointerToOption(scan.FixableCount).OrEmpty(),
		TotalCount:   scan.TotalCount,
		ScanTime:     scan.ScanTime,
	}
}

// PatchFlaggedImages patches every image whose latest completed vulnerability
// scan found fixable vulnerabilities and stored a raw report. Images that are
// themselves patch outputs are skipped (never patch a patched tag), as are
// images already patched since their latest scan. Used by the scheduled
// auto-patch job.
func (s *ImagePatchService) PatchFlaggedImages(ctx context.Context, envID string, user common.User) (patched, skipped int, err error) {
	var scans []vulnerability.VulnerabilityScanRecord
	if err := s.db.WithContext(ctx).
		Where("status = ? AND fixable_count > 0", vulnerability.ScanStatusCompleted).
		Where("id IN (?)", s.db.Model(&vulnerability.VulnerabilityReportRecord{}).Select("image_id")).
		Where("image_name NOT LIKE 'sha256:%' AND image_name NOT LIKE '%<none>%' AND image_name <> id").
		Find(&scans).Error; err != nil {
		return 0, 0, errors.WrapIf(err, "failed to list vulnerability scans")
	}

	patchedRefs, err := s.PatchedRefs(ctx, envID)
	if err != nil {
		return 0, 0, err
	}

	for i := range scans {
		scan := &scans[i]

		if _, isPatchOutput := patchedRefs[scan.ImageName]; isPatchOutput {
			skipped++
			continue
		}

		// Skip images already patched (or being patched) since their latest scan.
		var recent int64
		if err := s.db.WithContext(ctx).
			Model(&ImagePatchRecord{}).
			Where("environment_id = ? AND original_image_id = ? AND status IN ? AND created_at >= ?",
				envID, scan.ID, []string{string(imagepatch.PatchStatusCompleted), string(imagepatch.PatchStatusPatching)}, scan.ScanTime).
			Count(&recent).Error; err == nil && recent > 0 {
			skipped++
			continue
		}

		if _, err := s.PatchImage(ctx, envID, scan.ID, imagepatch.PatchOptions{ScanID: scan.ID}, user); err != nil {
			slog.WarnContext(ctx, "scheduled image patch failed to start",
				"imageId", scan.ID,
				"imageName", scan.ImageName,
				"error", err,
			)
			skipped++
			continue
		}
		patched++
	}

	return patched, skipped, nil
}

func (s *ImagePatchService) startPatchActivityInternal(ctx context.Context, envID, imageID, imageRef string, user *common.User) string {
	if s.activityService == nil {
		return ""
	}
	started, err := s.activityService.StartActivity(ctx, activity.StartActivityRequest{
		EnvironmentID: envID,
		Type:          activitytypes.TypeImagePatch,
		ResourceType:  new("image"),
		ResourceID:    &imageID,
		ResourceName:  &imageRef,
		StartedBy:     user,
		Progress:      new(0),
		LatestMessage: "Image patch queued",
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to create image patch activity", "error", err, "imageRef", imageRef)
		return ""
	}
	return started.ID
}

func (s *ImagePatchService) appendPatchActivityInternal(ctx context.Context, activityID string, progress int, message string) {
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

func (s *ImagePatchService) completePatchActivityInternal(ctx context.Context, activityID string, success bool, errMessage string) {
	if s.activityService == nil || activityID == "" {
		return
	}

	status := activitytypes.StatusSuccess
	message := "Image patch completed"
	var errorPtr *string
	if !success {
		if activitylib.CancelledByContext(ctx) {
			status = activitytypes.StatusCancelled
			message = "Image patch cancelled"
		} else {
			status = activitytypes.StatusFailed
			message = "Image patch failed"
			if strings.TrimSpace(errMessage) != "" {
				errorPtr = &errMessage
				message = errMessage
			}
		}
	}

	if _, err := s.activityService.CompleteActivity(utils.ActivityRuntimeContext(ctx, nil), activityID, status, message, errorPtr); err != nil {
		slog.DebugContext(ctx, "failed to complete image patch activity", "activityId", activityID, "error", err)
	}
}
