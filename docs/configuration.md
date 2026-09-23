# Configuration reference

gignore works without configuration. This page lists every setting.

## Where settings come from

gignore reads settings in this order, and later sources override earlier ones:

| Order | Source | Location |
| --- | --- | --- |
| 1 | Defaults | Built into gignore |
| 2 | User config | `$XDG_CONFIG_HOME/gignore/config.toml`, usually `~/.config/gignore/config.toml`. On Windows, `%AppData%\gignore\config.toml`. |
| 3 | Project config | `.gignore.toml` in the current directory or a parent, up to the repository root |
| 4 | Environment variables | See [Environment variables](#environment-variables) |
| 5 | Command-line flags | For example `--offline` |

Tables such as `[presets]` merge across files. Lists such as `generate.always` replace the earlier value.

A misspelled or unknown setting is an error, so typos don't get silently ignored. To see the effective configuration and the files it came from, run `gignore config show`.

gignore never reads a `.gignore.toml` above the repository root, so a file in your home directory doesn't affect every repository. For settings that apply everywhere, use the user config.

## `[online]`

Controls the gitignore.io catalog. Built-in templates work regardless of these settings.

| Setting | Default | Description |
| --- | --- | --- |
| `enabled` | `true` | Load online templates. When `false`, gignore still uses a previously cached catalog. |
| `url` | `"https://www.toptal.com/developers/gitignore/api"` | API base URL. Point it at a self-hosted mirror if you have one. |
| `cache_ttl` | `"168h"` | How long a downloaded catalog counts as fresh. Uses Go duration syntax, such as `30m`, `12h`, or `168h`. |
| `timeout` | `"15s"` | Timeout for each request. |
| `retries` | `2` | Extra attempts after a failed request. Only network errors, HTTP 429, and HTTP 5xx responses are retried. |
| `prefer` | `"builtin"` | Which copy wins when a template exists in both sources: `"builtin"` or `"online"`. |

When a refresh fails, gignore falls back to the cached catalog, even an expired one, and shows a warning.

## `[generate]`

| Setting | Default | Description |
| --- | --- | --- |
| `output` | `".gitignore"` | File to write. A relative path is resolved from the repository root. |
| `dedupe` | `true` | Drop rules that an earlier template already added. |
| `always` | `[]` | Templates added to every new block, such as your OS and editor. |
| `templates` | `[]` | Default selection for a project. Usually set in `.gignore.toml`. |
| `detect_depth` | `2` | How many directory levels below the project root detection scans. Accepts `0` to `6`. |

## `[tui]`

| Setting | Default | Description |
| --- | --- | --- |
| `theme` | `"auto"` | `auto` follows the terminal background. The other values are `dark`, `light`, and `mono`. |
| `mouse` | `true` | Enable clicking and scrolling. Turn it off to use your terminal's own text selection. |
| `detect` | `true` | In a project without a gignore block, preselect detected templates. |
| `search_mode` | `"fuzzy"` | Initial search mode: `fuzzy`, `exact`, or `regex`. |
| `suggest_os` | `true` | Include the template for your operating system in detection. |

## `[presets]`

Named lists of templates. To use a preset, put `@` before its name anywhere you can name a template:

```toml
[presets]
web = ["node", "macos", "visualstudiocode"]
backend = ["go", "@infra"]
infra = ["terraform", "helm"]
```

```sh
gignore generate @web -w
```

## `[aliases]`

Extra shorthand names:

```toml
[aliases]
tf = "terraform"
```

Built-in aliases:

| Alias | Template |
| --- | --- |
| `js`, `javascript`, `nodejs`, `ts`, `typescript` | `node` |
| `py` | `python` |
| `golang` | `go` |
| `rs` | `rust` |
| `vscode` | `visualstudiocode` |
| `vs`, `dotnet`, `csharp`, `c#` | `visualstudio` |
| `idea`, `intellij` | `jetbrains` |
| `mac`, `osx` | `macos` |
| `win` | `windows` |

A template with the same name as an alias always wins over the alias.

## Environment variables

| Variable | Effect |
| --- | --- |
| `GIGNORE_CONFIG` | Path of the user config file |
| `GIGNORE_OFFLINE=1` | Same as `online.enabled = false` |
| `GIGNORE_API_URL` | Overrides `online.url` |
| `GIGNORE_THEME` | Overrides `tui.theme` |
| `GIGNORE_OUTPUT` | Overrides `generate.output` |
| `GIGNORE_CONFIG_DIR` | Directory for `config.toml` and `templates/` |
| `GIGNORE_CACHE_DIR` | Directory for the online catalog cache |
| `GIGNORE_STATE_DIR` | Directory for usage history, which ranks recent templates in the picker |
| `NO_COLOR` | Disables colored output |

## Files gignore uses

| Path | Contents |
| --- | --- |
| `~/.config/gignore/config.toml` | User settings |
| `~/.config/gignore/templates/*.gitignore` | Your own templates |
| `~/.cache/gignore/catalog.json` | Cached gitignore.io catalog |
| `~/.local/state/gignore/usage.json` | How often you pick each template |

On Windows, these files live under `%AppData%\gignore` and `%LocalAppData%\gignore`.
