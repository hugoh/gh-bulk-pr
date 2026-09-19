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
| `--version` |                                               | Print version and exit            |

## Keys

| Key            | Action                                  |
| -------------- | --------------------------------------- |
| `↑`/`↓`        | Move                                    |
| `x` / `space`  | Toggle selection                        |
| `ctrl+a`       | Select all                              |
| `1` / `2`      | Switch tab: `involves:@me` / `owner:@me` |
| `/`            | Edit the full search query              |
| `enter` / `p`  | Toggle preview                          |
| `T`            | Open the PR in `gh enhance` (checks)    |
| `l`            | Add label to selected PRs               |
| `r`            | Refresh the PR list                     |
| `c`            | Close selected PRs                      |
| `m`            | Merge selected PRs                      |
| `esc`          | Close preview, then clear selection     |
| `q` / `ctrl+c` | Quit                                    |

Both tabs share the base query `is:open is:pr archived:false sort:updated-desc`. Editing the query with `/` deselects the tab unless it still matches one.

Queries run from the filter bar or a tab are appended to `$XDG_STATE_HOME/gh-bulk-pr/history` (`~/.local/state/gh-bulk-pr/history`). Press `↑`/`↓` in the search field to recall them.

Results load 50 at a time: the next page is fetched as you scroll near the bottom, and the header shows how many are loaded (`50 of 312`). GitHub search returns at most 1000 results, so narrow the query (`updated:>2026-01-01`, `repo:`, `author:`) for anything bigger. `ctrl+a` selects the loaded rows only, and the footer shows `N selected of M`. `r` reloads from the first page.

Every action asks for confirmation (`y`/`enter` or `n`/`esc`) before running.

## Development

Tools are managed by [mise](https://mise.jdx.dev).

```sh
mise run test
mise run lint
mise run build
```
