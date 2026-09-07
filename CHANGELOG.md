# Changelog

***English** · [繁體中文](CHANGELOG.zh-TW.md)*

Notable changes to this project are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The release workflow publishes the section matching the tag as the GitHub Release notes, alongside the same section from [CHANGELOG.zh-TW.md](CHANGELOG.zh-TW.md), so both files must carry every released version.

## [Unreleased]

### Changed

- The CLI now says what it expects rather than leaving it to be looked up. An argument error carries the command's usage line, so `custom-field list` names `<epic|story|task|issue>` instead of only counting what was missing; `--fields` says that an unknown name lists the available ones, which is the one complete source, since a field the sampled row left out still appears there; and the root help points an unattended caller at `aihki schema <command>` for a command's schemas, safety and idempotency. Each of these was a wrong turn taken while driving the CLI, and each is now answered where the wrong turn happens rather than in the README.

### Fixed

- A table that holds only one page of a longer list now says so, as `showing 30 of 54; use --limit to see more` on stderr. Until now a table that stopped at the page size looked exactly like a list that ended there, so `story history`, `story list` and every other paginated table quietly invited conclusions drawn from a fraction of the data. The notice stays off stdout so the table is still pipeable, says nothing when the page holds the whole list, and is silent under `--quiet`; JSON output is unchanged, having carried `page` all along.

## [0.5.0-rc.1] - 2026-09-07

Logging in is where this release goes. `aihki auth login` now asks which Taiga to log in to even when the profile already saved one, so moving to another Taiga no longer means knowing `--url`, and Enter keeps the saved URL without contacting anything. A URL under `taiga.io` that is not the web app, such as the community forum, is now offered the hosted app instead of only being refused.

### Added

- When the address given to an interactive `auth login` is under `taiga.io` but is not the web app, such as the community forum, the login offers `https://tree.taiga.io/` and continues there on a yes. Nothing is contacted beyond the typed site until the person has answered, and a script still gets the error, because nothing may choose a destination for it.
- `auth login --with-token` accepts the JSON object the web app holds, `{"auth_token": …, "refresh": …}`, as well as a bare token. With the refresh token an imported login renews itself the way a password login does, instead of ending when the access token expires; the wizard and the README give the console one-liner that copies both. The result says whether a refresh token was stored.

### Changed

- `auth login` at a terminal now asks which Taiga to log in to even when the profile already saved one, offering the saved URL as the default so that Enter keeps it and contacts nothing. Logging in is when someone moves to another Taiga, and until now the saved URL was used without a word and could only be changed by knowing `--url`. An API URL this invocation names through `--api-url` or the environment is still obeyed without a question, and a script keeps the saved URL, having nobody to ask.

## [0.4.0] - 2026-09-03

The first login is the whole of this release. `aihki auth login` now asks for the URL of any page inside your Taiga, with the hosted Taiga as the default, and then how your account signs in, so that an account backed by GitHub or Google is walked to its token instead of a password it does not have. `--url` replaces `--host`, which still works, and a token pasted at a terminal takes Enter rather than Ctrl-D.

### Added

- `aihki auth login` with nothing else asks two questions a person can answer: the URL of any page inside their Taiga, with the hosted Taiga offered as the default so that its users only press Enter, and how their account signs in, offering a password, a provider such as GitHub or Google, or an existing token. The site and API the credential will go to are shown before any credential is asked for, and a script that omits the URL is told to pass `--url` rather than being asked.
- `--url` takes the URL of any page inside the Taiga web app, such as a project or backlog page. The web app's `conf.json` is looked for at that path and at each path above it, the API it names has to answer as Taiga before it counts, and the API's own address is accepted as well. Only the site that was typed is contacted: a redirect within it is followed, a redirect to another site is reported, and nothing is sent with a credential. When no Taiga is found, the error lists every URL tried and what each answered, says that no credentials were sent, and for a host under `taiga.io` names `https://tree.taiga.io/` as the hosted web app.
- A refused password now says that an account which signs in through GitHub, Google or another provider has no password, and gives the `--with-token` command to run instead.

### Changed

- The flag that names the Taiga site is `--url` on `auth login` and `doctor`; `--host` still works and says to use `--url`. What the flag takes is a URL with a scheme and a path, which "host" misnamed.

### Fixed

- `auth login --with-token` at a terminal prompts for the token and takes one line, entered without echo like a password, so pasting it and pressing Enter is enough. It used to wait for the end of input, which at a terminal means Ctrl-D and looked like a hang. Piped input is unchanged.

## [0.3.2] - 2026-09-03

Fixes to the request layer, found by reviewing it against a real Taiga. The `aihki` binary changes in what it reports, not in what it sends: a transfer is no longer cut off at thirty seconds, a refresh that failed for any reason other than Taiga refusing it says so, every deletion that reads the record back runs through one implementation, and a `--host` that is not a Taiga web app is named as such.

### Fixed

- Interrupting `role delete`, `swimlane delete`, `due-date delete` or a workflow status delete no longer reports that nothing happened. Those four do not go through the shared `Delete` wrapper — they carry a `moveTo` query or reuse the delete-then-verify helper — so they call the request layer directly. When 0.3.0 replaced the "is this a GET" flag with a per-call statement of what a request can commit, those three call sites kept passing `false`, which used to mean "an ordinary GET" and now meant "this commits nothing". Ctrl-C mid-delete therefore exited 130 saying the command was interrupted, and a connection lost mid-delete exited 9 marked retryable, for a deletion Taiga may well have carried out. All four now report `ambiguous_commit` and exit 11 like every other write, and the flag is no longer a bool, so making the same mistake again fails to compile.
- When Taiga rotates the token and the new one cannot be written to the OS keyring, the command now says that the saved credential is stale and asks for `aihki auth login`, rather than repeating the `401` that started the refresh. Taiga retires the old refresh token as it issues the new one, so the login on disk is dead from that moment; the message saying so has existed since 0.3.0, but the request layer discarded it in favour of the rejection, which said "expired" and left no way to tell why the next attempt failed too.
- A malformed config left at the pre-rename location is reported under its own path. The parse error named the current location, which does not exist until the next save.
- Attachment uploads and downloads, CSV exports and project dumps are no longer cut off after thirty seconds. The HTTP client carried an overall deadline that covered the whole transfer, so a file larger than the link could move in that time failed every time: downloads as a retryable transport error, uploads as `ambiguous_commit` for a file Taiga had not received. Each JSON request still has thirty seconds per attempt. A transfer is instead abandoned when no data has moved in either direction for sixty seconds, and the error says so, so a dead peer still ends the command and a large file is as long as it is.
- A refresh that never reaches Taiga, comes back unreadable or is throttled is reported as what it was, rather than as the `401` that triggered it. That rejection sent people to log in again over a dropped connection, when the refresh token on disk was still good; only Taiga refusing the refresh keeps the original rejection.
- `project import` exercises the credential before streaming the dump. Every other command reaches Taiga with a JSON lookup first, which is where an expired token gets refreshed; import had no lookup of its own, so the first command after a day away failed with `auth` if it was an import.
- Interrupting an attachment or CSV download mid-stream exits `130` like an interrupt before the first byte, rather than `9` marked retryable.
- `auth login --host` and `doctor --host` pointed at something that is not a Taiga web app, such as the community forum, said only "Not Found". The error now names the `conf.json` URL that was tried, what came back, and what `--host` should be. A host that answers with a page instead of JSON reports `validation` rather than an internal failure.

### Changed

- Every deletion that reads the record back before reporting success -- work items, sprints, wiki links, workflow metadata, due dates and swimlanes -- runs through one implementation instead of five copies of the same exchange. The one wording that moved: the sprint read-back now says "sprint" in lowercase, as its sibling message already did.

## [0.3.1] - 2026-09-03

Tooling and documentation. The `aihki` binary behaves as it did in 0.3.0: the close and comment commands were rewritten to share one implementation, and the help text of all 38 commands, every dry-run label and every error code were compared against 0.3.0 to confirm nothing a caller sees moved.

### Added

- The exit table now lists `1`, which is what the CLI returns when it meets something it has no classification for and is worth reporting as a bug, and `130`, added in 0.3.0 but never documented. The Chinese table gained a translation column, since half its rows named the English term and half a Chinese one.
- A short section on what makes the writes safe to automate, linking to the page covering what Taiga refuses and what it merges.
- gosec runs as part of the local lint, so a new weak hash, hardcoded credential or subprocess built from user input is refused before a push rather than after one. Every rule turned off is turned off for a stated reason.
- CI reports test coverage.

### Changed

- Building from source needs Go 1.25.13 or newer, the first 1.25 with no outstanding standard library advisories. Released binaries were already built with 1.26.

### Fixed

- README claimed every mutation carries a version. Memberships, webhooks and attachment metadata carry none, so that claim is now scoped to work items, and the per-field behaviour it describes is exercised by the concurrency test rather than only asserted in prose.

## [0.3.0] - 2026-09-02

### Fixed

- A rejected write now says what was rejected. Taiga reports validation through Django REST Framework, which names each field it refused, and the CLI was discarding all of it and printing the HTTP status text instead, so a missing subject or an unknown assignee both read `Bad Request`. Nested serializer errors and the per-row form a bulk create is answered with are rendered too, and the rendering is bounded so a large response cannot fill a terminal with one line.
- Telling a stale write apart from a bad one no longer depends on the wording of Taiga's message. Taiga answers both with HTTP 400 under the same `version` key and separates them only by shape: its own concurrency check sends one sentence, while a malformed field arrives as a list. The previous rule searched the message for the word "version", which called `Version must be specified` somebody else's edit — sending a caller to re-read and retry a request that could never succeed — and would have stopped recognising a real conflict entirely against a server running in another language, since Taiga translates that sentence.
- An interrupted write no longer claims the command has a bug. Pressing Ctrl-C mid-write printed `unexpected failure: context canceled` and exited 1, which says the write did not happen; Taiga does not roll one back because the client stopped listening, so it now reports `ambiguous_commit` and asks you to verify. A request cancelled before anything was sent stays an ordinary interrupt.
- Interrupting a request that settles nothing — logging in, liking a project — no longer reports a possible commit. Whether a call can leave something behind is now declared where the endpoint is known rather than guessed from the HTTP verb.
- An attachment upload or project import that Taiga answered is no longer reported as a transport failure. Taiga replies to a rejected or oversized upload without reading the rest of the body, which fails the sending side; that failure was checked first, so a completed answer — including a `201` — was thrown away and the caller was told the upload failed while the attachment existed.
- Cancelling a CSV or attachment download is no longer reported as a retryable server fault, which invited an agent to start again something the operator had just stopped.

### Added

- Exit code `130` and the contract codes `interrupted` and `timeout`, for a command stopped before it finished. This follows the shell convention of 128 plus the signal number rather than taking a place in the partitioned table, because an interrupt is a decision, not a way the command failed. A write whose outcome is unknown still reports `ambiguous_commit` and exit 11, even when an interrupt caused it.
- An end-to-end pressure test: twelve accounts drive one project at once with no pacing, mixing contended edits, uncontended edits, comments, bulk creates and deliberate mistakes. It asserts that every accepted write to the contended record received its own version, that no failure was reported as an internal defect or as bare status text, and that a run which failed to provoke a conflict, a validation error and a missing record fails rather than passing on having proved nothing.

## [0.2.3] - 2026-09-02

Tooling and documentation only. The `aihki` binary is unchanged from 0.2.2.

### Added

- The install script now writes shell completions instead of only printing the command that generates them. It writes into a standard directory that already exists and names each file it wrote; it never creates such a directory, since that would leave a stray folder in the home directory of someone who does not use that shell. Windows still prints the hint, because installing PowerShell completion means editing a profile rather than dropping in a file.
- The uninstaller removes exactly those files, skipping any that does not look generated so a hand-written completion of the same name survives.

### Fixed

- CI now runs the installer and uninstaller as a round trip on all three operating systems. An earlier release claimed this coverage, but the change that was meant to add it silently matched nothing, so the job had only ever run the installer.

## [0.2.2] - 2026-09-02

### Fixed

- Errors that tell you what to run next named the pre-rename command, so a missing credential suggested `taiga auth login` and `TAIGA_TOKEN` rather than `aihki auth login` and `AIHKI_TOKEN`. Seventeen such messages across eleven files now name commands that exist.

### Changed

- The end-to-end suite authenticates with `AIHKI_TOKEN`, which it had never exercised, and gained a case proving `TAIGA_TOKEN` still authenticates a real binary. A fallback covered only by a unit test is a fallback nobody has run.

## [0.2.1] - 2026-09-02

Tooling and documentation only. The `aihki` binary is functionally unchanged from 0.2.0.

### Added

- Uninstall scripts for macOS, Linux and Windows, covering the binary, the PATH entry on Windows, and optionally the configuration directory and the credential in the OS keyring. Removal is conservative by default because uninstalling is often one step of an upgrade: `--purge` (`-Purge` on Windows) opts into removing settings and credentials, including whatever the pre-rename name left behind, and `--dry-run` reports what would go without changing anything.
- The POSIX uninstaller detects a Homebrew installation and hands you back to `brew uninstall` rather than deleting files Homebrew tracks. Both scripts print where a `project use --local` pin lives, since no uninstaller can find one from outside the repository holding it.
- A hero image on the repository front page.

### Changed

- INSTALL.md documents uninstalling in both languages.
- CI runs the installer and uninstaller as a round trip on all three operating systems, so the uninstaller has to remove exactly what the installer wrote.

## [0.2.0] - 2026-09-02

### Renamed to Aihki

The project is now **Aihki**, and the binary is `aihki`. Taiga ships under MPL-2.0, whose section 2.3 grants no rights in its trademarks or logos, and the Taiga maintainers have previously required a spin-off to drop the name. Leading with someone else's mark as this project's own identifier was not something the licence ever supported, so the name is now used only to describe what this client talks to.

Nothing about the Taiga integration changes. What changes is the identity of this tool.

**Existing installations keep working.** Every stored setting is migrated on first use rather than being abandoned:

- A credential in the old `taiga-cli` keyring service is adopted into `aihki` the first time it is read, so you are not logged out.
- A config file under `taiga-cli/` is read when `aihki/` has none, and the next save writes to the new location.
- A repository pinned with `taiga.profile` or `taiga.project` in `.git/config` still resolves; `aihki.*` takes precedence when both exist.
- `TAIGA_TOKEN`, `TAIGA_PROFILE`, `TAIGA_API_URL` and `TAIGA_PROJECT` are still honoured. `AIHKI_*` wins when both are set.

**What you do need to change:** `brew install koukeneko/tap/aihki` replaces the old formula, release archives are now named `aihki_<version>_<os>_<arch>`, and completion files are `aihki.bash`, `_aihki`, `aihki.fish` and `aihki.ps1`.

## [0.1.0] - 2026-09-02

First stable release. A Taiga 6 command line tool written in Go, offering readable terminal output and a stable JSON contract for shells, CI, and agents.

### Install

```sh
brew install koukeneko/tap/aihki
```

Or download the archive for your platform below, verify it against `SHA256SUMS`, and extract it.

### Features

- Full day-to-day operations for projects, epics, user stories, tasks, issues, sprints, and wiki pages.
- Members and role permissions, webhooks, custom fields, eight families of workflow metadata, swimlanes, tags, and due-date presets.
- Cross-project epic ↔ story links, plus watching, voting, comment editing, and history.
- Project export and import, ownership transfer, CSV export, and a third-party importer gateway.
- Search, timeline, and statistics for backlog velocity, issue trends, member contributions, and sprint burndown.
- Streaming attachment upload and download, with size and SHA-1 verified on the way in.
- Bulk creation for epics, stories, tasks, and issues, and bulk move and reorder for stories, tasks, and issues.

### Automation surface

- `--json` emits an envelope carrying `meta.contract`, currently contract 1.
- `--fields` selects fields, and `aihki schema <command>` returns JSON Schema with safety and idempotency annotations.
- Exit codes are partitioned by failure kind, and `--dry-run` resolves fully while guaranteeing that no write request is sent.
- Completions for Bash, Zsh, Fish, and PowerShell, with a five-minute cache and stale-on-error fallback when offline.

### Security

- Passwords are never accepted on the command line, and tokens live in the OS keyring rather than a config file.
- Webhook secrets, application token auth codes, and ownership transfer tokens never appear in any output.
- Only idempotent GETs are retried; a write whose outcome is unknown reports `ambiguous_commit` and asks for confirmation.
- Optimistic concurrency conflicts stop the command instead of merging or overwriting.
- Attachment downloads never send the API bearer token to the media URL.

### Release artifacts

- macOS binaries are signed with a Developer ID certificate and notarized by Apple, so a download runs without extra steps.
- Linux and Windows archives are byte-for-byte reproducible: the same source, toolchain, version, commit, and epoch produce identical bytes. macOS cannot reproduce because notarization requires a secure timestamp.
- Every release ships `SHA256SUMS` and an SPDX 2.3 SBOM.

### Known limitations

- Taiga 6's REST API exposes `archived_code` as read-only and offers no archive action, so `project archive|unarchive` reports `unsupported_capability` and points at a site administrator.
- A notarization ticket cannot be stapled to a bare executable, so Gatekeeper resolves it online and a fully offline first run can still be blocked.
- `stats system` exists only when a site enables Taiga's `STATS_ENABLED`; otherwise it reports `not_found`.

### Compatibility

Verified against Taiga 6.10.2 through a full Docker E2E run against a pinned image digest. Supports macOS, Linux, and Windows on `amd64` and `arm64`.

[Unreleased]: https://github.com/KoukeNeko/aihki/compare/v0.5.0-rc.1...HEAD
[0.5.0-rc.1]: https://github.com/KoukeNeko/aihki/releases/tag/v0.5.0-rc.1
[0.4.0]: https://github.com/KoukeNeko/aihki/releases/tag/v0.4.0
[0.3.2]: https://github.com/KoukeNeko/aihki/releases/tag/v0.3.2
[0.3.1]: https://github.com/KoukeNeko/aihki/releases/tag/v0.3.1
[0.3.0]: https://github.com/KoukeNeko/aihki/releases/tag/v0.3.0
[0.2.3]: https://github.com/KoukeNeko/aihki/releases/tag/v0.2.3
[0.2.2]: https://github.com/KoukeNeko/aihki/releases/tag/v0.2.2
[0.2.1]: https://github.com/KoukeNeko/aihki/releases/tag/v0.2.1
[0.2.0]: https://github.com/KoukeNeko/aihki/releases/tag/v0.2.0
[0.1.0]: https://github.com/KoukeNeko/aihki/releases/tag/v0.1.0
