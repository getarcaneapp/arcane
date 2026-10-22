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
	"slices"
	"strings"
	"sync"

	"github.com/containerd/platforms"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/klauspost/compress/zstd"
	"github.com/moby/buildkit/util/attestation"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/registryauth"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

const (
	dockerAttestationManifestArtifactType = "application/vnd.docker.attestation.manifest.v1+json"
	inTotoLayerMediaType                  = "application/vnd.in-toto+json"
	inTotoPredicateTypeAnnotation         = "in-toto.io/predicate-type"
	maxAttestationLayerBytes              = 16 << 20
)

// zstdDecoder is shared because DecodeAll is safe for concurrent use and a decoder is costly to build.
var zstdDecoder = sync.OnceValues(func() (*zstd.Decoder, error) {
	return zstd.NewReader(nil, zstd.WithDecoderMaxMemory(maxAttestationLayerBytes))
})

var safeRegistryTransport = sync.OnceValues(func() (http.RoundTripper, error) {
	safeClient, err := httpx.NewSafeOutboundHTTPClient(nil, httpx.DefaultLookupIP)
	if err != nil {
		return nil, err
	}
	return safeClient.Transport, nil
})

var errAttestationLayerTooLarge = common.Classify(common.ErrBadRequest, fmt.Errorf("attestation layer exceeds the %d MiB limit", maxAttestationLayerBytes>>20))

// Service reads in-toto attestations attached to local images and registry references.
type Service struct {
	dockerService *docker.DockerClientService
	registryAuth  func(ctx context.Context, imageRef string) (string, error)
}

type Query struct {
	Platform         string
	PredicateType    string
	IncludeStatement bool
}

type reference struct {
	ImageRef      string
	SubjectDigest string
	Empty         bool
	External      bool
}

type subject struct {
	Descriptor v1.Descriptor
	Digest     string
	Platform   string
}

type attestationStatementEnvelope struct {
	Type          string                          `json:"_type"`
	PredicateType string                          `json:"predicateType"`
	Subject       []types.ImageAttestationSubject `json:"subject"`
}

func NewService(
	dockerService *docker.DockerClientService,
	registryAuth func(ctx context.Context, imageRef string) (string, error),
) *Service {
	return &Service{dockerService: dockerService, registryAuth: registryAuth}
}

// GetImageAttestations returns in-toto attestations attached to a local image or registry reference.
func (s *Service) GetImageAttestations(ctx context.Context, imageName string, query Query) (*types.ImageAttestationList, error) {
	ctx, cancel := context.WithTimeout(ctx, timeouts.DefaultRegistry)
	defer cancel()

	resolution, err := s.resolveImageAttestationReference(ctx, imageName)
	if err != nil {
		return nil, err
	}
	if resolution.Empty {
		return &types.ImageAttestationList{
			ImageRef:      resolution.ImageRef,
			SubjectDigest: resolution.SubjectDigest,
			Platform:      strings.TrimSpace(query.Platform),
			Attestations:  []types.ImageAttestation{},
		}, nil
	}

	platform, hasPlatform, err := func() (ocispec.Platform, bool, error) {
		value := strings.TrimSpace(query.Platform)
		if value == "" {
			return ocispec.Platform{}, false, nil
		}
		parsed, parseErr := platforms.Parse(value)
		if parseErr != nil {
			return ocispec.Platform{}, false, common.Classify(common.ErrBadRequest, fmt.Errorf("invalid platform %q: %w", value, parseErr))
		}
		return parsed, true, nil
	}()
	if err != nil {
		return nil, err
	}

	ref, err := name.ParseReference(resolution.ImageRef, name.WeakValidation)
	if err != nil {
		return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("parse image reference %q: %w", resolution.ImageRef, err))
	}

	remoteOptions, err := s.remoteOptionsForImageRef(ctx, resolution)
	if err != nil {
		return nil, err
	}
	remoteDescriptor, err := remote.Get(ref, remoteOptions...)
	if err != nil {
		return nil, classifyRegistryError(ctx, fmt.Errorf("get image manifest %q: %w", resolution.ImageRef, err))
	}

	index, targets, err := subjects(remoteDescriptor, platform, hasPlatform)
	if err != nil {
		return nil, err
	}

	attestations := make([]types.ImageAttestation, 0)
	for _, target := range targets {
		referrerAttestations, readReferrerAttestationsErr := s.readReferrerAttestations(ctx, ref.Context(), target, remoteOptions, query)
		if readReferrerAttestationsErr != nil {
			return nil, readReferrerAttestationsErr
		}
		attestations = append(attestations, referrerAttestations...)
	}

	if index != nil && len(targets) > 0 {
		indexManifest, indexManifestErr := index.IndexManifest()
		if indexManifestErr != nil {
			return nil, fmt.Errorf("read image index manifest for inline attestations: %w", indexManifestErr)
		}

		subjectByDigest := make(map[string]subject, len(targets))
		for _, target := range targets {
			subjectByDigest[target.Digest] = target
		}

		for _, descriptor := range indexManifest.Manifests {
			referenceDigest := strings.TrimSpace(descriptor.Annotations[attestation.DockerAnnotationReferenceDigest])
			target, ok := subjectByDigest[referenceDigest]
			if (descriptor.Annotations[attestation.DockerAnnotationReferenceType] != attestation.DockerAnnotationReferenceTypeDefault &&
				descriptor.ArtifactType != dockerAttestationManifestArtifactType) || (referenceDigest != "" && !ok) {
				continue
			}

			attestationImage, imageErr := index.Image(descriptor.Digest)
			if imageErr != nil {
				return nil, fmt.Errorf("read inline attestation image %s: %w", descriptor.Digest.String(), imageErr)
			}

			items, imageErr := readAttestationImage(ctx, attestationImage, descriptor, kit.Ternary(referenceDigest == "", targets[0].Platform, target.Platform), query)
			if imageErr != nil {
				return nil, imageErr
			}
			attestations = append(attestations, items...)
		}
	}

	seen := make(map[string]struct{}, len(attestations))
	return &types.ImageAttestationList{
		ImageRef: resolution.ImageRef,
		SubjectDigest: func() string {
			if hasPlatform && len(targets) > 0 {
				return targets[0].Digest
			}
			return cmp.Or(resolution.SubjectDigest, remoteDescriptor.Digest.String())
		}(),
		Platform: func() string {
			if hasPlatform && len(targets) > 0 {
				return targets[0].Platform
			}
			return strings.TrimSpace(query.Platform)
		}(),
		Attestations: slices.DeleteFunc(attestations, func(item types.ImageAttestation) bool {
			key := strings.Join([]string{
				item.Digest,
				item.PredicateType,
				item.Platform,
			}, "|")
			_, duplicate := seen[key]
			seen[key] = struct{}{}
			return duplicate
		}),
	}, nil
}

func (s *Service) resolveImageAttestationReference(ctx context.Context, imageName string) (reference, error) {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" {
		return reference{}, common.Classify(common.ErrBadRequest, errors.New("image name is required"))
	}

	if inspect, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
		return dockerClient.ImageInspect(ctx, imageName)
	}); inspectErr == nil {
		imageRef := func() string {
			for _, candidate := range inspect.RepoDigests {
				if trimmed := strings.TrimSpace(candidate); strings.Contains(trimmed, "@sha256:") && !strings.Contains(trimmed, "<none>") {
					return trimmed
				}
			}
			return ""
		}()
		if imageRef == "" {
			return reference{ImageRef: imageName, Empty: true}, nil
		}
		_, subjectDigest, _ := strings.Cut(imageRef, "@")
		return reference{
			ImageRef:      imageRef,
			SubjectDigest: subjectDigest,
		}, nil
	}

	if strings.HasPrefix(imageName, "sha256:") || (len(imageName) == 64 && !strings.ContainsFunc(imageName, func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F')
	})) {
		return reference{}, common.Classify(common.ErrNotFound, fmt.Errorf("image %q does not have a registry reference", imageName))
	}
	if _, err := name.ParseReference(imageName, name.WeakValidation); err != nil {
		return reference{}, common.Classify(common.ErrBadRequest, fmt.Errorf("image %q does not have a registry reference: %w", imageName, err))
	}
	_, subjectDigest, _ := strings.Cut(imageName, "@")
	return reference{
		ImageRef:      imageName,
		SubjectDigest: subjectDigest,
		External:      true,
	}, nil
}

func (s *Service) remoteOptionsForImageRef(ctx context.Context, resolution reference) ([]remote.Option, error) {
	imageRef := resolution.ImageRef
	options := []remote.Option{remote.WithContext(ctx)}
	if resolution.External {
		safeTransport, err := safeRegistryTransport()
		if err != nil {
			return nil, fmt.Errorf("configure safe registry transport: %w", err)
		}
		options = append(options, remote.WithTransport(safeTransport))
	}

	encodedAuth, err := s.registryAuth(ctx, imageRef)
	if err != nil {
		slog.DebugContext(ctx, "registry auth lookup failed for attestation request", "image", imageRef, "error", err)
		return options, nil
	}
	if strings.TrimSpace(encodedAuth) == "" {
		return options, nil
	}

	dockerAuth, err := registryauth.DecodeAuthHeader(encodedAuth)
	if err != nil {
		slog.DebugContext(ctx, "registry auth decode failed for attestation request", "image", imageRef, "error", err)
		return options, nil
	}

	return append(options, remote.WithAuth(authn.FromConfig(authn.AuthConfig{
		Username:      dockerAuth.Username,
		Password:      dockerAuth.Password,
		Auth:          dockerAuth.Auth,
		IdentityToken: dockerAuth.IdentityToken,
		RegistryToken: dockerAuth.RegistryToken,
	}))), nil
}

func classifyRegistryError(ctx context.Context, err error) error {
	if errors.Is(err, errAttestationLayerTooLarge) {
		return errAttestationLayerTooLarge
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	slog.WarnContext(ctx, "attestation registry request failed", "error", err)
	transportErr, isTransport := errors.AsType[*transport.Error](err)
	switch {
	case errors.Is(err, common.ErrUnsafeRemoteURL):
		return common.Classify(common.ErrBadRequest, errors.New("registry address is not allowed"))
	case errors.Is(err, context.DeadlineExceeded):
		return common.Classify(common.ErrTimeout, errors.New("registry request timed out"))
	case isTransport && (transportErr.StatusCode == http.StatusUnauthorized || transportErr.StatusCode == http.StatusForbidden):
		return common.Classify(common.ErrBadRequest, errors.New("registry denied access with the configured credentials"))
	case isTransport && transportErr.StatusCode == http.StatusNotFound:
		return common.Classify(common.ErrNotFound, errors.New("image not found in the registry"))
	default:
		return common.Classify(common.ErrUnavailable, errors.New("registry unavailable"))
	}
}

func subjects(descriptor *remote.Descriptor, platform ocispec.Platform, hasPlatform bool) (v1.ImageIndex, []subject, error) {
	root := subject{
		Descriptor: descriptor.Descriptor,
		Digest:     descriptor.Digest.String(),
		Platform:   kit.Ternary(hasPlatform, platforms.Format(platform), ""),
	}
	if !descriptor.MediaType.IsIndex() {
		return nil, []subject{root}, nil
	}

	index, err := descriptor.ImageIndex()
	if err != nil {
		return nil, nil, fmt.Errorf("read image index %s: %w", descriptor.Digest.String(), err)
	}

	indexManifest, err := index.IndexManifest()
	if err != nil {
		return nil, nil, fmt.Errorf("read image index manifest %s: %w", descriptor.Digest.String(), err)
	}

	candidates := make([]subject, 0, len(indexManifest.Manifests)+1)
	if !hasPlatform {
		candidates = append(candidates, root)
	}
	for _, child := range indexManifest.Manifests {
		childPlatform := ocispec.Platform{
			Architecture: kit.FromPtr(child.Platform).Architecture,
			OS:           kit.FromPtr(child.Platform).OS,
			OSVersion:    kit.FromPtr(child.Platform).OSVersion,
			OSFeatures:   kit.FromPtr(child.Platform).OSFeatures,
			Variant:      kit.FromPtr(child.Platform).Variant,
		}
		if child.Annotations[attestation.DockerAnnotationReferenceType] == attestation.DockerAnnotationReferenceTypeDefault ||
			child.ArtifactType == dockerAttestationManifestArtifactType ||
			(hasPlatform && (child.Platform == nil || !platforms.Only(platform).Match(childPlatform))) {
			continue
		}
		candidates = append(candidates, subject{
			Descriptor: child,
			Digest:     child.Digest.String(),
			Platform: cmp.Or(
				kit.Ternary(childPlatform.OS != "" && childPlatform.Architecture != "", platforms.Format(childPlatform), ""),
				kit.Ternary(hasPlatform, platforms.Format(platform), ""),
			),
		})
	}

	seen := make(map[string]struct{}, len(candidates))
	return index, slices.DeleteFunc(candidates, func(candidate subject) bool {
		key := candidate.Digest + "|" + candidate.Platform
		_, duplicate := seen[key]
		seen[key] = struct{}{}
		return candidate.Digest == "" || duplicate
	}), nil
}

func (s *Service) readReferrerAttestations(
	ctx context.Context,
	repository name.Repository,
	target subject,
	remoteOptions []remote.Option,
	query Query,
) ([]types.ImageAttestation, error) {
	referrers, err := remote.Referrers(repository.Digest(target.Digest), remoteOptions...)
	if err != nil {
		if transportErr, ok := errors.AsType[*transport.Error](err); ok &&
			(transportErr.StatusCode == http.StatusNotFound || transportErr.StatusCode == http.StatusMethodNotAllowed) {
			return nil, nil
		}
		return nil, classifyRegistryError(ctx, fmt.Errorf("query image referrers for %s: %w", target.Digest, err))
	}

	manifest, err := referrers.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("read image referrers for %s: %w", target.Digest, err)
	}

	attestations := make([]types.ImageAttestation, 0, len(manifest.Manifests))
	for _, referrer := range manifest.Manifests {
		referrerDescriptor, getErr := remote.Get(repository.Digest(referrer.Digest.String()), remoteOptions...)
		if getErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, classifyRegistryError(ctx, ctxErr)
			}
			slog.WarnContext(ctx, "skipping unreadable image referrer", "subject", target.Digest, "referrer", referrer.Digest.String(), "error", getErr)
			continue
		}

		referrerImage, getErr := referrerDescriptor.Image()
		if getErr != nil {
			continue
		}

		items, getErr := readAttestationImage(ctx, referrerImage, v1.Descriptor{ //exhaustruct:enforce
			MediaType:    cmp.Or(referrer.MediaType, referrerDescriptor.MediaType),
			Size:         referrer.Size,
			Digest:       referrer.Digest,
			Data:         referrer.Data,
			URLs:         referrer.URLs,
			Annotations:  referrer.Annotations,
			Platform:     referrer.Platform,
			ArtifactType: cmp.Or(referrer.ArtifactType, referrerDescriptor.ArtifactType),
		}, target.Platform, query)
		if getErr != nil {
			if errors.Is(getErr, errAttestationLayerTooLarge) {
				return nil, errAttestationLayerTooLarge
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, classifyRegistryError(ctx, ctxErr)
			}
			slog.WarnContext(ctx, "skipping unreadable image referrer", "subject", target.Digest, "referrer", referrer.Digest.String(), "error", getErr)
			continue
		}
		attestations = append(attestations, items...)
	}
	return attestations, nil
}

func readAttestationImage(ctx context.Context, attestationImage v1.Image, artifactDescriptor v1.Descriptor, platform string, query Query) ([]types.ImageAttestation, error) {
	manifest, err := attestationImage.Manifest()
	if err != nil {
		return nil, fmt.Errorf("read attestation manifest %s: %w", artifactDescriptor.Digest.String(), err)
	}

	attestations := make([]types.ImageAttestation, 0, len(manifest.Layers))
	for _, layerDescriptor := range manifest.Layers {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		annotationPredicate := strings.TrimSpace(layerDescriptor.Annotations[inTotoPredicateTypeAnnotation])
		if layerDescriptor.MediaType != inTotoLayerMediaType ||
			(query.PredicateType != "" && annotationPredicate != "" && annotationPredicate != query.PredicateType) {
			continue
		}

		layer, layerErr := attestationImage.LayerByDigest(layerDescriptor.Digest)
		if layerErr != nil {
			return nil, fmt.Errorf("read attestation layer %s: %w", layerDescriptor.Digest.String(), layerErr)
		}

		rawStatement, layerErr := readAttestationLayerBytes(ctx, layer, layerDescriptor.Digest.String(), layerDescriptor.Size)
		if layerErr != nil {
			return nil, layerErr
		}

		var statement attestationStatementEnvelope
		if unmarshalErr := json.Unmarshal(rawStatement, &statement); unmarshalErr != nil {
			return nil, fmt.Errorf("parse attestation statement %s: %w", layerDescriptor.Digest.String(), unmarshalErr)
		}
		if query.PredicateType != "" && cmp.Or(statement.PredicateType, annotationPredicate) != query.PredicateType {
			continue
		}

		attestations = append(attestations, types.ImageAttestation{
			Digest:        layerDescriptor.Digest.String(),
			MediaType:     string(layerDescriptor.MediaType),
			ArtifactType:  cmp.Or(artifactDescriptor.ArtifactType, manifest.ArtifactType),
			PredicateType: cmp.Or(statement.PredicateType, annotationPredicate),
			StatementType: statement.Type,
			Subject:       kit.Ternary(statement.Subject == nil, []types.ImageAttestationSubject{}, statement.Subject),
			Platform:      platform,
			Size:          cmp.Or(layerDescriptor.Size, int64(len(rawStatement))),
			Statement: func() []byte {
				if !query.IncludeStatement {
					return nil
				}
				return append([]byte(nil), rawStatement...)
			}(),
		})
	}
	return attestations, nil
}

// readAttestationLayerBytes returns the raw in-toto statement
// bytes, transparently decompressing OCI blobs that are stored compressed. The
// in-toto media type does not encode the compression algorithm, so the
// container is detected from the magic header. gzip and zstd are the algorithms
// supported by the OCI image spec; anything else (including already
// uncompressed JSON) is returned unchanged.
func readAttestationLayerBytes(ctx context.Context, layer v1.Layer, digest string, size int64) ([]byte, error) {
	if size > maxAttestationLayerBytes {
		return nil, fmt.Errorf("read attestation layer %s: %w", digest, errAttestationLayerTooLarge)
	}
	reader, err := layer.Compressed()
	if err != nil {
		return nil, classifyRegistryError(ctx, fmt.Errorf("open attestation layer %s: %w", digest, err))
	}
	rawStatement, readErr := io.ReadAll(io.LimitReader(reader, maxAttestationLayerBytes+1))
	closeErr := reader.Close()
	switch {
	case readErr != nil:
		return nil, classifyRegistryError(ctx, fmt.Errorf("read attestation layer %s: %w", digest, readErr))
	case closeErr != nil:
		return nil, classifyRegistryError(ctx, fmt.Errorf("close attestation layer %s: %w", digest, closeErr))
	case len(rawStatement) > maxAttestationLayerBytes:
		return nil, fmt.Errorf("read attestation layer %s: %w", digest, errAttestationLayerTooLarge)
	case size > 0 && int64(len(rawStatement)) != size:
		return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("attestation layer %s is %d bytes, descriptor declares %d", digest, len(rawStatement), size))
	}

	statement, err := func() ([]byte, error) {
		switch {
		case len(rawStatement) >= 2 && rawStatement[0] == 0x1f && rawStatement[1] == 0x8b:
			gzipReader, gzipErr := gzip.NewReader(bytes.NewReader(rawStatement))
			if gzipErr != nil {
				return nil, gzipErr
			}
			defer func() { _ = gzipReader.Close() }()
			decompressed, gzipErr := io.ReadAll(io.LimitReader(gzipReader, maxAttestationLayerBytes+1))
			if gzipErr == nil && len(decompressed) > maxAttestationLayerBytes {
				return nil, errAttestationLayerTooLarge
			}
			return decompressed, gzipErr
		case len(rawStatement) >= 4 && rawStatement[0] == 0x28 && rawStatement[1] == 0xb5 && rawStatement[2] == 0x2f && rawStatement[3] == 0xfd:
			decoder, decoderErr := zstdDecoder()
			if decoderErr != nil {
				return nil, decoderErr
			}
			decompressed, decodeErr := decoder.DecodeAll(rawStatement, nil)
			if errors.Is(decodeErr, zstd.ErrDecoderSizeExceeded) {
				return nil, errAttestationLayerTooLarge
			}
			return decompressed, decodeErr
		default:
			return rawStatement, nil
		}
	}()
	if err != nil {
		return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("decompress attestation layer %s: %w", digest, err))
	}
	return statement, nil
}
