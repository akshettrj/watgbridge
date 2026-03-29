# Continuous integration

## Workflows

| File | Purpose |
|------|---------|
| [workflows/build.yml](workflows/build.yml) | On push to `main` when `state/version.txt` changes (or manual dispatch): builds Linux release binaries and attaches them to a GitHub Release. |
| [workflows/cache_nix.yml](workflows/cache_nix.yml) | On changes to Go/Nix inputs: builds the Nix flake package `packages.x86_64-linux.watgbridge` and pushes to Cachix (`watgbridge`). |
| [release.yml](release.yml) | Release automation config used with GitHub Releases (see repo layout). |

## CGO and SQLCipher

This project links **SQLCipher** via `github.com/mutecomm/go-sqlcipher/v4`. Builds **require CGO** and a **C toolchain** (`gcc` / `g++`).

- **Local / generic Linux**: use `CGO_ENABLED=1 go build` (default on many platforms when `gcc` is installed).
- **CI amd64** ([build.yml](workflows/build.yml) `build-amd64`): installs `gcc`; the build step sets `CGO_ENABLED=1` explicitly.
- **CI arm64 cross-compile** (`build-arm64`): sets `CGO_ENABLED=1` with `CC=aarch64-linux-gnu-gcc` and `CXX=aarch64-linux-gnu-g++` so cgo targets `linux/arm64` from the Ubuntu runner.
- **Docker (Alpine)**: the [Dockerfile](../Dockerfile) sets `CGO_CFLAGS` for musl (`_GNU_SOURCE`, `_LARGEFILE64_SOURCE`) so the bundled SQLite/SQLCipher sources compile.

If a job omits `gcc` or disables CGO, the build will fail at the SQLCipher/sqlite cgo step.

## Nix

The flake build should pull in native build inputs needed for CGO; if the Nix package fails after dependency changes, check `flake.nix` / `nix/packages` for `pkg-config`, compiler, and any SQLCipher-related flags.
