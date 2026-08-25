package backups

import (
	"context"
	"fmt"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/backup"
)

const maxPromptOptions = 20

// resolveS3Destination resolves an S3 destination identifier (ID or name) to
// the saved destination: exact ID first, then exact name (case-insensitive),
// then substring match. Multiple matches prompt interactively when allowed.
func resolveS3Destination(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (*backup.S3Destination, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return nil, errors.New("S3 destination name or ID is required")
	}

	resp, err := c.Get(ctx, types.BackupsS3Options())
	if err != nil {
		return nil, errors.WrapIf(err, "failed to list S3 destinations")
	}
	defer func() { _ = resp.Body.Close() }()
	if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
		return nil, errors.WrapIf(err, "failed to list S3 destinations")
	}

	var destinations []backup.S3Destination
	if err := cmdutil.DecodeJSON(resp, &destinations); err != nil {
		return nil, err
	}

	for i := range destinations {
		if destinations[i].ID == trimmed {
			return &destinations[i], nil
		}
	}

	matches := make([]backup.S3Destination, 0)
	for _, dest := range destinations {
		if strings.EqualFold(dest.Name, trimmed) {
			matches = append(matches, dest)
		}
	}
	if len(matches) == 0 {
		identifierLower := strings.ToLower(trimmed)
		for _, dest := range destinations {
			if strings.Contains(strings.ToLower(dest.Name), identifierLower) {
				matches = append(matches, dest)
			}
		}
	}

	if len(matches) == 1 {
		return &matches[0], nil
	}

	if len(matches) > 1 {
		if allowPrompt && prompt.IsInteractive() && len(matches) <= maxPromptOptions {
			options := make([]string, 0, len(matches))
			for _, match := range matches {
				options = append(options, fmt.Sprintf("%s (%s, bucket %s)", match.Name, match.ID, match.Bucket))
			}
			choice, err := prompt.Select("S3 destination", options)
			if err != nil {
				return nil, err
			}
			return &matches[choice], nil
		}
		names := make([]string, 0, len(matches))
		for _, match := range matches {
			names = append(names, fmt.Sprintf("%s (%s)", match.Name, match.ID))
		}
		return nil, errors.Errorf("multiple S3 destinations match %q: %s; use the exact name or ID", trimmed, strings.Join(names, ", "))
	}

	return nil, errors.Errorf("S3 destination %q not found; run `arcane backups s3 list`", trimmed)
}
