#!/usr/bin/env sh
# Render the Homebrew formula for a published release.
#
# The formula installs the release archives rather than rebuilding from source,
# so macOS users get the signed and notarized binary that CI produced. Each
# supported platform therefore pins its own archive and digest, read from the
# SHA256SUMS the release already publishes.
set -eu

REPOSITORY_URL="https://github.com/KoukeNeko/taiga-cli"

usage() {
    printf '%s\n' "usage: $0 <version-tag> <sha256sums-file>" >&2
    exit 2
}

[ "$#" -eq 2 ] || usage
tag=$1
checksums=$2

case "$tag" in
    v*) version=${tag#v} ;;
    *)
        printf '%s\n' "version tag $tag must start with v" >&2
        exit 2
        ;;
esac

case "$version" in
    *-*)
        printf '%s\n' "refusing to render a formula for pre-release $tag" >&2
        exit 2
        ;;
esac

[ -f "$checksums" ] || {
    printf '%s\n' "no such checksum file: $checksums" >&2
    exit 2
}

digest_for() {
    archive="taiga_${version}_$1.tar.gz"
    value=$(awk -v name="$archive" '$2 == name { print $1 }' "$checksums")
    [ -n "$value" ] || {
        printf '%s\n' "no digest for $archive in $checksums" >&2
        exit 1
    }
    printf '%s' "$value"
}

darwin_arm64=$(digest_for darwin_arm64)
darwin_amd64=$(digest_for darwin_amd64)
linux_arm64=$(digest_for linux_arm64)
linux_amd64=$(digest_for linux_amd64)

cat <<FORMULA
class Taiga < Formula
  desc "Independent command-line client for Taiga"
  homepage "$REPOSITORY_URL"
  version "$version"
  license "MIT"

  livecheck do
    url :homepage
    strategy :github_latest
  end

  on_macos do
    on_arm do
      url "$REPOSITORY_URL/releases/download/$tag/taiga_${version}_darwin_arm64.tar.gz"
      sha256 "$darwin_arm64"
    end
    on_intel do
      url "$REPOSITORY_URL/releases/download/$tag/taiga_${version}_darwin_amd64.tar.gz"
      sha256 "$darwin_amd64"
    end
  end

  on_linux do
    on_arm do
      url "$REPOSITORY_URL/releases/download/$tag/taiga_${version}_linux_arm64.tar.gz"
      sha256 "$linux_arm64"
    end
    on_intel do
      url "$REPOSITORY_URL/releases/download/$tag/taiga_${version}_linux_amd64.tar.gz"
      sha256 "$linux_amd64"
    end
  end

  def install
    bin.install "taiga"
    bash_completion.install "completions/taiga.bash" => "taiga"
    zsh_completion.install "completions/_taiga"
    fish_completion.install "completions/taiga.fish"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/taiga version")
    # A missing API URL is the documented validation failure, which proves the
    # binary runs and reports the structured contract rather than crashing.
    assert_match "missing_api_url", shell_output("#{bin}/taiga --json project list 2>&1", 7)
  end
end
FORMULA
