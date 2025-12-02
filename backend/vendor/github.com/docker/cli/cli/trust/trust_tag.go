package trust

import (
	"context"
	"fmt"
	"io"

	"github.com/distribution/reference"
	"github.com/docker/docker/client"
)

// TagTrusted tags a trusted ref. It is a shallow wrapper around [client.Client.ImageTag]
// that updates the given image references to their familiar format for tagging
// and printing.
func TagTrusted(ctx context.Context, apiClient client.ImageAPIClient, out io.Writer, trustedRef reference.Canonical, ref reference.NamedTagged) error {
	// Use familiar references when interacting with client and output
	familiarRef := reference.FamiliarString(ref)
	trustedFamiliarRef := reference.FamiliarString(trustedRef)

	_, _ = fmt.Fprintf(out, "Tagging %s as %s\n", trustedFamiliarRef, familiarRef)
	return apiClient.ImageTag(ctx, trustedFamiliarRef, familiarRef)
}
