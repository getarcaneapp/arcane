package imagetools

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/url"
	"strings"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type Source struct {
	Desc ocispecs.Descriptor
	Ref  reference.Named
}

func (r *Resolver) Combine(ctx context.Context, srcs []*Source, ann map[exptypes.AnnotationKey]string, preferIndex bool) ([]byte, ocispecs.Descriptor, map[digest.Digest]*Source, error) {
	eg, ctx := errgroup.WithContext(ctx)

	dts := make([][]byte, len(srcs))
	for i := range dts {
		func(i int) {
			eg.Go(func() error {
				dt, err := r.GetDescriptor(ctx, srcs[i].Ref.String(), srcs[i].Desc)
				if err != nil {
					return err
				}
				dts[i] = dt

				if srcs[i].Desc.MediaType == "" {
					mt, err := detectMediaType(dt)
					if err != nil {
						return err
					}
					srcs[i].Desc.MediaType = mt
				}

				mt := srcs[i].Desc.MediaType

				switch mt {
				case images.MediaTypeDockerSchema2Manifest, ocispecs.MediaTypeImageManifest:
					p := srcs[i].Desc.Platform
					if srcs[i].Desc.Platform == nil {
						p = &ocispecs.Platform{}
					}
					if p.OS == "" || p.Architecture == "" {
						if err := r.loadPlatform(ctx, p, srcs[i].Ref.String(), dt); err != nil {
							return err
						}
					}
					srcs[i].Desc.Platform = p
				case images.MediaTypeDockerSchema1Manifest:
					return errors.Errorf("schema1 manifests are not allowed in manifest lists")
				}

				return nil
			})
		}(i)
	}

	if err := eg.Wait(); err != nil {
		return nil, ocispecs.Descriptor{}, nil, err
	}

	// on single source, return original bytes
	if len(srcs) == 1 && len(ann) == 0 {
		switch srcs[0].Desc.MediaType {
		// if the source is already an image index or manifest list, there is no need to consider the value
		// of preferIndex since if set to true then the source is already in the preferred format, and if false
		// it doesn't matter since we're not going to split it into separate manifests
		case images.MediaTypeDockerSchema2ManifestList, ocispecs.MediaTypeImageIndex:
			srcMap := map[digest.Digest]*Source{
				srcs[0].Desc.Digest: srcs[0],
			}
			return dts[0], srcs[0].Desc, srcMap, nil
		default:
			if !preferIndex {
				return dts[0], srcs[0].Desc, nil, nil
			}
		}
	}

	indexes := map[digest.Digest]int{}
	sources := map[digest.Digest]*Source{}
	descs := make([]ocispecs.Descriptor, 0, len(srcs))

	addDesc := func(d ocispecs.Descriptor, src *Source) {
		idx, ok := indexes[d.Digest]
		if ok {
			old := descs[idx]
			if old.MediaType == "" {
				old.MediaType = d.MediaType
			}
			if d.Platform != nil {
				old.Platform = d.Platform
			}
			if old.Annotations == nil {
				old.Annotations = map[string]string{}
			}
			maps.Copy(old.Annotations, d.Annotations)
			descs[idx] = old
		} else {
			indexes[d.Digest] = len(descs)
			descs = append(descs, d)
		}
		sources[d.Digest] = src
	}

	for i, src := range srcs {
		switch src.Desc.MediaType {
		case images.MediaTypeDockerSchema2ManifestList, ocispecs.MediaTypeImageIndex:
			var mfst ocispecs.Index
			if err := json.Unmarshal(dts[i], &mfst); err != nil {
				return nil, ocispecs.Descriptor{}, nil, errors.WithStack(err)
			}
			for _, d := range mfst.Manifests {
				addDesc(d, src)
			}
		default:
			addDesc(src.Desc, src)
		}
	}

	dockerMfsts := 0
	for _, desc := range descs {
		if strings.HasPrefix(desc.MediaType, "application/vnd.docker.") {
			dockerMfsts++
		}
	}

	var mt string
	if dockerMfsts == len(descs) {
		// all manifests are Docker types, use Docker manifest list
		mt = images.MediaTypeDockerSchema2ManifestList
	} else {
		// otherwise, use OCI index
		mt = ocispecs.MediaTypeImageIndex
	}

	// annotations are only allowed on OCI indexes
	indexAnnotation := make(map[string]string)
	if mt == ocispecs.MediaTypeImageIndex {
		for k, v := range ann {
			switch k.Type {
			case exptypes.AnnotationIndex:
				indexAnnotation[k.Key] = v
			case exptypes.AnnotationManifestDescriptor:
				for i := range descs {
					if descs[i].Annotations == nil {
						descs[i].Annotations = map[string]string{}
					}
					if k.Platform == nil || k.PlatformString() == platforms.Format(*descs[i].Platform) {
						descs[i].Annotations[k.Key] = v
					}
				}
			case exptypes.AnnotationManifest, "":
				return nil, ocispecs.Descriptor{}, nil, errors.Errorf("%q annotations are not supported yet", k.Type)
			case exptypes.AnnotationIndexDescriptor:
				return nil, ocispecs.Descriptor{}, nil, errors.Errorf("%q annotations are invalid while creating an image", k.Type)
			}
		}
	}

	idxBytes, err := json.MarshalIndent(ocispecs.Index{
		MediaType: mt,
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		Manifests:   descs,
		Annotations: indexAnnotation,
	}, "", "  ")
	if err != nil {
		return nil, ocispecs.Descriptor{}, nil, errors.Wrap(err, "failed to marshal index")
	}

	return idxBytes, ocispecs.Descriptor{
		MediaType: mt,
		Size:      int64(len(idxBytes)),
		Digest:    digest.FromBytes(idxBytes),
	}, sources, nil
}

func (r *Resolver) Push(ctx context.Context, ref reference.Named, desc ocispecs.Descriptor, dt []byte) error {
	ctx = remotes.WithMediaTypeKeyPrefix(ctx, "application/vnd.in-toto+json", "intoto")

	fullRef, err := reference.WithDigest(reference.TagNameOnly(ref), desc.Digest)
	if err != nil {
		return errors.Wrapf(err, "failed to combine ref %s with digest %s", ref, desc.Digest)
	}
	p, err := r.resolver().Pusher(ctx, fullRef.String())
	if err != nil {
		return err
	}
	cw, err := p.Push(ctx, desc)
	if err != nil {
		if errdefs.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	err = content.Copy(ctx, cw, bytes.NewReader(dt), desc.Size, desc.Digest)
	if errdefs.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (r *Resolver) Copy(ctx context.Context, src *Source, dest reference.Named) error {
	ctx = remotes.WithMediaTypeKeyPrefix(ctx, "application/vnd.in-toto+json", "intoto")
	ctx = remotes.WithMediaTypeKeyPrefix(ctx, "application/vnd.oci.empty.v1+json", "empty")

	// push by digest
	p, err := r.resolver().Pusher(ctx, dest.Name())
	if err != nil {
		return err
	}

	srcRef := reference.TagNameOnly(src.Ref)
	f, err := r.resolver().Fetcher(ctx, srcRef.String())
	if err != nil {
		return err
	}

	refspec := reference.TrimNamed(src.Ref).String()
	u, err := url.Parse("dummy://" + refspec)
	if err != nil {
		return err
	}

	desc := src.Desc
	desc.Annotations = maps.Clone(desc.Annotations)
	if desc.Annotations == nil {
		desc.Annotations = make(map[string]string)
	}

	source, repo := u.Hostname(), strings.TrimPrefix(u.Path, "/")
	desc.Annotations["containerd.io/distribution.source."+source] = repo

	err = contentutil.CopyChain(ctx, contentutil.FromPusher(p), contentutil.FromFetcher(f), desc)
	if err != nil {
		return err
	}
	return nil
}

func (r *Resolver) loadPlatform(ctx context.Context, p2 *ocispecs.Platform, in string, dt []byte) error {
	var manifest ocispecs.Manifest
	if err := json.Unmarshal(dt, &manifest); err != nil {
		return errors.WithStack(err)
	}

	dt, err := r.GetDescriptor(ctx, in, manifest.Config)
	if err != nil {
		return err
	}

	var p ocispecs.Platform
	if err := json.Unmarshal(dt, &p); err != nil {
		return errors.WithStack(err)
	}

	p = platforms.Normalize(p)

	if p2.Architecture == "" {
		p2.Architecture = p.Architecture
		if p2.Variant == "" {
			p2.Variant = p.Variant
		}
	}
	if p2.OS == "" {
		p2.OS = p.OS
	}

	return nil
}

func detectMediaType(dt []byte) (string, error) {
	var mfst struct {
		MediaType string          `json:"mediaType"`
		Config    json.RawMessage `json:"config"`
		FSLayers  []string        `json:"fsLayers"`
	}

	if err := json.Unmarshal(dt, &mfst); err != nil {
		return "", errors.WithStack(err)
	}

	if mfst.MediaType != "" {
		return mfst.MediaType, nil
	}
	if mfst.Config != nil {
		return images.MediaTypeDockerSchema2Manifest, nil
	}
	if len(mfst.FSLayers) > 0 {
		return images.MediaTypeDockerSchema1Manifest, nil
	}

	return images.MediaTypeDockerSchema2ManifestList, nil
}
