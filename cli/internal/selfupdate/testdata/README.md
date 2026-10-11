Fixtures are immutable release checksum files and Sigstore bundles downloaded from:

- https://github.com/getarcaneapp/arcane/releases/download/v1.18.0/
- https://github.com/getarcaneapp/arcane/releases/download/v2.15.1/

The pinned public-good trust root came from TUF timestamp/snapshot/targets metadata:

https://tuf-repo-cdn.sigstore.dev/targets/6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66.trusted_root.json

SHA-256: `6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66`

Tests use these local files without network access.
