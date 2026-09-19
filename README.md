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

| Flag        | Default                                       | Description                       |
| ----------- | --------------------------------------------- | --------------------------------- |
| `--query`   | `is:open is:pr archived:false sort:updated-desc involves:@me`   | GitHub search query for the list  |
| `--mouse`   | off                                           | Scroll with the mouse wheel (hold shift to select text) |
| `--version` |                                               | Print version and exit            |

## Keys

Press `?` in the app for this list.

| Key                       | Action                                         |
| ------------------------- | ---------------------------------------------- |
| `↑`/`↓`, `j`/`k`          | Move                                           |
| `g`/`home`, `G`/`end`     | Jump to the first / last loaded row            |
| `pgup`/`b`, `pgdn`/`f`    | Page up / down                                 |
| `x` / `space`             | Toggle selection                               |
| `ctrl+a`                  | Select all loaded rows                         |
| `1` / `2`                 | Switch tab: `involves:@me` / `owner:@me`       |
| `/`                       | Edit the full search query                     |
| `enter` / `p`             | Toggle preview                                 |
| `T`                       | Open the PR in `gh enhance` (checks)           |
| `l`                       | Add label to selected PRs                      |
| `c`                       | Close selected PRs                             |
| `m`                       | Merge selected PRs                             |
| `r`                       | Refresh the PR list                            |
| `esc`                     | Close preview, then clear selection            |
| `?`                       | Show all keys                                  |
| `q`                       | Quit (press twice when PRs are selected)       |
| `ctrl+c`                  | Quit at once, from any screen                  |

Both tabs share the base query `is:open is:pr archived:false sort:updated-desc`. Editing the query with `/` deselects the tab unless it still matches one.

Queries run from the filter bar or a tab are appended to `$XDG_STATE_HOME/gh-bulk-pr/history` (`~/.local/state/gh-bulk-pr/history`). Press `↑`/`↓` in the search field to recall them.

Results load 50 at a time: the next page is fetched as you scroll near the bottom, and the header shows how many are loaded (`50 of 312`). GitHub search returns at most 1000 results, so narrow the query (`updated:>2026-01-01`, `repo:`, `author:`) for anything bigger. `ctrl+a` selects the loaded rows only, and the footer shows `N selected of M`. `r` reloads from the first page.

Every action asks for confirmation before running. Close and merge need an explicit `y`; labelling also accepts `enter`. `n` or `esc` cancels. The confirm and results lists scroll (`j`/`k`, `g`/`G`, page keys), and failed PRs are listed first in the results. The app needs a terminal of at least 91 columns by 11 rows.

## Development

Tools are managed by [mise](https://mise.jdx.dev).

```sh
mise run test
mise run lint
mise run build
```
