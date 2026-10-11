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
| `--refresh` | `30s` | how often to re-check PRs waiting on checks, merge state or auto-merge, e.g. 30s (0 turns it off) |
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
| `A` | my PRs |
| `1/2` | tab |
| `r` | refresh |
| `T` | checks |
| `o` | open |
| `O` | open all |
| `y` | copy url |
| `l` | label |
| `c` | close |
| `m` | merge |
| `a` | auto |
| `v` | approve |
| `u` | update |
| `U` | update on GitHub |
| `?` | help |
| `q` | quit |

With nothing selected, `l`, `c`, `m`, `a`, `v`, `u` and `U` act on the PR under the cursor.

`m` and `a` merge with squash where the repo allows it, and otherwise with a merge commit or a rebase, whichever the repo allows; the confirm screen notes `merges with a merge commit` or `merges by rebasing` on those PRs. `v` approves each selected PR.

`a` toggles auto-merge on each selected PR: it enables it where it is off and disables it where it is on. PRs with auto-merge enabled show `on` in the Auto column. A PR that is already ready to merge has nothing to wait for (GitHub refuses auto-merge on it), so `a` merges it right away; the confirm screen marks those PRs `merges now` and needs an explicit `y`. Repos with auto-merge turned off fail for the PRs that need it.

`u` merges the base branch into each selected PR, for PRs that show `behind` when the repo requires branches to be up to date. Pair it with `a` so they merge once checks pass. Renovate and Dependabot PRs are updated differently: see [Bot-aware actions](#bot-aware-actions).

`ctrl+c` quits at once from any screen; `q` needs a second press while PRs are selected. `O` asks for confirmation above 5 PRs and opens at most 20.

Tabs (`1`/`2`): `owner:@me`, `involves:@me`. All share the base query `is:open is:pr archived:false sort:updated-desc`. Editing the query with `/` deselects the tab unless it still matches one.

Queries run from the filter bar or a tab are appended to `$XDG_STATE_HOME/gh-bulk-pr/history` (`~/.local/state/gh-bulk-pr/history`). Press `↑`/`↓` in the search field to recall them.

Results load 50 at a time: the next page is fetched as you scroll near the bottom, and the header shows how many are loaded (`50 of 312`). GitHub search returns at most 1000 results, so narrow the query (`updated:>2026-01-01`, `repo:`, `author:`) for anything bigger. `ctrl+a` selects the loaded rows only, and the footer shows `N selected of M`. `r` reloads from the first page.

PRs that can change by themselves are re-checked every `--refresh`: pending checks, a merge state GitHub is still working out (`↻`), auto-merge, or a Renovate rebase in progress (`↻`). Each PR slows down on its own, up to every 5 minutes, while nothing changes. In a list of open PRs, merged and closed PRs drop out as soon as the app sees them close, even while GitHub's search still lists them; a PR reopened later shows up again.

Every action asks for confirmation before running. Close and merge need an explicit `y`, as do `a` and `U` on the PRs they mark; the others also accept `enter`. `n` or `esc` cancels. The confirm and results lists scroll (`j`/`k`, `g`/`G`, page keys), and failed PRs are listed first in the results. The app needs a terminal of at least 99 columns by 11 rows.

## Bot-aware actions

Dependency bots stop updating a branch someone else has pushed to, and GitHub's update commit is authored by you. So for their PRs, some actions ask the bot instead. A PR is [Renovate](https://docs.renovatebot.com)'s when the Renovate app opened it, or, for a self-hosted Renovate, when its body has Renovate's rebase/retry checkbox, which is known once its details have loaded; it is [Dependabot](https://docs.github.com/code-security/dependabot)'s when Dependabot opened it.

- `u` asks the bot to rebase the PR instead of updating it on GitHub. For Renovate it adds the `rebase` label (Renovate's default `rebaseLabel`), which also works when Renovate cut a long body before its checkbox; for Dependabot it comments `@dependabot rebase`. The confirm and results screens mark those PRs `rebase via Renovate` or `rebase via Dependabot`; other selected PRs get a normal update.
- While the `rebase` label stays on, or Renovate's box stays ticked, the row shows `↻`, the preview says `rebase requested`, and the PR is re-checked until Renovate removes it. Dependabot leaves no such mark, so its PRs show the rebase once their checks restart.
- `U` updates on GitHub anyway. On a bot's PR the confirm screen warns `Renovate stops updating this PR` (or Dependabot) and needs an explicit `y`; on other PRs it is the same as `u`.
- `c` notes what closing tells the bot: `Renovate ignores this update from now on`, `Dependabot skips this version from now on`.

## Development

Tools are managed by [mise](https://mise.jdx.dev).

```sh
mise run test
mise run lint
mise run build
```
