# Contributing

Bug reports, template detection rules, and pull requests are welcome.

## Set up

1. Install Go 1.25 or later.
2. Clone the repository and run the tests:

   ```sh
   make test
   ```

3. Optional: install [golangci-lint](https://golangci-lint.run/) and run `make lint`.

## Project layout

| Path | Contents |
| --- | --- |
| `cmd/gignore` | Entry point |
| `internal/cli` | Commands and flags |
| `internal/tui` | Interactive picker |
| `internal/engine` | Shared logic that turns a selection into file content |
| `internal/catalog` | Template sources: built-in, gitignore.io, and local files |
| `internal/block` | Reading and writing the managed block |
| `internal/check` | `gignore check` rules |
| `internal/detect` | Project detection rules |
| `tools/synctemplates` | Refreshes the built-in templates |

## Common changes

- **Detect another project type:** add a rule to `rules` in `internal/detect/detect.go`, and a case to `detect_test.go`.
- **Add a check:** add it to `internal/check/check.go` with a stable `code`, and list it in the `gignore check` help text in `internal/cli/inspect.go`.
- **Refresh built-in templates:** run `make sync-templates`. A weekly workflow also does this automatically.

Don't edit files under `internal/catalog/builtin/data` by hand. They are overwritten on every sync. Fix templates upstream in [github/gitignore](https://github.com/github/gitignore).

## Pull requests

- Keep the marker lines in `internal/block` stable. Changing them breaks every existing `.gitignore` that gignore manages.
- Add or update tests for behavior changes.
- Use [Conventional Commits](https://www.conventionalcommits.org/) prefixes such as `feat:` and `fix:`. The release notes are grouped by these prefixes.

## Releases

To publish a release, push a tag such as `v1.2.0`. The release workflow runs GoReleaser, which publishes archives, Linux packages, the Homebrew cask, and the Scoop manifest. It needs a `TAP_GITHUB_TOKEN` secret with write access to `kwmx/homebrew-tap` and `kwmx/scoop-bucket`.

To build every artifact locally without publishing, run `make snapshot`.
