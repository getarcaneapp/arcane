# Nested compose file discovery troubleshooting

Arcane discovers projects by scanning the configured projects directory for compose files. Use this checklist when a compose file in a nested folder does not appear in the Projects list or does not update after you edit it.

## Expected project layout

A project is a directory that contains one supported compose file. Nested projects are supported when they are inside the configured projects directory and within the scan depth.

```text
/app/data/projects/
├── media/
│   └── compose.yaml
└── homelab/
    └── monitoring/
        └── docker-compose.yml
```

Supported default compose filenames are `compose.yaml`, `compose.yml`, `docker-compose.yaml`, `docker-compose.yml`, `podman-compose.yaml`, and `podman-compose.yml`. Arcane can also detect a single custom `.yaml` or `.yml` file when it has compose root keys, but using one of the default names avoids ambiguity. Avoid keeping two custom compose-looking YAML files in the same project directory because Arcane may refuse to choose between them.

## Scan depth and directories

`PROJECTS_DIRECTORY` controls the root Arcane scans. In the official container this defaults to `/app/data/projects`, so host paths must be mounted into that location or configured to match the mount target. `PROJECT_SCAN_MAX_DEPTH` controls how many directory levels under `PROJECTS_DIRECTORY` Arcane scans and watches. The default is `3`; increase it if your compose file is deeper than that.

Examples:

- `/app/data/projects/blog/compose.yaml` is depth 1.
- `/app/data/projects/homelab/monitoring/docker-compose.yml` is depth 2.
- `/app/data/projects/a/b/c/d/compose.yaml` is depth 4 and needs `PROJECT_SCAN_MAX_DEPTH=4` or higher.

After changing `PROJECTS_DIRECTORY`, `PROJECT_SCAN_MAX_DEPTH`, or `FOLLOW_PROJECT_SYMLINKS`, restart Arcane so the filesystem watcher is recreated with the new settings. If the UI still shows stale data after a move or rename, restart Arcane or trigger a project rescan by touching the compose file inside the configured directory.

## Symlinks and mounts

Arcane only follows symlinked project directories when `FOLLOW_PROJECT_SYMLINKS` is enabled in settings/configuration. If the nested compose path is a symlink target, enable that option and restart Arcane. If Arcane runs in Docker, verify the path you see on the host is also mounted at the same effective location inside the Arcane container; a compose file that exists only on the host cannot be discovered from inside the container.

Keep local templates outside `PROJECTS_DIRECTORY`. Arcane disables the templates watcher when the templates directory overlaps the projects directory to avoid treating templates as projects, and overlapping paths can make discovery confusing.

## Quick diagnostics

Run these checks from the host running Arcane:

```bash
docker exec <arcane-container> printenv PROJECTS_DIRECTORY PROJECT_SCAN_MAX_DEPTH FOLLOW_PROJECT_SYMLINKS
docker exec <arcane-container> sh -lc 'find "$PROJECTS_DIRECTORY" -maxdepth "${PROJECT_SCAN_MAX_DEPTH:-3}" -type f \( -name compose.yaml -o -name compose.yml -o -name docker-compose.yaml -o -name docker-compose.yml -o -name podman-compose.yaml -o -name podman-compose.yml \) -print'
docker logs <arcane-container> | grep -Ei 'filesystem watcher|sync projects|project sync|compose|inotify'
```

When opening an issue, include the Arcane version, install method, the `PROJECTS_DIRECTORY` mount/configuration, `PROJECT_SCAN_MAX_DEPTH`, whether `FOLLOW_PROJECT_SYMLINKS` is enabled, the relative path from `PROJECTS_DIRECTORY` to the missing compose file, the sanitized compose filename/content, and relevant logs from the filesystem watcher or project sync. This information is especially useful for nested compose detection reports such as #3080.
