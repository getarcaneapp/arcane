[default]
_list:
    @{{ just_executable() }} --list

# Run development servers: frontend, backend, agent, all, docker, logs
[group('development')]
mod dev 'just/dev.just'

# Build application code: single {frontend|backend|all}; containers: image {manager|agent} [tag] [--push]
[group('build')]
mod build 'just/build.just'

# Run tests: e2e, backend, cli, all
[group('checks')]
mod test 'just/test.just'

# Format: frontend, tests, email, js, go, just, all. Add --check to verify.
[group('checks')]
mod format 'just/format.just'

# Lint: frontend, tests, email, js, backend, cli, types, go, proto, all
[group('checks')]
mod lint 'just/lint.just'

# Apply go fix: backend, cli, types, all
[group('checks')]
mod fix 'just/fix.just'

# Snyk: scan
[group('checks')]
mod snyk 'just/snyk.just'

# Manage dependencies: install <target>, update <target> [--patch]
[group('dependencies')]
mod deps 'just/deps.just'

# Generate code: proto
[group('generation')]
mod generate 'just/generate.just'

# Generate docs: config [output] [source_root]
[group('generation')]
mod docs 'just/docs.just'

# Localization: add <locale> <native_name>. Example: just i18n add es "Español"
[group('generation')]
mod i18n 'just/i18n.just'

# Benchmark agent tunnel transports: tunnel [count] [benchtime], memory [profile] [benchtime]
[group('performance')]
mod bench 'just/bench.just'

# Release tooling: version [github-output] [next|beta]
[group('release')]
mod release 'just/release.just'

# Repository maintenance: clean
[group('maintenance')]
mod repo 'just/repo.just'

# Maintainer reports: feats, fixes
[group('maintenance')]
mod utils 'just/utils.just'
