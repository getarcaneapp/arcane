// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package image

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/containerd/platforms"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
	"github.com/docker/cli/internal/tui"
	"github.com/docker/docker/api/types/filters"
	imagetypes "github.com/docker/docker/api/types/image"
	"github.com/docker/go-units"
	"github.com/morikuni/aec"
	"github.com/opencontainers/go-digest"
)

type treeOptions struct {
	all     bool
	filters filters.Args
}

type treeView struct {
	images []topImage

	// imageSpacing indicates whether there should be extra spacing between images.
	imageSpacing bool
}

func runTree(ctx context.Context, dockerCLI command.Cli, opts treeOptions) error {
	images, err := dockerCLI.Client().ImageList(ctx, imagetypes.ListOptions{
		All:       opts.all,
		Filters:   opts.filters,
		Manifests: true,
	})
	if err != nil {
		return err
	}
	if !opts.all {
		images = slices.DeleteFunc(images, isDangling)
	}

	view := treeView{
		images: make([]topImage, 0, len(images)),
	}
	attested := make(map[digest.Digest]bool)

	for _, img := range images {
		details := imageDetails{
			ID:        img.ID,
			DiskUsage: units.HumanSizeWithPrecision(float64(img.Size), 3),
			InUse:     img.Containers > 0,
		}

		var totalContent int64
		children := make([]subImage, 0, len(img.Manifests))
		for _, im := range img.Manifests {
			totalContent += im.Size.Content

			if im.Kind == imagetypes.ManifestKindAttestation {
				attested[im.AttestationData.For] = true
				continue
			}
			if im.Kind != imagetypes.ManifestKindImage {
				continue
			}

			sub := subImage{
				Platform:  platforms.Format(im.ImageData.Platform),
				Available: im.Available,
				Details: imageDetails{
					ID:          im.ID,
					DiskUsage:   units.HumanSizeWithPrecision(float64(im.Size.Total), 3),
					InUse:       len(im.ImageData.Containers) > 0,
					ContentSize: units.HumanSizeWithPrecision(float64(im.Size.Content), 3),
				},
			}

			if sub.Details.InUse {
				// Mark top-level parent image as used if any of its subimages are used.
				details.InUse = true
			}

			children = append(children, sub)

			// Add extra spacing between images if there's at least one entry with children.
			view.imageSpacing = true
		}

		details.ContentSize = units.HumanSizeWithPrecision(float64(totalContent), 3)

		view.images = append(view.images, topImage{
			Names:    img.RepoTags,
			Details:  details,
			Children: children,
			created:  img.Created,
		})
	}

	sort.Slice(view.images, func(i, j int) bool {
		return view.images[i].created > view.images[j].created
	})

	return printImageTree(dockerCLI, view)
}

type imageDetails struct {
	ID          string
	DiskUsage   string
	InUse       bool
	ContentSize string
}

type topImage struct {
	Names    []string
	Details  imageDetails
	Children []subImage

	created int64
}

type subImage struct {
	Platform  string
	Available bool
	Details   imageDetails
}

const columnSpacing = 3

var chipInUse = imageChip{
	letter: "U",
	desc:   "In Use",
	fg:     0,
	bg:     14,
	check:  func(d *imageDetails) bool { return d.InUse },
}

var chipPlaceholder = tui.Str{
	Plain: " ",
	Fancy: "   ",
}

type imageChip struct {
	desc   string
	fg, bg int
	letter string
	check  func(*imageDetails) bool
}

func (c imageChip) String(isTerm bool) string {
	return tui.Str{
		Plain: c.letter,
		Fancy: tui.Chip(c.fg, c.bg, " "+c.letter+" "),
	}.String(isTerm)
}

var allChips = []imageChip{
	chipInUse,
}

func getPossibleChips(view treeView) (chips []imageChip) {
	remaining := make([]imageChip, len(allChips))
	copy(remaining, allChips)

	var possible []imageChip
	for _, img := range view.images {
		details := []imageDetails{img.Details}

		for _, c := range img.Children {
			details = append(details, c.Details)
		}

		for _, d := range details {
			for idx := len(remaining) - 1; idx >= 0; idx-- {
				chip := remaining[idx]
				if chip.check(&d) {
					possible = append(possible, chip)
					remaining = append(remaining[:idx], remaining[idx+1:]...)
				}
			}
		}
	}

	return possible
}

func printImageTree(dockerCLI command.Cli, view treeView) error {
	out := tui.NewOutput(dockerCLI.Out())
	_, width := out.GetTtySize()
	if width == 0 {
		width = 80
	}
	if width < 20 {
		width = 20
	}

	topNameColor := out.Color(aec.NewBuilder(aec.BlueF, aec.Bold).ANSI)
	normalColor := out.Color(tui.ColorSecondary)
	untaggedColor := out.Color(tui.ColorTertiary)
	isTerm := out.IsTerminal()

	out.PrintlnWithColor(tui.ColorWarning, "WARNING: This is an experimental feature. The output may change and shouldn't be depended on.")

	out.Println(generateLegend(out, width))
	out.Println()

	possibleChips := getPossibleChips(view)
	columns := []imgColumn{
		{
			Title: "Image",
			Align: alignLeft,
			Width: 0,
		},
		{
			Title: "ID",
			Align: alignLeft,
			Width: 12,
			DetailsValue: func(d *imageDetails) string {
				return formatter.TruncateID(d.ID)
			},
		},
		{
			Title: "Disk usage",
			Align: alignRight,
			Width: 10,
			DetailsValue: func(d *imageDetails) string {
				return d.DiskUsage
			},
		},
		{
			Title: "Content size",
			Align: alignRight,
			Width: 12,
			DetailsValue: func(d *imageDetails) string {
				return d.ContentSize
			},
		},
		{
			Title: "Extra",
			Align: alignLeft,
			Width: func() int {
				maxChipsWidth := 0
				for _, chip := range possibleChips {
					s := chip.String(isTerm)
					l := tui.Width(s)
					maxChipsWidth += l
				}

				le := len("Extra")
				if le > maxChipsWidth {
					return le
				}
				return maxChipsWidth
			}(),
			Color: &tui.ColorNone,
			DetailsValue: func(d *imageDetails) string {
				var out string
				for _, chip := range possibleChips {
					if chip.check(d) {
						out += chip.String(isTerm)
					} else {
						out += chipPlaceholder.String(isTerm)
					}
				}
				return out
			},
		},
	}

	columns = adjustColumns(width, columns, view.images)

	// Print columns
	for i, h := range columns {
		if i > 0 {
			_, _ = fmt.Fprint(out, strings.Repeat(" ", columnSpacing))
		}

		_, _ = fmt.Fprint(out, h.Print(tui.ColorTitle, strings.ToUpper(h.Title)))
	}
	_, _ = fmt.Fprintln(out)

	// Print images
	for _, img := range view.images {
		printNames(out, columns, img, topNameColor, untaggedColor)
		printDetails(out, columns, normalColor, img.Details)

		if len(img.Children) > 0 || view.imageSpacing {
			_, _ = fmt.Fprintln(out)
		}
		printChildren(out, columns, img, normalColor)
		_, _ = fmt.Fprintln(out)
	}

	return nil
}

// adjustColumns adjusts the width of the first column to maximize the space
// available for image names and removes any columns that would be too narrow
// to display their content.
func adjustColumns(width uint, columns []imgColumn, images []topImage) []imgColumn {
	nameWidth := int(width)
	for idx, h := range columns {
		if h.Width == 0 {
			continue
		}
		d := h.Width
		if idx > 0 {
			d += columnSpacing
		}
		// If the first column gets too short, remove remaining columns
		if nameWidth-d < 12 {
			columns = columns[:idx]
			break
		}
		nameWidth -= d
	}

	// Try to make the first column as narrow as possible
	widest := widestFirstColumnValue(columns, images)
	if nameWidth > widest {
		nameWidth = widest
	}
	columns[0].Width = nameWidth
	return columns
}

func generateLegend(out tui.Output, width uint) string {
	var legend string
	legend += out.Sprint(tui.InfoHeader)
	for idx, chip := range allChips {
		legend += " " + out.Sprint(chip) + " " + chip.desc
		if idx < len(allChips)-1 {
			legend += " |"
		}
	}
	legend += " "

	r := int(width) - tui.Width(legend)
	if r < 0 {
		r = 0
	}
	legend = strings.Repeat(" ", r) + legend
	return legend
}

func printDetails(out tui.Output, headers []imgColumn, defaultColor aec.ANSI, details imageDetails) {
	for _, h := range headers {
		if h.DetailsValue == nil {
			continue
		}

		_, _ = fmt.Fprint(out, strings.Repeat(" ", columnSpacing))
		clr := defaultColor
		if h.Color != nil {
			clr = *h.Color
		}
		val := h.DetailsValue(&details)
		_, _ = fmt.Fprint(out, h.Print(clr, val))
	}
}

func printChildren(out tui.Output, headers []imgColumn, img topImage, normalColor aec.ANSI) {
	for idx, sub := range img.Children {
		clr := normalColor
		if !sub.Available {
			clr = normalColor.With(aec.Faint)
		}

		text := sub.Platform
		if idx != len(img.Children)-1 {
			_, _ = fmt.Fprint(out, headers[0].Print(clr, "├─ "+text))
		} else {
			_, _ = fmt.Fprint(out, headers[0].Print(clr, "└─ "+text))
		}

		printDetails(out, headers, clr, sub.Details)
		_, _ = fmt.Fprintln(out, "")
	}
}

func printNames(out tui.Output, headers []imgColumn, img topImage, color, untaggedColor aec.ANSI) {
	if len(img.Names) == 0 {
		_, _ = fmt.Fprint(out, headers[0].Print(untaggedColor, "<untagged>"))
	}

	// TODO: Replace with namesLongestToShortest := slices.SortedFunc(slices.Values(img.Names))
	// once we move to Go 1.23.
	namesLongestToShortest := make([]string, len(img.Names))
	copy(namesLongestToShortest, img.Names)
	sort.Slice(namesLongestToShortest, func(i, j int) bool {
		return len(namesLongestToShortest[i]) > len(namesLongestToShortest[j])
	})

	for nameIdx, name := range namesLongestToShortest {
		// Don't limit first names to the column width because only the last
		// name will be printed alongside other columns.
		if nameIdx < len(img.Names)-1 {
			_, fullWidth := out.GetTtySize()
			_, _ = fmt.Fprintln(out, color.Apply(tui.Ellipsis(name, int(fullWidth))))
		} else {
			_, _ = fmt.Fprint(out, headers[0].Print(color, name))
		}
	}
}

type alignment int

const (
	alignLeft alignment = iota
	alignCenter
	alignRight
)

type imgColumn struct {
	Title string
	Width int
	Align alignment

	DetailsValue func(*imageDetails) string
	Color        *aec.ANSI
}

func (h imgColumn) Print(clr aec.ANSI, s string) string {
	switch h.Align {
	case alignCenter:
		return h.PrintC(clr, s)
	case alignRight:
		return h.PrintR(clr, s)
	case alignLeft:
	}
	return h.PrintL(clr, s)
}

func (h imgColumn) PrintC(clr aec.ANSI, s string) string {
	ln := tui.Width(s)

	if ln > h.Width {
		return clr.Apply(tui.Ellipsis(s, h.Width))
	}

	fill := h.Width - ln

	l := fill / 2
	r := fill - l

	return strings.Repeat(" ", l) + clr.Apply(s) + strings.Repeat(" ", r)
}

func (h imgColumn) PrintL(clr aec.ANSI, s string) string {
	ln := tui.Width(s)
	if ln > h.Width {
		return clr.Apply(tui.Ellipsis(s, h.Width))
	}

	return clr.Apply(s) + strings.Repeat(" ", h.Width-ln)
}

func (h imgColumn) PrintR(clr aec.ANSI, s string) string {
	ln := tui.Width(s)
	if ln > h.Width {
		return clr.Apply(tui.Ellipsis(s, h.Width))
	}

	return strings.Repeat(" ", h.Width-ln) + clr.Apply(s)
}

// widestFirstColumnValue calculates the width needed to fully display the image names and platforms.
func widestFirstColumnValue(headers []imgColumn, images []topImage) int {
	width := len(headers[0].Title)
	for _, img := range images {
		for _, name := range img.Names {
			if len(name) > width {
				width = len(name)
			}
		}
		for _, sub := range img.Children {
			pl := len(sub.Platform) + len("└─ ")
			if pl > width {
				width = pl
			}
		}
	}
	return width
}
