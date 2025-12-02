package imagetools

// TODO: replace with go-imageinspect library when public

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"maps"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	intoto "github.com/in-toto/in-toto-golang/in_toto"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const (
	inTotoGenericMime        = "application/vnd.in-toto+json"
	inTotoSPDXDSSEMime       = "application/vnd.in-toto.spdx+dsse"
	inTotoProvenanceDSSEMime = "application/vnd.in-toto.provenance+dsse"
)

var (
	annotationReferences = []string{
		"com.docker.reference.digest",
		"vnd.docker.reference.digest", // TODO: deprecate/remove after migration to new annotation
	}
)

type contentCache interface {
	content.Provider
	content.Ingester
}

type loader struct {
	resolver remotes.Resolver
	cache    contentCache
}

type manifest struct {
	desc     ocispecs.Descriptor
	manifest ocispecs.Manifest
}

type index struct {
	desc  ocispecs.Descriptor
	index ocispecs.Index
}

type asset struct {
	config     *ocispecs.Image
	sbom       *sbomStub
	provenance *provenanceStub

	deferredSbom       func() (*sbomStub, error)
	deferredProvenance func() (*provenanceStub, error)
}

type result struct {
	mu        sync.Mutex
	indexes   map[digest.Digest]index
	manifests map[digest.Digest]manifest
	images    map[string]digest.Digest
	refs      map[digest.Digest][]digest.Digest

	platforms []string
	assets    map[string]asset
}

func newLoader(resolver remotes.Resolver) *loader {
	return &loader{
		resolver: resolver,
		cache:    contentutil.NewBuffer(),
	}
}

func (l *loader) Load(ctx context.Context, ref string) (*result, error) {
	named, err := parseRef(ref)
	if err != nil {
		return nil, err
	}

	_, desc, err := l.resolver.Resolve(ctx, named.String())
	if err != nil {
		return nil, err
	}

	canonical, err := reference.WithDigest(named, desc.Digest)
	if err != nil {
		return nil, err
	}

	fetcher, err := l.resolver.Fetcher(ctx, canonical.String())
	if err != nil {
		return nil, err
	}

	r := &result{
		indexes:   make(map[digest.Digest]index),
		manifests: make(map[digest.Digest]manifest),
		images:    make(map[string]digest.Digest),
		refs:      make(map[digest.Digest][]digest.Digest),
		assets:    make(map[string]asset),
	}

	if err := l.fetch(ctx, fetcher, desc, r); err != nil {
		return nil, err
	}

	for platform, dgst := range r.images {
		r.platforms = append(r.platforms, platform)

		mfst, ok := r.manifests[dgst]
		if !ok {
			return nil, errors.Errorf("image %s not found", platform)
		}

		var a asset
		annotations := map[string]string{}
		maps.Copy(annotations, mfst.desc.Annotations)
		maps.Copy(annotations, mfst.manifest.Annotations)

		if err := l.scanConfig(ctx, fetcher, mfst.manifest.Config, &a); err != nil {
			return nil, err
		}

		refs, ok := r.refs[dgst]
		if ok {
			if err := l.scanSBOM(ctx, fetcher, r, refs, &a); err != nil {
				return nil, err
			}
		}

		if err := l.scanProvenance(ctx, fetcher, r, refs, &a); err != nil {
			return nil, err
		}

		r.assets[platform] = a
	}

	sort.Strings(r.platforms)
	return r, nil
}

func (l *loader) fetch(ctx context.Context, fetcher remotes.Fetcher, desc ocispecs.Descriptor, r *result) error {
	_, err := remotes.FetchHandler(l.cache, fetcher)(ctx, desc)
	if err != nil {
		return err
	}

	switch desc.MediaType {
	case images.MediaTypeDockerSchema2Manifest, ocispecs.MediaTypeImageManifest:
		var mfst ocispecs.Manifest
		dt, err := content.ReadBlob(ctx, l.cache, desc)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(dt, &mfst); err != nil {
			return err
		}
		r.mu.Lock()
		r.manifests[desc.Digest] = manifest{
			desc:     desc,
			manifest: mfst,
		}
		r.mu.Unlock()

		found := false
		for _, annotationReference := range annotationReferences {
			ref, ok := desc.Annotations[annotationReference]
			if !ok {
				continue
			}

			refdgst, err := digest.Parse(ref)
			if err != nil {
				return err
			}
			r.mu.Lock()
			r.refs[refdgst] = append(r.refs[refdgst], desc.Digest)
			r.mu.Unlock()
			found = true
			break
		}
		if !found {
			p := desc.Platform
			if p == nil {
				p, err = l.readPlatformFromConfig(ctx, fetcher, mfst.Config)
				if err != nil {
					return err
				}
			}
			r.mu.Lock()
			r.images[platforms.Format(platforms.Normalize(*p))] = desc.Digest
			r.mu.Unlock()
		}
	case images.MediaTypeDockerSchema2ManifestList, ocispecs.MediaTypeImageIndex:
		var idx ocispecs.Index
		dt, err := content.ReadBlob(ctx, l.cache, desc)
		if err != nil {
			return err
		}

		if err := json.Unmarshal(dt, &idx); err != nil {
			return err
		}

		r.mu.Lock()
		r.indexes[desc.Digest] = index{
			desc:  desc,
			index: idx,
		}
		r.mu.Unlock()

		eg, ctx := errgroup.WithContext(ctx)
		for _, d := range idx.Manifests {
			eg.Go(func() error {
				return l.fetch(ctx, fetcher, d, r)
			})
		}

		if err := eg.Wait(); err != nil {
			return err
		}
	default:
	}
	return nil
}

func (l *loader) readPlatformFromConfig(ctx context.Context, fetcher remotes.Fetcher, desc ocispecs.Descriptor) (*ocispecs.Platform, error) {
	_, err := remotes.FetchHandler(l.cache, fetcher)(ctx, desc)
	if err != nil {
		return nil, err
	}

	dt, err := content.ReadBlob(ctx, l.cache, desc)
	if err != nil {
		return nil, err
	}

	var config ocispecs.Image
	if err := json.Unmarshal(dt, &config); err != nil {
		return nil, err
	}

	return &ocispecs.Platform{
		OS:           config.OS,
		Architecture: config.Architecture,
		Variant:      config.Variant,
	}, nil
}

func (l *loader) scanConfig(ctx context.Context, fetcher remotes.Fetcher, desc ocispecs.Descriptor, as *asset) error {
	_, err := remotes.FetchHandler(l.cache, fetcher)(ctx, desc)
	if err != nil {
		return err
	}
	dt, err := content.ReadBlob(ctx, l.cache, desc)
	if err != nil {
		return err
	}
	return json.Unmarshal(dt, &as.config)
}

type sbomStub struct {
	SPDX            any   `json:",omitempty"`
	AdditionalSPDXs []any `json:",omitempty"`
}

func (l *loader) scanSBOM(ctx context.Context, fetcher remotes.Fetcher, r *result, refs []digest.Digest, as *asset) error {
	ctx = withIntotoMediaTypes(ctx)
	as.deferredSbom = func() (*sbomStub, error) {
		var sbom *sbomStub
		for _, dgst := range refs {
			mfst, ok := r.manifests[dgst]
			if !ok {
				return nil, errors.Errorf("referenced image %s not found", dgst)
			}
			for _, layer := range mfst.manifest.Layers {
				if (layer.MediaType == inTotoGenericMime || isInTotoDSSE(layer.MediaType)) &&
					layer.Annotations["in-toto.io/predicate-type"] == intoto.PredicateSPDX {
					_, err := remotes.FetchHandler(l.cache, fetcher)(ctx, layer)
					if err != nil {
						return nil, err
					}
					dt, err := content.ReadBlob(ctx, l.cache, layer)
					if err != nil {
						return nil, err
					}

					dt, err = decodeDSSE(dt, layer.MediaType)
					if err != nil {
						return nil, err
					}

					var spdx struct {
						Predicate any `json:"predicate"`
					}
					if err := json.Unmarshal(dt, &spdx); err != nil {
						return nil, err
					}

					if sbom == nil {
						sbom = &sbomStub{}
						sbom.SPDX = spdx.Predicate
					} else {
						sbom.AdditionalSPDXs = append(sbom.AdditionalSPDXs, spdx.Predicate)
					}
				}
			}
		}
		return sbom, nil
	}
	return nil
}

type provenanceStub struct {
	SLSA any `json:",omitempty"`
}

func (l *loader) scanProvenance(ctx context.Context, fetcher remotes.Fetcher, r *result, refs []digest.Digest, as *asset) error {
	ctx = withIntotoMediaTypes(ctx)
	as.deferredProvenance = func() (*provenanceStub, error) {
		var provenance *provenanceStub
		for _, dgst := range refs {
			mfst, ok := r.manifests[dgst]
			if !ok {
				return nil, errors.Errorf("referenced image %s not found", dgst)
			}
			for _, layer := range mfst.manifest.Layers {
				if (layer.MediaType == inTotoGenericMime || isInTotoDSSE(layer.MediaType)) &&
					strings.HasPrefix(layer.Annotations["in-toto.io/predicate-type"], "https://slsa.dev/provenance/") {
					_, err := remotes.FetchHandler(l.cache, fetcher)(ctx, layer)
					if err != nil {
						return nil, err
					}
					dt, err := content.ReadBlob(ctx, l.cache, layer)
					if err != nil {
						return nil, err
					}

					dt, err = decodeDSSE(dt, layer.MediaType)
					if err != nil {
						return nil, err
					}

					var slsa struct {
						Predicate any `json:"predicate"`
					}
					if err := json.Unmarshal(dt, &slsa); err != nil {
						return nil, err
					}
					provenance = &provenanceStub{
						SLSA: slsa.Predicate,
					}
					break
				}
			}
		}
		return provenance, nil
	}
	return nil
}

func (r *result) Configs() map[string]*ocispecs.Image {
	if len(r.assets) == 0 {
		return nil
	}
	res := make(map[string]*ocispecs.Image)
	for p, a := range r.assets {
		if a.config == nil {
			continue
		}
		res[p] = a.config
	}
	return res
}

func (r *result) Provenance() (map[string]provenanceStub, error) {
	if len(r.assets) == 0 {
		return nil, nil
	}
	res := make(map[string]provenanceStub)
	for p, a := range r.assets {
		if a.deferredProvenance == nil {
			continue
		}
		if a.provenance == nil {
			provenance, err := a.deferredProvenance()
			if err != nil {
				return nil, err
			}
			if provenance == nil {
				continue
			}
			a.provenance = provenance
		}
		res[p] = *a.provenance
	}
	return res, nil
}

func (r *result) SBOM() (map[string]sbomStub, error) {
	if len(r.assets) == 0 {
		return nil, nil
	}
	res := make(map[string]sbomStub)
	for p, a := range r.assets {
		if a.deferredSbom == nil {
			continue
		}
		if a.sbom == nil {
			sbom, err := a.deferredSbom()
			if err != nil {
				return nil, err
			}
			if sbom == nil {
				continue
			}
			a.sbom = sbom
		}
		res[p] = *a.sbom
	}
	return res, nil
}

func isInTotoDSSE(mime string) bool {
	isDSSE, _ := regexp.MatchString("application/vnd\\.in-toto\\..*\\+dsse", mime)

	return isDSSE
}

func decodeDSSE(dt []byte, mime string) ([]byte, error) {
	if isInTotoDSSE(mime) {
		var dsse struct {
			Payload string `json:"payload"`
		}
		if err := json.Unmarshal(dt, &dsse); err != nil {
			return nil, err
		}

		decoded, err := base64.StdEncoding.DecodeString(dsse.Payload)
		if err != nil {
			return nil, err
		}

		dt = decoded
	}

	return dt, nil
}

func withIntotoMediaTypes(ctx context.Context) context.Context {
	for _, mime := range []string{inTotoGenericMime, inTotoSPDXDSSEMime, inTotoProvenanceDSSEMime} {
		ctx = remotes.WithMediaTypeKeyPrefix(ctx, mime, "intoto")
	}
	return ctx
}
