<p align="center">
  <img src="assets/hero.png" alt="Taiga CLI — an independent command-line client for Taiga" width="100%">
</p>

<h1 align="center">Taiga CLI</h1>

<p align="center">
  <strong>An independent command-line client for Taiga.</strong><br>
  Readable terminal output for people, and a stable JSON contract for shells, CI, and agents.
</p>

<p align="center">
  <a href="https://github.com/KoukeNeko/taiga-cli/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/KoukeNeko/taiga-cli?style=for-the-badge&logo=github&label=RELEASE&color=2196F3"></a>
  <a href="https://github.com/KoukeNeko/taiga-cli/releases"><img alt="Release downloads" src="https://img.shields.io/github/downloads/KoukeNeko/taiga-cli/total?style=for-the-badge&logo=github&label=DOWNLOADS&color=4CAF50"></a>
  <a href="https://github.com/KoukeNeko/taiga-cli/actions/workflows/ci.yml"><img alt="CI status" src="https://img.shields.io/github/actions/workflow/status/KoukeNeko/taiga-cli/ci.yml?branch=main&style=for-the-badge&logo=githubactions&logoColor=white&label=CI"></a>
  <a href="COMPATIBILITY.md"><img alt="Verified against Taiga 6.10.2" src="https://img.shields.io/badge/TAIGA-6.10.2_VERIFIED-00A5A5?style=for-the-badge"></a>
  <a href="LICENSE"><img alt="MIT licence" src="https://img.shields.io/badge/LICENSE-MIT-4CAF50?style=for-the-badge&logo=github"></a>
</p>

<p align="center">
  <strong>English</strong> · <a href="README.zh-TW.md">繁體中文</a>
</p>

<p align="center">
  <a href="INSTALL.md">Install</a>
  · <a href="#getting-started">Getting started</a>
  · <a href="https://github.com/KoukeNeko/taiga-cli/wiki">Handbook</a>
  · <a href="CHANGELOG.md">Changelog</a>
  · <a href="COMPATIBILITY.md">Compatibility</a>
</p>

```sh
taiga issue list
taiga issue create --subject "Fix token refresh" --type Bug
taiga issue close 42 --status Closed

# The same data, for a script, a CI job, or an agent
taiga issue view 42 --json --fields ref,subject,status,version
```

```json
{"data":{"ref":42,"status":"Closed","subject":"Fix token refresh","version":8},"meta":{"contract":1}}
```

An independent command-line client for [Taiga 6](https://taiga.io/), not affiliated with the Taiga
project. It drives projects, agile workflows, and wikis without leaving the terminal, discovers the
API from the frontend's `conf.json`, including sites deployed under a `/taiga/` subpath, and hands the
token to your operating system keyring instead of writing it into a config file.

**One command serves both people and programs.** Run it directly and you get aligned tables; add
`--json` and you get a versioned contract, backed by fixed exit codes and JSON Schema descriptors, so
a shell script, a CI job, or an LLM agent can drive it safely. The output format never changes just
because it was piped — if you want JSON, you ask for it.

**Writes behave predictably.** Work items carry a version, and Taiga checks it per field: a change to
a field someone else has already changed is refused instead of overwriting them, while their edit to
a different field merges. Nothing is ever auto-merged on your behalf — a refusal stops the command so
you can re-read and decide. Only idempotent GETs are retried. If a connection drops mid-write, the
CLI reports `ambiguous_commit` and asks you to verify rather than blindly resending.

## What it does

### The whole Taiga workflow

Day-to-day operations for projects, epics, user stories, tasks, issues, sprints, and wiki pages are
all here: list, view, create, edit, assign, comment, close, delete. Plus members and role
permissions, webhooks, custom fields, eight families of workflow metadata, swimlanes, tags, due-date
presets, and cross-project epic ↔ story links.

Work items accept a bare ref, a `project#ref` pair, or a pasted Taiga URL — all three work:

```text
42
example-project#42
https://taiga.example.com/taiga/project/example-project/issue/42
```

### A stable surface for automation

`--json` emits a `meta.contract` version, `--fields` selects columns, and `taiga schema <command>`
returns that command's input and output JSON Schema along with `safety` and `idempotency`
annotations — enough for an agent to decide whether a command may run unattended. Exit codes are
partitioned by failure kind, and `--dry-run` resolves and displays the mutation it would send while
guaranteeing that no write request leaves the process.

### Hard to break things with

Deleting work items and metadata is verified by reading the target back. Attachment and CSV downloads
stream to a `0600` temporary file, verify their digests, and land atomically without clobbering an
existing file. Destructive actions require an explicit `--yes` when there is no terminal. Webhook
secrets, application-token auth codes, and ownership-transfer tokens never appear in any output,
including dry runs.

### Safe to automate

A wrapper that misreports what happened to your data is worse than no wrapper, because a script acts
on the answer. Three things follow from that:

- **A refused write is told apart from a bad one by structure, not wording.** Taiga answers both with
  HTTP 400 under the same key and separates them only by the shape of the value, and it translates
  the sentence, so matching on the words would misfire and would fail outright on a server running in
  another language.
- **A write whose outcome is unknown says so.** Interrupting a request already in flight does not
  un-send it, so the CLI reports `ambiguous_commit` and asks you to check rather than claiming a
  failure it cannot prove.
- **Concurrent writes are exercised, not assumed.** An end-to-end test drives one project from twelve
  accounts at once and checks that no two accepted writes ever saw the same resulting version.

[Concurrency and conflicts](https://github.com/KoukeNeko/taiga-cli/wiki/Work-Items) covers what Taiga
refuses and what it merges.

### Many sites, many projects

Profiles switch between Taiga sites, each remembering its own API URL and default project. You can
also pin a profile and project to a single Git repository, stored in `.git/config` so it is never
committed:

```sh
taiga project use example-project --local
```

### Diagnosable when something breaks

`taiga doctor` checks frontend discovery, the API, authentication, and the default project one by
one. When you need help, `taiga doctor bundle` produces a report you can share without worrying:
version information, presence booleans, and status codes only — no URLs, usernames, project names,
or credentials — created locally and never uploaded.

## Getting started

1. **Install.** Homebrew, on macOS and Linux:

   ```sh
   brew install koukeneko/tap/taiga
   ```

   [Scoop](https://scoop.sh), on Windows:

   ```powershell
   scoop bucket add koukeneko https://github.com/KoukeNeko/scoop-bucket
   scoop install taiga
   ```

   Or the install script, which verifies the download against the release checksums before it
   installs anything:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/install.sh | sh
   ```

   ```powershell
   irm https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/install.ps1 | iex
   ```

   Release archives, manual checksum verification, and building from source are covered in
   [INSTALL.md](INSTALL.md). On Windows, open a new terminal afterwards to pick up the PATH change.

2. **Log in.** Run it with nothing else and answer two questions: the URL of any page inside your
   Taiga, with the hosted Taiga offered as the default, and how your account signs in. The token
   goes to the OS keyring:

   ```sh
   taiga auth login
   ```

   To skip the first question, pass `--url` with the URL of any page inside the Taiga web app, such
   as a project or backlog page; the API's address works too, and nothing beyond the site you typed
   is contacted. The hosted Taiga is `https://tree.taiga.io/`; the forum at `community.taiga.io` is
   a different site with its own accounts, and pasting its address offers the hosted app instead.

   ```sh
   taiga auth login --url https://taiga.example.com/taiga/ --profile company
   ```

   An account that signs in through GitHub or Google has no Taiga password. Choose that option at
   the second question, or pass `--with-token`, and taiga takes the tokens the web app holds: sign
   in on the web, open the browser's JavaScript console on that page, and run this to put them on
   the clipboard:

   ```js
   copy(JSON.stringify({auth_token: JSON.parse(localStorage.token), refresh: JSON.parse(localStorage.refresh)}))
   ```

   Then paste the result at the prompt, or pipe it in:

   ```sh
   pbpaste | taiga auth login --url https://tree.taiga.io/ --with-token
   ```

   The refresh token in that object lets the login renew itself the way a password login does. A
   bare token works too, but then the login lasts only until that token expires, which is 24 hours
   on a default Taiga 6.

   A token imported this way comes without a refresh token, so it stops working when the server's
   access token expires, which is 24 hours on a default Taiga 6. `TAIGA_TOKEN` is the same thing
   for a script.

3. **Pick a project:**

   ```sh
   taiga project list
   taiga project use example-project
   ```

4. **Start working:**

   ```sh
   taiga issue list
   taiga issue create --subject "Fix token refresh" --type Bug
   taiga issue assign 42 --to alice
   taiga issue close 42 --status Closed
   ```

5. **Wire up automation:**

   ```sh
   taiga issue view 42 --json --fields id,ref,subject,status,version --no-input
   ```

The full command reference, flag documentation, and per-subsystem behaviour live in the
[handbook wiki](https://github.com/KoukeNeko/taiga-cli/wiki), which includes
[worked automation recipes](https://github.com/KoukeNeko/taiga-cli/wiki/Automation-Recipes) for CI, shell
scripts and agents.

## Compatibility

- Taiga 6.10.2, verified by Docker E2E against a pinned image digest
- macOS, Linux, and Windows on `amd64` and `arm64`, built as pure Go (`CGO_ENABLED=0`)
- Password login, existing bearer tokens, and refresh-token rotation

The detailed matrix and known limits are in [COMPATIBILITY.md](COMPATIBILITY.md).

---

## Technical reference

### Setting resolution

General settings live in the operating system's user config directory. Tokens are never written
there:

```toml
current_profile = "company"

[profiles.company]
api_url = "https://taiga.example.com/taiga/api/v1/"
project = "example-project"
```

Resolution order, highest first:

```text
command flag
→ TAIGA_PROFILE / TAIGA_API_URL / TAIGA_PROJECT / TAIGA_TOKEN
→ Git-local taiga.profile / taiga.project
→ current profile
→ safe defaults
```

### JSON contract

Successful data goes to stdout and errors go to stderr, never mixed into one stream. Single records
use `data`, lists use `items` and `page`, and both carry `meta.contract`:

```json
{
  "data": { "id": 123, "ref": 42, "subject": "Fix token refresh", "version": 7 },
  "meta": { "contract": 1 }
}
```

Within one contract version only optional fields are added. Removing a field, renaming it, or
changing the type of an existing one requires a version bump and migration notes in the release.

| Exit code | Meaning |
| ---: | --- |
| 0 | success |
| 1 | unexpected internal failure |
| 2 | usage / schema |
| 3 | authentication |
| 4 | forbidden |
| 5 | not found |
| 6 | OCC conflict |
| 7 | validation / ambiguity |
| 8 | throttled |
| 9 | transport / upstream |
| 10 | confirmation required |
| 11 | ambiguous commit |
| 130 | interrupted before finishing |

`1` means the CLI hit something it has no classification for, and is worth reporting as a bug. `130`
follows the shell convention of 128 plus the signal number rather than taking a place in the table
above, because stopping a command is a decision rather than a way it failed; an interrupt that
stopped a write in flight reports `11` instead.

### Security principles

- Passwords are never accepted on the command line
- Authorization headers, passwords, and tokens never appear in verbose logs
- Only GETs are retried automatically, with a bounded count; POST and PATCH are never resent blindly
- A write whose outcome is unknown reports `ambiguous_commit` instead of retrying
- OCC conflicts are never auto-merged or overwritten
- Attachment downloads never send the API bearer token to the media URL
- TLS verification is always on

### Development and testing

The fast loop, no Docker required:

```sh
make test
make test-race
make lint
```

Integration tests against a real Taiga server:

```sh
make test-integration
```

The harness uses a dedicated `taiga-cli-e2e` Compose project on `localhost:19000`, creates its own
throwaway account, project, and issues, and tears down only its own containers and volumes — it never
touches a Taiga instance you use day to day.

Rebuilding cross-platform release artifacts:

```sh
make release \
  VERSION=v0.1.0 \
  COMMIT="$(git rev-parse HEAD)" \
  SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)"
```

The same source, Go toolchain, version, commit, and epoch produce byte-identical Linux and Windows
archives. The maintainer release process is in [RELEASING.md](RELEASING.md). macOS archives are the exception: notarization requires a secure timestamp from Apple, so a
Developer ID signature can never reproduce. Their contents are otherwise built identically.

<p>
  <img alt="Cobra" src="https://img.shields.io/badge/COBRA-CLI-00ADD8?style=for-the-badge&logo=go&logoColor=white">
  <img alt="Reproducible builds" src="https://img.shields.io/badge/BUILDS-REPRODUCIBLE-4CAF50?style=for-the-badge">
  <img alt="SPDX 2.3 SBOM" src="https://img.shields.io/badge/SBOM-SPDX_2.3-2196F3?style=for-the-badge">
  <img alt="Codacy code quality" src="https://img.shields.io/codacy/grade/b8f361063a874af3be58396ac7c7ad27?branch=main&style=for-the-badge&logo=codacy&label=CODE%20QUALITY">
</p>

## Support

If Taiga CLI is useful to you, you can support development:

<a href="https://buymeacoffee.com/doershing"><img alt="Buy Me a Coffee" src="https://img.shields.io/badge/Buy%20Me%20a%20Coffee-doershing-FFDD00?style=for-the-badge&logo=buymeacoffee&logoColor=black"></a>

## Trademarks

Taiga is a trademark of its respective owner. This project is an independent client that is not
affiliated with, endorsed by, or sponsored by the Taiga project or its maintainers, and uses the name
only to describe the software it works with. The Taiga team
[confirmed on the community forum](https://community.taiga.io/t/aihki-a-cli-client-for-taiga-plus-a-naming-question/8947)
that a small, independent third-party client describing itself with the Taiga name is not a problem.

## License

[MIT](LICENSE) © KoukeNeko
