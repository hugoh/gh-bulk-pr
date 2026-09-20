# gh-bulk-pr

A [`gh`](https://cli.github.com) extension for filtering, multi-selecting, and bulk-editing pull requests from the terminal.

## Install

```sh
gh extension install hugoh/gh-bulk-pr
```

## Usage

```sh
gh bulk-pr
gh bulk-pr --query "is:open is:pr author:@me archived:false"
```

| Flag | Default | Description |
| --- | --- | --- |
| `--mouse` | `false` | scroll with the mouse wheel (the terminal then needs shift to select text) |
| `--query` | `is:open is:pr archived:false sort:updated-desc owner:@me` | GitHub search query for the PR list |
| `--version` | `false` | print version and exit |

## Keys

Press `?` in the app for this list.

| Key | Action |
| --- | --- |
| `↑/k` | up |
| `↓/j` | down |
| `g/home` | top |
| `G/end` | bottom |
| `pgup/b` | page up |
| `pgdn/f` | page down |
| `x/space` | select |
| `ctrl+a` | select all |
| `esc` | close/clear |
| `enter/p` | preview |
| `/` | filter |
| `s` | open/closed |
| `1/2` | tab |
| `r` | refresh |
| `T` | checks |
| `o` | open |
| `O` | open all |
| `l` | label |
| `c` | close |
| `m` | merge |
| `a` | auto |
| `?` | help |
| `q` | quit |

`a` toggles squash auto-merge on each selected PR: it enables it where it is off and disables it where it is on. PRs with auto-merge enabled show `on` in the Auto column. A PR that is already ready to merge has nothing to wait for (GitHub refuses auto-merge on it), so `a` squash-merges it right away; the confirm screen marks those PRs `merges now` and needs an explicit `y`. Repos with auto-merge turned off fail for the PRs that need it.

`ctrl+c` quits at once from any screen; `q` needs a second press while PRs are selected. `O` asks for confirmation above 5 PRs and opens at most 20.

Tabs (`1`/`2`): `owner:@me`, `involves:@me`. All share the base query `is:open is:pr archived:false sort:updated-desc`. Editing the query with `/` deselects the tab unless it still matches one.

Queries run from the filter bar or a tab are appended to `$XDG_STATE_HOME/gh-bulk-pr/history` (`~/.local/state/gh-bulk-pr/history`). Press `↑`/`↓` in the search field to recall them.

Results load 50 at a time: the next page is fetched as you scroll near the bottom, and the header shows how many are loaded (`50 of 312`). GitHub search returns at most 1000 results, so narrow the query (`updated:>2026-01-01`, `repo:`, `author:`) for anything bigger. `ctrl+a` selects the loaded rows only, and the footer shows `N selected of M`. `r` reloads from the first page.

Every action asks for confirmation before running. Close and merge need an explicit `y`; labelling also accepts `enter`. `n` or `esc` cancels. The confirm and results lists scroll (`j`/`k`, `g`/`G`, page keys), and failed PRs are listed first in the results. The app needs a terminal of at least 97 columns by 11 rows.

## Development

Tools are managed by [mise](https://mise.jdx.dev).

```sh
mise run test
mise run lint
mise run build
```
