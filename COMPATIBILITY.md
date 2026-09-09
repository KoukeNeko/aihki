# Compatibility matrix

***English** · [繁體中文](COMPATIBILITY.zh-TW.md)*

Updated: 2026-09-03

## Taiga server

| Server | Status | Evidence and limits |
| --- | --- | --- |
| Taiga 6.10.2 | Verified | CI runs a full Docker E2E against a pinned image digest, covering auth, projects, agile workflows, wiki, attachments, bulk, stats, and export/import. |
| Taiga 6.10.x | Expected compatible | Same API family. Run `taiga doctor` first. |
| Earlier Taiga 6 | Unverified | The basic `/api/v1` may work, but no promise is made about every endpoint and serializer field. |
| TaigaNext | Unsupported | A different API contract, with no compatibility layer yet. |

`project archive|unarchive` reports `unsupported_capability`, because the verified Taiga REST contract
offers no archive action a CLI can use safely.

## Operating systems and architectures

Release packaging produces pure Go binaries (`CGO_ENABLED=0`) for:

- macOS `amd64`, `arm64`
- Linux `amd64`, `arm64`
- Windows `amd64`, `arm64`

CI compiles on Ubuntu, macOS, and Windows, and on Linux verifies the complete release archive,
checksums, SBOM, embedded version metadata, and reproducibility.

macOS binaries are signed with a Developer ID certificate and notarized by Apple (Team ID
`33832Z66QU`). Because notarization requires a secure timestamp, darwin archives are not
byte-for-byte reproducible; Linux and Windows archives are.

## Authentication

| Mode | Status |
| --- | --- |
| Taiga normal username and password | Verified |
| Existing bearer token or `TAIGA_TOKEN` | Verified |
| Refresh token rotation | Verified by automated test |
| SSO and LDAP plugins | No generic interactive login. An existing token can be imported where the site allows it. |
| Accounts that sign in through GitHub, Google or another provider | Token import: paste the web app's `{"auth_token": …, "refresh": …}` from the browser's Local Storage into `auth login --with-token`; the refresh token lets the login renew itself. A bare token lasts one access-token lifetime, 24 hours on a default Taiga 6. |

## Machine contract migration

The CLI JSON contract is currently `meta.contract: 1`. Within one contract version only optional
fields are added. Removing a field, renaming it, or changing the type of an existing one requires a
contract version bump and migration notes in the release.

The diagnostic bundle format is currently `manifest.format: 1` and evolves independently of the CLI
JSON contract. A bundle carries no runtime identifiers or secrets, and is always created locally and
never uploaded.
