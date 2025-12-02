package formatter

import (
	"strconv"
	"time"

	"github.com/distribution/reference"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-units"
)

const (
	defaultImageTableFormat           = "table {{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{if .CreatedSince }}{{.CreatedSince}}{{else}}N/A{{end}}\t{{.Size}}"
	defaultImageTableFormatWithDigest = "table {{.Repository}}\t{{.Tag}}\t{{.Digest}}\t{{.ID}}\t{{if .CreatedSince }}{{.CreatedSince}}{{else}}N/A{{end}}\t{{.Size}}"

	imageIDHeader    = "IMAGE ID"
	repositoryHeader = "REPOSITORY"
	tagHeader        = "TAG"
	digestHeader     = "DIGEST"
)

// ImageContext contains image specific information required by the formatter, encapsulate a Context struct.
type ImageContext struct {
	Context
	Digest bool
}

func isDangling(img image.Summary) bool {
	if len(img.RepoTags) == 0 && len(img.RepoDigests) == 0 {
		return true
	}
	return len(img.RepoTags) == 1 && img.RepoTags[0] == "<none>:<none>" && len(img.RepoDigests) == 1 && img.RepoDigests[0] == "<none>@<none>"
}

// NewImageFormat returns a format for rendering an ImageContext
func NewImageFormat(source string, quiet bool, digest bool) Format {
	switch source {
	case TableFormatKey:
		switch {
		case quiet:
			return DefaultQuietFormat
		case digest:
			return defaultImageTableFormatWithDigest
		default:
			return defaultImageTableFormat
		}
	case RawFormatKey:
		switch {
		case quiet:
			return `image_id: {{.ID}}`
		case digest:
			return `repository: {{ .Repository }}
tag: {{.Tag}}
digest: {{.Digest}}
image_id: {{.ID}}
created_at: {{.CreatedAt}}
virtual_size: {{.Size}}
`
		default:
			return `repository: {{ .Repository }}
tag: {{.Tag}}
image_id: {{.ID}}
created_at: {{.CreatedAt}}
virtual_size: {{.Size}}
`
		}
	}

	format := Format(source)
	if format.IsTable() && digest && !format.Contains("{{.Digest}}") {
		format += "\t{{.Digest}}"
	}
	return format
}

// ImageWrite writes the formatter images using the ImageContext
func ImageWrite(ctx ImageContext, images []image.Summary) error {
	render := func(format func(subContext SubContext) error) error {
		return imageFormat(ctx, images, format)
	}
	return ctx.Write(newImageContext(), render)
}

// needDigest determines whether the image digest should be ignored or not when writing image context
func needDigest(ctx ImageContext) bool {
	return ctx.Digest || ctx.Format.Contains("{{.Digest}}")
}

func imageFormat(ctx ImageContext, images []image.Summary, format func(subContext SubContext) error) error {
	for _, img := range images {
		formatted := []*imageContext{}
		if isDangling(img) {
			formatted = append(formatted, &imageContext{
				trunc:  ctx.Trunc,
				i:      img,
				repo:   "<none>",
				tag:    "<none>",
				digest: "<none>",
			})
		} else {
			formatted = imageFormatTaggedAndDigest(ctx, img)
		}
		for _, imageCtx := range formatted {
			if err := format(imageCtx); err != nil {
				return err
			}
		}
	}
	return nil
}

func imageFormatTaggedAndDigest(ctx ImageContext, img image.Summary) []*imageContext {
	repoTags := map[string][]string{}
	repoDigests := map[string][]string{}
	images := []*imageContext{}

	for _, refString := range img.RepoTags {
		ref, err := reference.ParseNormalizedNamed(refString)
		if err != nil {
			continue
		}
		if nt, ok := ref.(reference.NamedTagged); ok {
			familiarRef := reference.FamiliarName(ref)
			repoTags[familiarRef] = append(repoTags[familiarRef], nt.Tag())
		}
	}
	for _, refString := range img.RepoDigests {
		ref, err := reference.ParseNormalizedNamed(refString)
		if err != nil {
			continue
		}
		if c, ok := ref.(reference.Canonical); ok {
			familiarRef := reference.FamiliarName(ref)
			repoDigests[familiarRef] = append(repoDigests[familiarRef], c.Digest().String())
		}
	}

	addImage := func(repo, tag, digest string) {
		images = append(images, &imageContext{
			trunc:  ctx.Trunc,
			i:      img,
			repo:   repo,
			tag:    tag,
			digest: digest,
		})
	}

	for repo, tags := range repoTags {
		digests := repoDigests[repo]

		// Do not display digests as their own row
		delete(repoDigests, repo)

		if !needDigest(ctx) {
			// Ignore digest references, just show tag once
			digests = nil
		}

		for _, tag := range tags {
			if len(digests) == 0 {
				addImage(repo, tag, "<none>")
				continue
			}
			// Display the digests for each tag
			for _, dgst := range digests {
				addImage(repo, tag, dgst)
			}
		}
	}

	// Show rows for remaining digest only references
	for repo, digests := range repoDigests {
		// If digests are displayed, show row per digest
		if ctx.Digest {
			for _, dgst := range digests {
				addImage(repo, "<none>", dgst)
			}
		} else {
			addImage(repo, "<none>", "")
		}
	}
	return images
}

type imageContext struct {
	HeaderContext
	trunc  bool
	i      image.Summary
	repo   string
	tag    string
	digest string
}

func newImageContext() *imageContext {
	imageCtx := imageContext{}
	imageCtx.Header = SubHeaderContext{
		"ID":           imageIDHeader,
		"Repository":   repositoryHeader,
		"Tag":          tagHeader,
		"Digest":       digestHeader,
		"CreatedSince": CreatedSinceHeader,
		"CreatedAt":    CreatedAtHeader,
		"Size":         SizeHeader,
		"Containers":   containersHeader,
		"VirtualSize":  SizeHeader, // Deprecated: VirtualSize is deprecated, and equivalent to Size.
		"SharedSize":   sharedSizeHeader,
		"UniqueSize":   uniqueSizeHeader,
	}
	return &imageCtx
}

func (c *imageContext) MarshalJSON() ([]byte, error) {
	return MarshalJSON(c)
}

func (c *imageContext) ID() string {
	if c.trunc {
		return TruncateID(c.i.ID)
	}
	return c.i.ID
}

func (c *imageContext) Repository() string {
	return c.repo
}

func (c *imageContext) Tag() string {
	return c.tag
}

func (c *imageContext) Digest() string {
	return c.digest
}

func (c *imageContext) CreatedSince() string {
	createdAt := time.Unix(c.i.Created, 0)

	if createdAt.IsZero() {
		return ""
	}

	return units.HumanDuration(time.Now().UTC().Sub(createdAt)) + " ago"
}

func (c *imageContext) CreatedAt() string {
	return time.Unix(c.i.Created, 0).String()
}

func (c *imageContext) Size() string {
	return units.HumanSizeWithPrecision(float64(c.i.Size), 3)
}

func (c *imageContext) Containers() string {
	if c.i.Containers == -1 {
		return "N/A"
	}
	return strconv.FormatInt(c.i.Containers, 10)
}

// VirtualSize shows the virtual size of the image and all of its parent
// images. Starting with docker 1.10, images are self-contained, and
// the VirtualSize is identical to Size.
//
// Deprecated: VirtualSize is deprecated, and equivalent to [imageContext.Size].
func (c *imageContext) VirtualSize() string {
	return units.HumanSize(float64(c.i.Size))
}

func (c *imageContext) SharedSize() string {
	if c.i.SharedSize == -1 {
		return "N/A"
	}
	return units.HumanSize(float64(c.i.SharedSize))
}

func (c *imageContext) UniqueSize() string {
	if c.i.Size == -1 || c.i.SharedSize == -1 {
		return "N/A"
	}
	return units.HumanSize(float64(c.i.Size - c.i.SharedSize))
}
