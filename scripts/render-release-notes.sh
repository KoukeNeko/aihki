#!/usr/bin/env sh
# Compose bilingual GitHub Release notes for one version.
#
# English leads because the repository front page does, and the Traditional
# Chinese section follows inside a collapsed block so the page stays readable
# while carrying both.
set -eu

ENGLISH_CHANGELOG="CHANGELOG.md"
CHINESE_CHANGELOG="CHANGELOG.zh-TW.md"

usage() {
    printf '%s\n' "usage: $0 <version-tag>" >&2
    exit 2
}

[ "$#" -eq 1 ] || usage
tag=$1
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
extract="$script_dir/extract-changelog.sh"

english=$("$extract" "$tag" "$ENGLISH_CHANGELOG")
chinese=$("$extract" "$tag" "$CHINESE_CHANGELOG")

printf '%s\n' "$english"
printf '\n---\n\n'
printf '<details>\n<summary><b>繁體中文</b></summary>\n\n'
printf '%s\n' "$chinese"
printf '\n</details>\n'

# A short install/update block so anyone landing on the release can get it
# without hunting; the full matrix lives in INSTALL.md.
cat <<'NOTES'

---

## Install / Update

- **Homebrew** (macOS/Linux): `brew install koukeneko/tap/taiga` · update with `brew upgrade taiga`
- **Scoop** (Windows): `scoop bucket add koukeneko https://github.com/KoukeNeko/scoop-bucket && scoop install taiga`
- **APT / DNF repositories** (auto-updating) and **`.deb` / `.rpm`**: see [INSTALL.md](https://github.com/KoukeNeko/taiga-cli/blob/main/INSTALL.md)
- Or download an asset below and verify it against `SHA256SUMS`.
NOTES
