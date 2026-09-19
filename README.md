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
| `--query`   | `is:open is:pr involves:@me archived:false`   | GitHub search query for the list  |
| `--version` |                                               | Print version and exit            |

## Keys

| Key            | Action                                  |
| -------------- | --------------------------------------- |
| `↑`/`↓`        | Move                                    |
| `x` / `space`  | Toggle selection                        |
| `ctrl+a`       | Select all                              |
| `/`            | Change the search query                 |
| `enter` / `p`  | Toggle preview                          |
| `T`            | Open the PR in `gh enhance` (checks)    |
| `l`            | Add label to selected PRs               |
| `r`            | Refresh the PR list                     |
| `c`            | Close selected PRs                      |
| `m`            | Merge selected PRs                      |
| `esc`          | Close preview, then clear selection     |
| `q` / `ctrl+c` | Quit                                    |

Every action asks for confirmation (`y`/`enter` or `n`/`esc`) before running.

## Development

Tools are managed by [mise](https://mise.jdx.dev).

```sh
mise run test
mise run lint
mise run build
```
