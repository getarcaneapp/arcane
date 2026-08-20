package backups

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/backup"
)

// s3DestinationRef resolves an S3 destination by ID or name: exact ID via the
// GET endpoint, then exact name (case-insensitive), then substring match over
// the saved destinations.
var s3DestinationRef = cmdutil.ResourceRef[backup.S3Destination, backup.S3Destination]{
	Singular: "S3 destination",
	Plural:   "S3 destinations",
	IDHint:   "the destination ID",
	ListCmd:  "arcane backups s3 list",
	GetPath:  func(_, identifier string) string { return types.BackupsS3Destination(identifier) },
	SearchCandidates: func(ctx context.Context, c *client.Client, identifier string) ([]backup.S3Destination, error) {
		destinations, err := c.DoJSON[[]backup.S3Destination](ctx, http.MethodGet, types.BackupsS3Options(), nil)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to list S3 destinations")
		}
		matches := make([]backup.S3Destination, 0)
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
	Label: func(dest backup.S3Destination) string {
		return fmt.Sprintf("%s (%s, bucket %s)", dest.Name, dest.ID, dest.Bucket)
	},
	Promote: func(dest backup.S3Destination) *backup.S3Destination { return &dest },
}
