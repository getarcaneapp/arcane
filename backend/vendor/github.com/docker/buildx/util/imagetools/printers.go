package imagetools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"text/template"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

const defaultPfx = "  "

type Printer struct {
	ctx      context.Context
	resolver *Resolver

	name   string
	format string

	raw      []byte
	ref      reference.Named
	manifest ocispecs.Descriptor
	index    ocispecs.Index
}

func NewPrinter(ctx context.Context, opt Opt, name string, format string) (*Printer, error) {
	resolver := New(opt)

	ref, err := parseRef(name)
	if err != nil {
		return nil, err
	}

	dt, mfst, err := resolver.Get(ctx, ref.String())
	if err != nil {
		return nil, err
	}

	var idx ocispecs.Index
	if err = json.Unmarshal(dt, &idx); err != nil {
		return nil, err
	}

	return &Printer{
		ctx:      ctx,
		resolver: resolver,
		name:     name,
		format:   format,
		raw:      dt,
		ref:      ref,
		manifest: mfst,
		index:    idx,
	}, nil
}

func (p *Printer) Print(raw bool, out io.Writer) error {
	if raw {
		_, err := fmt.Fprintf(out, "%s", p.raw) // avoid newline to keep digest
		return err
	}

	if p.format == "" {
		w := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
		_, _ = fmt.Fprintf(w, "Name:\t%s\n", p.ref.String())
		_, _ = fmt.Fprintf(w, "MediaType:\t%s\n", p.manifest.MediaType)
		_, _ = fmt.Fprintf(w, "Digest:\t%s\n", p.manifest.Digest)
		_ = w.Flush()
		switch p.manifest.MediaType {
		case images.MediaTypeDockerSchema2ManifestList, ocispecs.MediaTypeImageIndex:
			if err := p.printManifestList(out); err != nil {
				return err
			}
		}
		return nil
	}

	res, err := newLoader(p.resolver.resolver()).Load(p.ctx, p.name)
	if err != nil {
		return err
	}

	tpl, err := template.New("").Funcs(template.FuncMap{
		"json": func(v any) string {
			b, _ := json.MarshalIndent(v, "", "  ")
			return string(b)
		},
	}).Parse(p.format)
	if err != nil {
		return err
	}

	imageconfigs := res.Configs()
	format := tpl.Root.String()

	var mfst any
	switch p.manifest.MediaType {
	case images.MediaTypeDockerSchema2Manifest, ocispecs.MediaTypeImageManifest:
		mfst = p.manifest
	case images.MediaTypeDockerSchema2ManifestList, ocispecs.MediaTypeImageIndex:
		mfst = struct {
			SchemaVersion int                   `json:"schemaVersion"`
			MediaType     string                `json:"mediaType,omitempty"`
			Digest        digest.Digest         `json:"digest"`
			Size          int64                 `json:"size"`
			Manifests     []ocispecs.Descriptor `json:"manifests"`
			Annotations   map[string]string     `json:"annotations,omitempty"`
		}{
			SchemaVersion: p.index.SchemaVersion,
			MediaType:     p.index.MediaType,
			Digest:        p.manifest.Digest,
			Size:          p.manifest.Size,
			Manifests:     p.index.Manifests,
			Annotations:   p.index.Annotations,
		}
	}

	switch {
	// TODO: print formatted config
	case strings.HasPrefix(format, "{{.Manifest"):
		w := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
		_, _ = fmt.Fprintf(w, "Name:\t%s\n", p.ref.String())
		switch {
		case strings.HasPrefix(format, "{{.Manifest"):
			_, _ = fmt.Fprintf(w, "MediaType:\t%s\n", p.manifest.MediaType)
			_, _ = fmt.Fprintf(w, "Digest:\t%s\n", p.manifest.Digest)
			_ = w.Flush()
			switch p.manifest.MediaType {
			case images.MediaTypeDockerSchema2ManifestList, ocispecs.MediaTypeImageIndex:
				_ = p.printManifestList(out)
			}
		}
	default:
		if len(res.platforms) > 1 {
			return tpl.Execute(out, tplInputs{
				Name:     p.name,
				Manifest: mfst,
				Image:    imageconfigs,
				result:   res,
			})
		}
		var ic *ocispecs.Image
		for _, v := range imageconfigs {
			ic = v
		}
		return tpl.Execute(out, tplInput{
			Name:     p.name,
			Manifest: mfst,
			Image:    ic,
			result:   res,
		})
	}

	return nil
}

func (p *Printer) printManifestList(out io.Writer) error {
	w := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
	_, _ = fmt.Fprintf(w, "\t\n")
	_, _ = fmt.Fprintf(w, "Manifests:\t\n")
	_ = w.Flush()

	w = tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	for i, m := range p.index.Manifests {
		if i != 0 {
			_, _ = fmt.Fprintf(w, "\t\n")
		}
		cr, err := reference.WithDigest(p.ref, m.Digest)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "%sName:\t%s\n", defaultPfx, cr.String())
		_, _ = fmt.Fprintf(w, "%sMediaType:\t%s\n", defaultPfx, m.MediaType)
		if p := m.Platform; p != nil {
			_, _ = fmt.Fprintf(w, "%sPlatform:\t%s\n", defaultPfx, platforms.Format(*p))
			if p.OSVersion != "" {
				_, _ = fmt.Fprintf(w, "%sOSVersion:\t%s\n", defaultPfx, p.OSVersion)
			}
			if len(p.OSFeatures) > 0 {
				_, _ = fmt.Fprintf(w, "%sOSFeatures:\t%s\n", defaultPfx, strings.Join(p.OSFeatures, ", "))
			}
			if len(m.URLs) > 0 {
				_, _ = fmt.Fprintf(w, "%sURLs:\t%s\n", defaultPfx, strings.Join(m.URLs, ", "))
			}
			if len(m.Annotations) > 0 {
				_, _ = fmt.Fprintf(w, "%sAnnotations:\t\n", defaultPfx)
				_ = w.Flush()
				w2 := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
				for k, v := range m.Annotations {
					_, _ = fmt.Fprintf(w2, "%s%s:\t%s\n", defaultPfx+defaultPfx, k, v)
				}
				_ = w2.Flush()
			}
		}
	}
	return w.Flush()
}

type tplInput struct {
	Name     string          `json:"name,omitempty"`
	Manifest any             `json:"manifest,omitempty"`
	Image    *ocispecs.Image `json:"image,omitempty"`

	result *result
}

func (inp tplInput) SBOM() (sbomStub, error) {
	sbom, err := inp.result.SBOM()
	if err != nil {
		return sbomStub{}, nil
	}
	for _, v := range sbom {
		return v, nil
	}
	return sbomStub{}, nil
}

func (inp tplInput) Provenance() (provenanceStub, error) {
	provenance, err := inp.result.Provenance()
	if err != nil {
		return provenanceStub{}, nil
	}
	for _, v := range provenance {
		return v, nil
	}
	return provenanceStub{}, nil
}

type tplInputs struct {
	Name     string                     `json:"name,omitempty"`
	Manifest any                        `json:"manifest,omitempty"`
	Image    map[string]*ocispecs.Image `json:"image,omitempty"`

	result *result
}

func (inp tplInputs) SBOM() (map[string]sbomStub, error) {
	return inp.result.SBOM()
}

func (inp tplInputs) Provenance() (map[string]provenanceStub, error) {
	return inp.result.Provenance()
}
