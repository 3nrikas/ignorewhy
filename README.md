<p align="center">
  <img src="assets/ignorewhy.png" width="220" alt="ignorewhy">
</p>

<h1 align="center">ignorewhy</h1>

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

## Install

```sh
go install github.com/3nrikas/ignorewhy@latest
```

Git must be available on `PATH`. npm is optional; when it is unavailable, the
Git and Docker checks still work.

## Usage

Run it from anywhere inside a Git repository:

```sh
ignorewhy path/to/file
ignorewhy --help
ignorewhy --version
```

## Features

- Explains Git ignore matches, including the source file and line number.
- Distinguishes tracked files from untracked files that Git ignores.
- Uses Moby-compatible `.dockerignore` matching, including negation and rule
  order.
- Supports `Dockerfile.dockerignore` for the default Dockerfile.
- Uses `npm pack` to follow real npm package inclusion rules.
- Disables npm lifecycle scripts and runs npm analysis offline.
- Detects files ignored by Git but included by Docker or npm.
- Works locally without a Docker daemon, account, or telemetry.

## How it decides

| Context | Source of truth |
| --- | --- |
| Git | `git check-ignore` and `git ls-files` |
| Docker | Moby's `patternmatcher` |
| npm | `npm pack --dry-run --ignore-scripts --offline` |

The current version analyzes one path at a time. The Git repository root is
used as the Docker build context and as the root npm package. npm workspaces and
custom Docker build contexts are not supported yet.

## License

[MIT](LICENSE)
