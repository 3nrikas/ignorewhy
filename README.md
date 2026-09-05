<p align="center">
  <img src="assets/mascot.png" width="220" alt="ignorewhy mascot">
</p>

<p align="center">
  <img src="assets/ignorewhy.png" height="64" alt="ignorewhy">
</p>

<p align="center">
  <strong>See why a file is ignored here, but shipped there.</strong>
</p>

`ignorewhy` compares how Git, Docker, and npm treat the same path. It shows
which tools include it, which exclude it, and where those boundaries disagree.

```text
$ ignorewhy .env.production

.env.production

Git
  IGNORED
  .gitignore:12 -> .env*

Docker
  INCLUDED
  no matching ignore rule

npm
  INCLUDED
  selected by npm pack

warning: ignored by Git but included in Docker build context and npm package
```

Adding a file to `.gitignore` only affects Git. It can still enter a Docker
build context or an npm package. `ignorewhy` makes that visible before the file
ships somewhere you did not expect.

<img src="assets/install.png" height="64" alt="Install">

```sh
go install github.com/3nrikas/ignorewhy@latest
```

Git must be available on `PATH`. npm is optional; when it is unavailable, the
Git and Docker checks still work.

<img src="assets/usage.png" height="64" alt="Usage">

Run it from anywhere inside a Git repository:

```sh
ignorewhy scan
ignorewhy scan --json
ignorewhy scan --ci
ignorewhy path/to/file
ignorewhy --help
ignorewhy --version
```

`scan --json` writes schema-versioned machine-readable output. `scan --ci`
returns exit code 3 when findings are present; operational errors return 1 and
invalid command usage returns 2. Plain `scan` remains informational and returns
success even when it finds mismatches.

<img src="assets/features.png" height="64" alt="Features">

- Explains Git ignore matches, including the source file and line number.
- Distinguishes tracked files from untracked files that Git ignores.
- Uses Moby-compatible `.dockerignore` matching, including negation and rule
  order.
- Supports `Dockerfile.dockerignore` for the default Dockerfile.
- Uses `npm pack` to follow real npm package inclusion rules.
- Disables npm lifecycle scripts and runs npm analysis offline.
- Scans a repository for cross-context mismatches in one command.
- Provides deterministic JSON output and a CI failure mode.
- Detects files ignored by Git but included by Docker or npm.
- Works locally without a Docker daemon, account, or telemetry.

<img src="assets/hid.png" height="64" alt="How it decides">

| Context | Source of truth |
| --- | --- |
| Git | `git check-ignore` and `git ls-files` |
| Docker | Moby's `patternmatcher` |
| npm | `npm pack --dry-run --ignore-scripts --offline` |

The Git repository root is used as the Docker build context and as the root npm
package. npm workspaces and custom Docker build contexts are not supported yet.

<img src="assets/license.png" height="64" alt="License">

[MIT](LICENSE)
