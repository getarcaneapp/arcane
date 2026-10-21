package backups

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// s3DestinationRef resolves exact IDs, then case-insensitive names and substrings.
var s3DestinationRef = cmdutil.ResourceRef[types.S3Destination, types.S3Destination]{
	Singular: "S3 destination",
	Plural:   "S3 destinations",
	IDHint:   "the destination ID",
	ListCmd:  "arcane admin backups s3 list",
	GetPath:  func(_, identifier string) string { return clitypes.BackupsS3Destination(identifier) },
	SearchCandidates: func(ctx context.Context, c *client.Client, identifier string) ([]types.S3Destination, error) {
		destinations, err := c.DoJSON[[]types.S3Destination](ctx, http.MethodGet, clitypes.BackupsS3Options(), nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list S3 destinations: %w", err)
		}
		matches := make([]types.S3Destination, 0)
		for _, dest := range destinations {
			if strings.EqualFold(dest.Name, identifier) {
				matches = append(matches, dest)
			}
		}
		if len(matches) == 0 {
			identifierLower := strings.ToLower(identifier)
			for _, dest := range destinations {
				if strings.Contains(strings.ToLower(dest.Name), identifierLower) {
					matches = append(matches, dest)
				}
			}
		}
		return matches, nil
	},
	Label: func(dest types.S3Destination) string {
		return fmt.Sprintf("%s (%s, bucket %s)", dest.Name, dest.ID, dest.Bucket)
	},
	Promote: func(dest types.S3Destination) *types.S3Destination { return &dest },
}
