package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/containerd/platforms"
	utilsregistry "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/registryauth"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/klauspost/compress/zstd"
	attestationTypes "github.com/moby/buildkit/util/attestation"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	dockerAttestationManifestArtifactTypeInternal = "application/vnd.docker.attestation.manifest.v1+json"
	inTotoLayerMediaTypeInternal                  = "application/vnd.in-toto+json"
	inTotoPredicateTypeAnnotationInternal         = "in-toto.io/predicate-type"
)

type ImageAttestationQuery struct {
	Platform         string
	PredicateType    string
	IncludeStatement bool
}

type imageAttestationReferenceInternal struct {
	ImageRef      string
	SubjectDigest string
	Empty         bool
}

type imageAttestationSubjectInternal struct {
	Descriptor v1.Descriptor
	Digest     string
	Platform   string
}

type attestationStatementEnvelopeInternal struct {
	Type          string                          `json:"_type"`
	PredicateType string                          `json:"predicateType"`
	Subject       []imagetypes.AttestationSubject `json:"subject"`
}

// GetImageAttestations returns in-toto attestations attached to a local image or registry reference.
func (s *ImageService) GetImageAttestations(ctx context.Context, imageName string, query ImageAttestationQuery) (*imagetypes.AttestationList, error) {
	resolution, err := s.resolveImageAttestationReferenceInternal(ctx, imageName)
	if err != nil {
		return nil, err
	}

	out := &imagetypes.AttestationList{
		ImageRef:      resolution.ImageRef,
		SubjectDigest: resolution.SubjectDigest,
		Platform:      strings.TrimSpace(query.Platform),
		Attestations:  []imagetypes.Attestation{},
	}
	if resolution.Empty {
		return out, nil
	}

	platform, hasPlatform, err := parseAttestationPlatformInternal(query.Platform)
	if err != nil {
		return nil, err
	}

	ref, err := name.ParseReference(resolution.ImageRef, name.WeakValidation)
	if err != nil {
		return nil, fmt.Errorf("parse image reference %q: %w", resolution.ImageRef, err)
	}

	remoteOptions := s.remoteOptionsForImageRefInternal(ctx, resolution.ImageRef)
	remoteDescriptor, err := remote.Get(ref, remoteOptions...)
	if err != nil {
		return nil, fmt.Errorf("get image manifest %q: %w", resolution.ImageRef, err)
	}

	if out.SubjectDigest == "" {
		out.SubjectDigest = remoteDescriptor.Digest.String()
	}

	index, subjects, err := imageAttestationSubjectsInternal(remoteDescriptor, platform, hasPlatform)
	if err != nil {
		return nil, err
	}
	if hasPlatform && len(subjects) > 0 {
		out.SubjectDigest = subjects[0].Digest
		out.Platform = subjects[0].Platform
	}

	attestations := make([]imagetypes.Attestation, 0)
	for _, subject := range subjects {
		referrerAttestations, err := s.readReferrerAttestationsInternal(ctx, ref.Context(), subject, remoteOptions, query)
		if err != nil {
			return nil, err
		}
		attestations = append(attestations, referrerAttestations...)
	}

	if index != nil {
		inlineAttestations, err := readInlineAttestationsInternal(ctx, index, subjects, query)
		if err != nil {
			return nil, err
		}
		attestations = append(attestations, inlineAttestations...)
	}

	out.Attestations = dedupeAttestationsInternal(attestations)
	return out, nil
}

func (s *ImageService) resolveImageAttestationReferenceInternal(ctx context.Context, imageName string) (imageAttestationReferenceInternal, error) {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" {
		return imageAttestationReferenceInternal{}, errors.New("image name is required")
	}

	if s != nil && s.dockerService != nil {
		dockerClient, err := s.dockerService.GetClient(ctx)
		if err == nil {
			inspect, inspectErr := dockerClient.ImageInspect(ctx, imageName)
			if inspectErr == nil {
				imageRef := firstUsableImageReferenceInternal(inspect.RepoDigests, inspect.RepoTags)
				if imageRef == "" {
					return imageAttestationReferenceInternal{ImageRef: imageName, Empty: true}, nil
				}
				return imageAttestationReferenceInternal{
					ImageRef:      imageRef,
					SubjectDigest: digestFromReferenceInternal(imageRef),
				}, nil
			}
		}
	}

	if isLikelyLocalImageIDInternal(imageName) {
		return imageAttestationReferenceInternal{}, fmt.Errorf("image %q does not have a registry reference", imageName)
	}
	if _, err := name.ParseReference(imageName, name.WeakValidation); err != nil {
		return imageAttestationReferenceInternal{}, fmt.Errorf("image %q does not have a registry reference: %w", imageName, err)
	}
	return imageAttestationReferenceInternal{
		ImageRef:      imageName,
		SubjectDigest: digestFromReferenceInternal(imageName),
	}, nil
}

func firstUsableImageReferenceInternal(repoDigests, repoTags []string) string {
	for _, repoDigest := range repoDigests {
		if isUsableImageReferenceInternal(repoDigest) && strings.Contains(repoDigest, "@sha256:") {
			return strings.TrimSpace(repoDigest)
		}
	}
	for _, repoTag := range repoTags {
		if isUsableImageReferenceInternal(repoTag) {
			return strings.TrimSpace(repoTag)
		}
	}
	return ""
}

func isUsableImageReferenceInternal(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.Contains(value, "<none>")
}

func isLikelyLocalImageIDInternal(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "sha256:") {
		return true
	}
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

func digestFromReferenceInternal(imageRef string) string {
	if _, digest, found := strings.Cut(strings.TrimSpace(imageRef), "@"); found {
		return digest
	}
	return ""
}

func parseAttestationPlatformInternal(value string) (ocispec.Platform, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return ocispec.Platform{}, false, nil
	}

	platform, err := platforms.Parse(value)
	if err != nil {
		return ocispec.Platform{}, false, fmt.Errorf("invalid platform %q: %w", value, err)
	}
	return platform, true, nil
}

func (s *ImageService) remoteOptionsForImageRefInternal(ctx context.Context, imageRef string) []remote.Option {
	options := make([]remote.Option, 0, 2)
	options = append(options, remote.WithContext(ctx))
	if s == nil || s.registryService == nil {
		return options
	}

	registryHost, err := utilsregistry.GetRegistryAddress(imageRef)
	if err != nil {
		slog.DebugContext(ctx, "skipping registry auth for unparsable image ref", "image", imageRef, "error", err)
		return options
	}

	encodedAuth, err := s.registryService.GetRegistryAuthForHost(ctx, registryHost)
	if err != nil {
		slog.DebugContext(ctx, "registry auth lookup failed for attestation request", "image", imageRef, "registry", registryHost, "error", err)
		return options
	}
	if strings.TrimSpace(encodedAuth) == "" {
		return options
	}

	dockerAuth, err := utilsregistry.DecodeAuthHeader(encodedAuth)
	if err != nil {
		slog.DebugContext(ctx, "registry auth decode failed for attestation request", "image", imageRef, "registry", registryHost, "error", err)
		return options
	}

	options = append(options, remote.WithAuth(authn.FromConfig(authn.AuthConfig{
		Username:      dockerAuth.Username,
		Password:      dockerAuth.Password,
		Auth:          dockerAuth.Auth,
		IdentityToken: dockerAuth.IdentityToken,
		RegistryToken: dockerAuth.RegistryToken,
	})))
	return options
}

func imageAttestationSubjectsInternal(descriptor *remote.Descriptor, platform ocispec.Platform, hasPlatform bool) (v1.ImageIndex, []imageAttestationSubjectInternal, error) {
	root := imageAttestationSubjectInternal{
		Descriptor: descriptor.Descriptor,
		Digest:     descriptor.Digest.String(),
	}
	if !descriptor.MediaType.IsIndex() {
		if hasPlatform {
			root.Platform = platforms.Format(platform)
		}
		return nil, []imageAttestationSubjectInternal{root}, nil
	}

	index, err := descriptor.ImageIndex()
	if err != nil {
		return nil, nil, fmt.Errorf("read image index %s: %w", descriptor.Digest.String(), err)
	}

	indexManifest, err := index.IndexManifest()
	if err != nil {
		return nil, nil, fmt.Errorf("read image index manifest %s: %w", descriptor.Digest.String(), err)
	}

	subjects := make([]imageAttestationSubjectInternal, 0, len(indexManifest.Manifests)+1)
	if !hasPlatform {
		subjects = append(subjects, root)
	}
	for _, child := range indexManifest.Manifests {
		if isInlineAttestationDescriptorInternal(child) {
			continue
		}
		if hasPlatform && !platformDescriptorMatchesInternal(child.Platform, platform) {
			continue
		}
		childPlatform := platformStringInternal(child.Platform)
		if hasPlatform && childPlatform == "" {
			childPlatform = platforms.Format(platform)
		}
		subjects = append(subjects, imageAttestationSubjectInternal{
			Descriptor: child,
			Digest:     child.Digest.String(),
			Platform:   childPlatform,
		})
	}

	return index, dedupeSubjectsInternal(subjects), nil
}

func platformDescriptorMatchesInternal(platform *v1.Platform, wanted ocispec.Platform) bool {
	if platform == nil {
		return false
	}
	candidate := ocispec.Platform{
		Architecture: platform.Architecture,
		OS:           platform.OS,
		OSVersion:    platform.OSVersion,
		OSFeatures:   platform.OSFeatures,
		Variant:      platform.Variant,
	}
	return platforms.Only(wanted).Match(candidate)
}

func platformStringInternal(platform *v1.Platform) string {
	if platform == nil || platform.OS == "" || platform.Architecture == "" {
		return ""
	}
	return platforms.Format(ocispec.Platform{
		Architecture: platform.Architecture,
		OS:           platform.OS,
		OSVersion:    platform.OSVersion,
		OSFeatures:   platform.OSFeatures,
		Variant:      platform.Variant,
	})
}

func dedupeSubjectsInternal(subjects []imageAttestationSubjectInternal) []imageAttestationSubjectInternal {
	seen := make(map[string]struct{}, len(subjects))
	out := make([]imageAttestationSubjectInternal, 0, len(subjects))
	for _, subject := range subjects {
		if subject.Digest == "" {
			continue
		}
		key := subject.Digest + "|" + subject.Platform
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, subject)
	}
	return out
}

func (s *ImageService) readReferrerAttestationsInternal(ctx context.Context, repository name.Repository, subject imageAttestationSubjectInternal, remoteOptions []remote.Option, query ImageAttestationQuery) ([]imagetypes.Attestation, error) {
	referrers, err := remote.Referrers(repository.Digest(subject.Digest), remoteOptions...)
	if err != nil {
		if shouldIgnoreReferrersErrorInternal(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("query image referrers for %s: %w", subject.Digest, err)
	}

	manifest, err := referrers.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("read image referrers for %s: %w", subject.Digest, err)
	}

	attestations := make([]imagetypes.Attestation, 0, len(manifest.Manifests))
	for _, referrer := range manifest.Manifests {
		referrerDescriptor, err := remote.Get(repository.Digest(referrer.Digest.String()), remoteOptions...)
		if err != nil {
			return nil, fmt.Errorf("get image referrer %s: %w", referrer.Digest.String(), err)
		}

		referrerImage, err := referrerDescriptor.Image()
		if err != nil {
			continue
		}

		if referrer.ArtifactType == "" {
			referrer.ArtifactType = referrerDescriptor.ArtifactType
		}
		if referrer.MediaType == "" {
			referrer.MediaType = referrerDescriptor.MediaType
		}

		items, err := readAttestationImageInternal(ctx, referrerImage, referrer, subject.Platform, query)
		if err != nil {
			return nil, err
		}
		attestations = append(attestations, items...)
	}
	return attestations, nil
}

func shouldIgnoreReferrersErrorInternal(err error) bool {
	var transportErr *transport.Error
	if errors.As(err, &transportErr) {
		return transportErr.StatusCode == http.StatusNotFound || transportErr.StatusCode == http.StatusMethodNotAllowed
	}
	return false
}

func readInlineAttestationsInternal(ctx context.Context, index v1.ImageIndex, subjects []imageAttestationSubjectInternal, query ImageAttestationQuery) ([]imagetypes.Attestation, error) {
	if len(subjects) == 0 {
		return nil, nil
	}

	indexManifest, err := index.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("read image index manifest for inline attestations: %w", err)
	}

	subjectByDigest := make(map[string]imageAttestationSubjectInternal, len(subjects))
	for _, subject := range subjects {
		subjectByDigest[subject.Digest] = subject
	}

	attestations := make([]imagetypes.Attestation, 0)
	for _, descriptor := range indexManifest.Manifests {
		if !isInlineAttestationDescriptorInternal(descriptor) {
			continue
		}

		referenceDigest := strings.TrimSpace(descriptor.Annotations[attestationTypes.DockerAnnotationReferenceDigest])
		subject, ok := subjectByDigest[referenceDigest]
		if referenceDigest != "" && !ok {
			continue
		}
		if referenceDigest == "" {
			subject = firstSubjectInternal(subjects)
		}

		attestationImage, err := index.Image(descriptor.Digest)
		if err != nil {
			return nil, fmt.Errorf("read inline attestation image %s: %w", descriptor.Digest.String(), err)
		}

		items, err := readAttestationImageInternal(ctx, attestationImage, descriptor, subject.Platform, query)
		if err != nil {
			return nil, err
		}
		attestations = append(attestations, items...)
	}
	return attestations, nil
}

func firstSubjectInternal(subjects []imageAttestationSubjectInternal) imageAttestationSubjectInternal {
	if len(subjects) == 0 {
		return imageAttestationSubjectInternal{}
	}
	return subjects[0]
}

func isInlineAttestationDescriptorInternal(descriptor v1.Descriptor) bool {
	if descriptor.Annotations[attestationTypes.DockerAnnotationReferenceType] == attestationTypes.DockerAnnotationReferenceTypeDefault {
		return true
	}
	return descriptor.ArtifactType == dockerAttestationManifestArtifactTypeInternal
}

func readAttestationImageInternal(ctx context.Context, attestationImage v1.Image, artifactDescriptor v1.Descriptor, platform string, query ImageAttestationQuery) ([]imagetypes.Attestation, error) {
	manifest, err := attestationImage.Manifest()
	if err != nil {
		return nil, fmt.Errorf("read attestation manifest %s: %w", artifactDescriptor.Digest.String(), err)
	}

	artifactType := artifactDescriptor.ArtifactType
	if artifactType == "" {
		artifactType = manifest.ArtifactType
	}

	attestations := make([]imagetypes.Attestation, 0, len(manifest.Layers))
	for _, layerDescriptor := range manifest.Layers {
		attestation, ok, err := readAttestationLayerInternal(ctx, attestationImage, layerDescriptor, artifactType, platform, query)
		if err != nil {
			return nil, err
		}
		if ok {
			attestations = append(attestations, attestation)
		}
	}
	return attestations, nil
}

func readAttestationLayerInternal(ctx context.Context, attestationImage v1.Image, layerDescriptor v1.Descriptor, artifactType, platform string, query ImageAttestationQuery) (imagetypes.Attestation, bool, error) {
	if err := ctx.Err(); err != nil {
		return imagetypes.Attestation{}, false, err
	}
	if layerDescriptor.MediaType != types.MediaType(inTotoLayerMediaTypeInternal) {
		return imagetypes.Attestation{}, false, nil
	}

	annotationPredicate := strings.TrimSpace(layerDescriptor.Annotations[inTotoPredicateTypeAnnotationInternal])
	if query.PredicateType != "" && annotationPredicate != "" && annotationPredicate != query.PredicateType {
		return imagetypes.Attestation{}, false, nil
	}

	layer, err := attestationImage.LayerByDigest(layerDescriptor.Digest)
	if err != nil {
		return imagetypes.Attestation{}, false, fmt.Errorf("read attestation layer %s: %w", layerDescriptor.Digest.String(), err)
	}

	rawStatement, err := readAttestationLayerBytesInternal(layer, layerDescriptor.Digest.String())
	if err != nil {
		return imagetypes.Attestation{}, false, err
	}

	statement, err := parseAttestationStatementInternal(rawStatement)
	if err != nil {
		return imagetypes.Attestation{}, false, fmt.Errorf("parse attestation statement %s: %w", layerDescriptor.Digest.String(), err)
	}
	if statement.PredicateType == "" {
		statement.PredicateType = annotationPredicate
	}
	if query.PredicateType != "" && statement.PredicateType != query.PredicateType {
		return imagetypes.Attestation{}, false, nil
	}

	size := layerDescriptor.Size
	if size == 0 {
		size = int64(len(rawStatement))
	}

	attestation := imagetypes.Attestation{
		Digest:        layerDescriptor.Digest.String(),
		MediaType:     string(layerDescriptor.MediaType),
		ArtifactType:  artifactType,
		PredicateType: statement.PredicateType,
		StatementType: statement.Type,
		Subject:       statement.Subject,
		Platform:      platform,
		Size:          size,
	}
	if attestation.Subject == nil {
		attestation.Subject = []imagetypes.AttestationSubject{}
	}
	if query.IncludeStatement {
		attestation.Statement = append([]byte(nil), rawStatement...)
	}
	return attestation, true, nil
}

func readAttestationLayerBytesInternal(layer v1.Layer, digest string) ([]byte, error) {
	reader, err := layer.Compressed()
	if err != nil {
		return nil, fmt.Errorf("open attestation layer %s: %w", digest, err)
	}
	rawStatement, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read attestation layer %s: %w", digest, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close attestation layer %s: %w", digest, closeErr)
	}

	statement, err := decompressAttestationStatementInternal(rawStatement)
	if err != nil {
		return nil, fmt.Errorf("decompress attestation layer %s: %w", digest, err)
	}
	return statement, nil
}

// decompressAttestationStatementInternal returns the raw in-toto statement
// bytes, transparently decompressing OCI blobs that are stored compressed. The
// in-toto media type does not encode the compression algorithm, so the
// container is detected from the magic header. gzip and zstd are the algorithms
// supported by the OCI image spec; anything else (including already
// uncompressed JSON) is returned unchanged.
func decompressAttestationStatementInternal(data []byte) ([]byte, error) {
	switch {
	case len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b:
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer func() { _ = reader.Close() }()
		return io.ReadAll(reader)
	case len(data) >= 4 && data[0] == 0x28 && data[1] == 0xb5 && data[2] == 0x2f && data[3] == 0xfd:
		reader, err := zstd.NewReader(nil)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return reader.DecodeAll(data, nil)
	default:
		return data, nil
	}
}

func parseAttestationStatementInternal(rawStatement []byte) (attestationStatementEnvelopeInternal, error) {
	var statement attestationStatementEnvelopeInternal
	if err := json.Unmarshal(rawStatement, &statement); err != nil {
		return attestationStatementEnvelopeInternal{}, err
	}
	return statement, nil
}

func dedupeAttestationsInternal(attestations []imagetypes.Attestation) []imagetypes.Attestation {
	seen := make(map[string]struct{}, len(attestations))
	out := make([]imagetypes.Attestation, 0, len(attestations))
	for _, attestation := range attestations {
		key := strings.Join([]string{
			attestation.Digest,
			attestation.PredicateType,
			attestation.Platform,
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, attestation)
	}
	return out
}
