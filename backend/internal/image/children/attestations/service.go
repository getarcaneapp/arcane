package attestations

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/containerd/platforms"
	"github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/klauspost/compress/zstd"
	"github.com/moby/buildkit/util/attestation"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/registryauth"
)

// zstdDecoder is shared because DecodeAll is safe for concurrent use and a decoder is costly to build.
var zstdDecoder = sync.OnceValues(func() (*zstd.Decoder, error) { return zstd.NewReader(nil) })

// Service reads in-toto attestations attached to local images and registry references.
type Service struct {
	dockerClient func(ctx context.Context) (*client.Client, error)
	registryAuth func(ctx context.Context, registryHost string) (string, error)
}

func NewService(
	dockerClient func(ctx context.Context) (*client.Client, error),
	registryAuth func(ctx context.Context, registryHost string) (string, error),
) *Service {
	return &Service{dockerClient: dockerClient, registryAuth: registryAuth}
}

type Query struct {
	Platform         string
	PredicateType    string
	IncludeStatement bool
}

type referenceInternal struct {
	ImageRef      string
	SubjectDigest string
	Empty         bool
}

type subjectInternal struct {
	Descriptor v1.Descriptor
	Digest     string
	Platform   string
}

// GetImageAttestations returns in-toto attestations attached to a local image or registry reference.
func (s *Service) GetImageAttestations(ctx context.Context, imageName string, query Query) (*image.AttestationList, error) {
	resolution, err := s.resolveImageAttestationReferenceInternal(ctx, imageName)
	if err != nil {
		return nil, err
	}

	out := &image.AttestationList{
		ImageRef:      resolution.ImageRef,
		SubjectDigest: resolution.SubjectDigest,
		Platform:      strings.TrimSpace(query.Platform),
		Attestations:  []image.Attestation{},
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

	index, subjects, err := subjectsInternal(remoteDescriptor, platform, hasPlatform)
	if err != nil {
		return nil, err
	}
	if hasPlatform && len(subjects) > 0 {
		out.SubjectDigest = subjects[0].Digest
		out.Platform = subjects[0].Platform
	}

	attestations := make([]image.Attestation, 0)
	for _, subject := range subjects {
		referrerAttestations, readReferrerAttestationsErr := s.readReferrerAttestationsInternal(ctx, ref.Context(), subject, remoteOptions, query)
		if readReferrerAttestationsErr != nil {
			return nil, readReferrerAttestationsErr
		}
		attestations = append(attestations, referrerAttestations...)
	}

	if index != nil {
		inlineAttestations, readInlineAttestationsErr := readInlineAttestationsInternal(ctx, index, subjects, query)
		if readInlineAttestationsErr != nil {
			return nil, readInlineAttestationsErr
		}
		attestations = append(attestations, inlineAttestations...)
	}

	out.Attestations = dedupeAttestationsInternal(attestations)
	return out, nil
}

func (s *Service) resolveImageAttestationReferenceInternal(ctx context.Context, imageName string) (referenceInternal, error) {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" {
		return referenceInternal{}, errors.New("image name is required")
	}

	if s.dockerClient != nil {
		dockerClient, err := s.dockerClient(ctx)
		if err == nil {
			inspect, inspectErr := dockerClient.ImageInspect(ctx, imageName)
			if inspectErr == nil {
				imageRef := firstUsableImageReferenceInternal(inspect.RepoDigests, inspect.RepoTags)
				if imageRef == "" {
					return referenceInternal{ImageRef: imageName, Empty: true}, nil
				}
				return referenceInternal{
					ImageRef:      imageRef,
					SubjectDigest: digestFromReferenceInternal(imageRef),
				}, nil
			}
		}
	}

	if isLikelyLocalImageIDInternal(imageName) {
		return referenceInternal{}, fmt.Errorf("image %q does not have a registry reference", imageName)
	}
	if _, err := name.ParseReference(imageName, name.WeakValidation); err != nil {
		return referenceInternal{}, fmt.Errorf("image %q does not have a registry reference: %w", imageName, err)
	}
	return referenceInternal{
		ImageRef:      imageName,
		SubjectDigest: digestFromReferenceInternal(imageName),
	}, nil
}

func (s *Service) remoteOptionsForImageRefInternal(ctx context.Context, imageRef string) []remote.Option {
	options := make([]remote.Option, 0, 2)
	options = append(options, remote.WithContext(ctx))
	if s.registryAuth == nil {
		return options
	}

	registryHost, err := registryauth.GetRegistryAddress(imageRef)
	if err != nil {
		slog.DebugContext(ctx, "skipping registry auth for unparsable image ref", "image", imageRef, "error", err)
		return options
	}

	encodedAuth, err := s.registryAuth(ctx, registryHost)
	if err != nil {
		slog.DebugContext(ctx, "registry auth lookup failed for attestation request", "image", imageRef, "registry", registryHost, "error", err)
		return options
	}
	if strings.TrimSpace(encodedAuth) == "" {
		return options
	}

	dockerAuth, err := registryauth.DecodeAuthHeader(encodedAuth)
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

func subjectsInternal(descriptor *remote.Descriptor, platform ocispec.Platform, hasPlatform bool) (v1.ImageIndex, []subjectInternal, error) {
	root := subjectInternal{
		Descriptor: descriptor.Descriptor,
		Digest:     descriptor.Digest.String(),
	}
	if !descriptor.MediaType.IsIndex() {
		if hasPlatform {
			root.Platform = platforms.Format(platform)
		}
		return nil, []subjectInternal{root}, nil
	}

	index, err := descriptor.ImageIndex()
	if err != nil {
		return nil, nil, fmt.Errorf("read image index %s: %w", descriptor.Digest.String(), err)
	}

	indexManifest, err := index.IndexManifest()
	if err != nil {
		return nil, nil, fmt.Errorf("read image index manifest %s: %w", descriptor.Digest.String(), err)
	}

	subjects := make([]subjectInternal, 0, len(indexManifest.Manifests)+1)
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
		subjects = append(subjects, subjectInternal{
			Descriptor: child,
			Digest:     child.Digest.String(),
			Platform:   childPlatform,
		})
	}

	return index, dedupeSubjectsInternal(subjects), nil
}

func dedupeSubjectsInternal(subjects []subjectInternal) []subjectInternal {
	seen := make(map[string]struct{}, len(subjects))
	out := make([]subjectInternal, 0, len(subjects))
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

func (
	s *Service,
) readReferrerAttestationsInternal(
	ctx context.Context,
	repository name.Repository,
	subject subjectInternal,
	remoteOptions []remote.Option,
	query Query,
) (
	[]image.Attestation,
	error,
) {
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

	attestations := make([]image.Attestation, 0, len(manifest.Manifests))
	for _, referrer := range manifest.Manifests {
		referrerDescriptor, getErr := remote.Get(repository.Digest(referrer.Digest.String()), remoteOptions...)
		if getErr != nil {
			return nil, fmt.Errorf("get image referrer %s: %w", referrer.Digest.String(), getErr)
		}

		referrerImage, getErr := referrerDescriptor.Image()
		if getErr != nil {
			continue
		}

		if referrer.ArtifactType == "" {
			referrer.ArtifactType = referrerDescriptor.ArtifactType
		}
		if referrer.MediaType == "" {
			referrer.MediaType = referrerDescriptor.MediaType
		}

		items, getErr := readAttestationImageInternal(ctx, referrerImage, referrer, subject.Platform, query)
		if getErr != nil {
			return nil, getErr
		}
		attestations = append(attestations, items...)
	}
	return attestations, nil
}

func readInlineAttestationsInternal(ctx context.Context, index v1.ImageIndex, subjects []subjectInternal, query Query) ([]image.Attestation, error) {
	if len(subjects) == 0 {
		return nil, nil
	}

	indexManifest, err := index.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("read image index manifest for inline attestations: %w", err)
	}

	subjectByDigest := make(map[string]subjectInternal, len(subjects))
	for _, subject := range subjects {
		subjectByDigest[subject.Digest] = subject
	}

	attestations := make([]image.Attestation, 0)
	for _, descriptor := range indexManifest.Manifests {
		if !isInlineAttestationDescriptorInternal(descriptor) {
			continue
		}

		referenceDigest := strings.TrimSpace(descriptor.Annotations[attestation.DockerAnnotationReferenceDigest])
		subject, ok := subjectByDigest[referenceDigest]
		if referenceDigest != "" && !ok {
			continue
		}
		if referenceDigest == "" {
			subject = firstSubjectInternal(subjects)
		}

		attestationImage, imageErr := index.Image(descriptor.Digest)
		if imageErr != nil {
			return nil, fmt.Errorf("read inline attestation image %s: %w", descriptor.Digest.String(), imageErr)
		}

		items, imageErr := readAttestationImageInternal(ctx, attestationImage, descriptor, subject.Platform, query)
		if imageErr != nil {
			return nil, imageErr
		}
		attestations = append(attestations, items...)
	}
	return attestations, nil
}

func firstSubjectInternal(subjects []subjectInternal) subjectInternal {
	if len(subjects) == 0 {
		return subjectInternal{}
	}
	return subjects[0]
}

func readAttestationImageInternal(ctx context.Context, attestationImage v1.Image, artifactDescriptor v1.Descriptor, platform string, query Query) ([]image.Attestation, error) {
	manifest, err := attestationImage.Manifest()
	if err != nil {
		return nil, fmt.Errorf("read attestation manifest %s: %w", artifactDescriptor.Digest.String(), err)
	}

	artifactType := artifactDescriptor.ArtifactType
	if artifactType == "" {
		artifactType = manifest.ArtifactType
	}

	attestations := make([]image.Attestation, 0, len(manifest.Layers))
	for _, layerDescriptor := range manifest.Layers {
		item, ok, readAttestationLayerErr := readAttestationLayerInternal(ctx, attestationImage, layerDescriptor, artifactType, platform, query)
		if readAttestationLayerErr != nil {
			return nil, readAttestationLayerErr
		}
		if ok {
			attestations = append(attestations, item)
		}
	}
	return attestations, nil
}

func readAttestationLayerInternal(
	ctx context.Context,
	attestationImage v1.Image,
	layerDescriptor v1.Descriptor,
	artifactType, platform string,
	query Query,
) (
	image.Attestation,
	bool,
	error,
) {
	if err := ctx.Err(); err != nil {
		return image.Attestation{}, false, err
	}
	if layerDescriptor.MediaType != inTotoLayerMediaType {
		return image.Attestation{}, false, nil
	}

	annotationPredicate := strings.TrimSpace(layerDescriptor.Annotations[inTotoPredicateTypeAnnotation])
	if query.PredicateType != "" && annotationPredicate != "" && annotationPredicate != query.PredicateType {
		return image.Attestation{}, false, nil
	}

	layer, err := attestationImage.LayerByDigest(layerDescriptor.Digest)
	if err != nil {
		return image.Attestation{}, false, fmt.Errorf("read attestation layer %s: %w", layerDescriptor.Digest.String(), err)
	}

	rawStatement, err := readAttestationLayerBytesInternal(layer, layerDescriptor.Digest.String())
	if err != nil {
		return image.Attestation{}, false, err
	}

	statement, err := parseAttestationStatementInternal(rawStatement)
	if err != nil {
		return image.Attestation{}, false, fmt.Errorf("parse attestation statement %s: %w", layerDescriptor.Digest.String(), err)
	}
	statement.PredicateType = cmp.Or(statement.PredicateType, annotationPredicate)
	if query.PredicateType != "" && statement.PredicateType != query.PredicateType {
		return image.Attestation{}, false, nil
	}

	size := cmp.Or(layerDescriptor.Size, int64(len(rawStatement)))

	item := image.Attestation{
		Digest:        layerDescriptor.Digest.String(),
		MediaType:     string(layerDescriptor.MediaType),
		ArtifactType:  artifactType,
		PredicateType: statement.PredicateType,
		StatementType: statement.Type,
		Subject:       statement.Subject,
		Platform:      platform,
		Size:          size,
	}
	if item.Subject == nil {
		item.Subject = []image.AttestationSubject{}
	}
	if query.IncludeStatement {
		item.Statement = append([]byte(nil), rawStatement...)
	}
	return item, true, nil
}

func dedupeAttestationsInternal(attestations []image.Attestation) []image.Attestation {
	seen := make(map[string]struct{}, len(attestations))
	out := make([]image.Attestation, 0, len(attestations))
	for _, item := range attestations {
		key := strings.Join([]string{
			item.Digest,
			item.PredicateType,
			item.Platform,
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func parseAttestationStatementInternal(rawStatement []byte) (attestationStatementEnvelopeInternal, error) {
	var statement attestationStatementEnvelopeInternal
	if err := json.Unmarshal(rawStatement, &statement); err != nil {
		return attestationStatementEnvelopeInternal{}, err
	}
	return statement, nil
}

type attestationStatementEnvelopeInternal struct {
	Type          string                     `json:"_type"`
	PredicateType string                     `json:"predicateType"`
	Subject       []image.AttestationSubject `json:"subject"`
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
		decoder, err := zstdDecoder()
		if err != nil {
			return nil, err
		}
		return decoder.DecodeAll(data, nil)
	default:
		return data, nil
	}
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

func isInlineAttestationDescriptorInternal(descriptor v1.Descriptor) bool {
	return descriptor.Annotations[attestation.DockerAnnotationReferenceType] == attestation.DockerAnnotationReferenceTypeDefault ||
		descriptor.ArtifactType == dockerAttestationManifestArtifactType
}

const (
	dockerAttestationManifestArtifactType = "application/vnd.docker.attestation.manifest.v1+json"
	inTotoLayerMediaType                  = "application/vnd.in-toto+json"
	inTotoPredicateTypeAnnotation         = "in-toto.io/predicate-type"
)

func shouldIgnoreReferrersErrorInternal(err error) bool {
	if transportErr, ok := errors.AsType[*transport.Error](err); ok {
		return transportErr.StatusCode == http.StatusNotFound || transportErr.StatusCode == http.StatusMethodNotAllowed
	}
	return false
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

func digestFromReferenceInternal(imageRef string) string {
	if _, digest, found := strings.Cut(strings.TrimSpace(imageRef), "@"); found {
		return digest
	}
	return ""
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

func isUsableImageReferenceInternal(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.Contains(value, "<none>")
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
