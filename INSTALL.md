# Installing and upgrading

***English** · [繁體中文](INSTALL.zh-TW.md)*

## Install script

macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/install.ps1 | iex
```

The script detects your platform, resolves the latest **stable** release, downloads `SHA256SUMS`, and
**verifies the digest before installing**. A mismatch, or an archive missing from `SHA256SUMS`, aborts
and leaves any existing installation untouched. The default location is `~/.local/bin`, or
`%LOCALAPPDATA%\Programs\taiga` on Windows, where the directory is added to your user PATH.

On Windows, **open a new terminal** afterwards for the PATH change to take effect.

Choosing a version or location:

```sh
TAIGA_VERSION=v0.1.0 TAIGA_INSTALL_DIR=/usr/local/bin sh install.sh
```

Passing parameters on Windows means saving the script to a file first, and the PowerShell execution
policy blocks a `.ps1` downloaded from the internet by default. The `irm | iex` form above is
unaffected because it runs a string rather than a file, but a saved script needs an explicit
exemption:

```powershell
irm https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/install.ps1 -OutFile install.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1 -Version v0.1.0 -InstallDir C:\Tools\taiga
```

To avoid repeating `-ExecutionPolicy Bypass`, run `Unblock-File .\install.ps1` once to clear the
download mark.

## Homebrew (macOS and Linux)

```sh
brew install koukeneko/tap/taiga
```

Upgrading:

```sh
brew upgrade taiga
```

The tap tracks **stable releases only** and never installs a pre-release. The formula installs the
binary from the release archive, so macOS users get the signed and notarized executable, along with
Bash, Zsh, and Fish completions.

To try a pre-release, download the archive manually as described in the next section.

## Scoop (Windows)

```powershell
scoop bucket add koukeneko https://github.com/KoukeNeko/scoop-bucket
scoop install taiga
```

Upgrading:

```powershell
scoop update taiga
```

The manifest tracks the GitHub releases and auto-updates to new stable versions. Scoop verifies each
download against the release checksums and shims `taiga` onto your PATH.

## Linux packages (.deb and .rpm)

Every release attaches `.deb` and `.rpm` packages for x86-64 and ARM64, built from the same binaries
as the archives. Download the file for your architecture from the
[latest release](https://github.com/KoukeNeko/taiga-cli/releases/latest), then install it from the
directory you downloaded it into.

```sh
# Debian / Ubuntu
sudo apt install ./taiga-cli_*.deb

# Fedora / RHEL
sudo dnf install ./taiga-cli-*.rpm
```

`apt install ./…` and `dnf install ./…` are preferred over `dpkg -i` / `rpm -i` because they resolve
dependencies (this static binary has none, but the habit is safer). The package is named
**`taiga-cli`**; the installed command is `taiga`, at `/usr/bin/taiga`, with Bash, Zsh, and Fish
completions.

To upgrade, download and install the newer file when a release is out.

## Official release archives

Download the archive for your platform from the GitHub Release, together with `SHA256SUMS` from the
same version:

| Operating system | Architecture | Archive |
| --- | --- | --- |
| macOS | Intel | `taiga_<version>_darwin_amd64.tar.gz` |
| macOS | Apple silicon | `taiga_<version>_darwin_arm64.tar.gz` |
| Linux | x86-64 | `taiga_<version>_linux_amd64.tar.gz` |
| Linux | ARM64 | `taiga_<version>_linux_arm64.tar.gz` |
| Windows | x86-64 | `taiga_<version>_windows_amd64.zip` |
| Windows | ARM64 | `taiga_<version>_windows_arm64.zip` |

Verifying on Linux:

```sh
sha256sum --check SHA256SUMS
```

Verifying on macOS:

```sh
shasum -a 256 --check SHA256SUMS
```

On Windows PowerShell, use `Get-FileHash -Algorithm SHA256 <archive>` and compare against
`SHA256SUMS`. Once verified, extract the archive and move `taiga` (`taiga.exe` on Windows) into a
directory on your `PATH`. Every archive also carries the READMEs, the compatibility matrix, an SPDX
SBOM, and completions for four shells.

## macOS Gatekeeper

Released macOS binaries are signed with a Developer ID certificate and notarized by Apple, so they run
without any extra step. You can confirm that yourself:

```sh
codesign --verify --strict --verbose=2 ./taiga
spctl -a -vvv -t install ./taiga
```

`spctl` reporting `accepted` with `source=Notarized Developer ID` is the expected result.

A notarization ticket cannot be stapled to a bare executable, because `stapler` supports only `.app`,
`.dmg`, and `.pkg`. Gatekeeper therefore resolves it **online**. A first run with no network at all
can still be blocked; reconnect and run it again, or clear the quarantine attribute:

```sh
xattr -d com.apple.quarantine ./taiga
```

Files fetched with `curl` or `wget` are never quarantined, so this does not arise there. Either way,
verify the download against `SHA256SUMS` before running it.

## Shell completion

The `completions/` directory in each archive contains:

- Bash: `taiga.bash`
- Zsh: `_taiga`
- Fish: `taiga.fish`
- PowerShell: `taiga.ps1`

The install script writes them for you when a standard completion directory already exists, and
names each file it wrote. It never creates such a directory, since doing so would leave a stray
folder in the home directory of someone who does not use that shell. The uninstaller removes exactly
those files.

They can also be generated by hand:

```sh
taiga completion bash
taiga completion zsh
taiga completion fish
taiga completion powershell
```

## Upgrading

1. Read that version's release notes and [COMPATIBILITY.md](COMPATIBILITY.md).
2. Download and verify the new archive.
3. Replace the old binary. Config files and OS keyring credentials do not need to move.
4. Run `taiga version --json` to confirm the version, commit, and platform.
5. Run `taiga doctor --json` to confirm the API, authentication, and default project.

Downgrading is the same in reverse: put back a verified older binary. If the release notes mention a
configuration migration, back up the Taiga CLI config file in your operating system's user config
directory first.

## Uninstalling

Homebrew:

```sh
brew uninstall taiga
```

Scoop:

```powershell
scoop uninstall taiga
```

Everything else:

```sh
curl -fsSL https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/uninstall.sh | sh
```

```powershell
irm https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/uninstall.ps1 | iex
```

By default only the binary is removed. Configuration and the credential in your OS keyring are kept,
because uninstalling is often one step of an upgrade. Add `--purge` (`-Purge` on Windows) to remove
those as well, including anything left under the pre-rename name. `--dry-run` reports what would go
without changing anything.

The POSIX script detects a Homebrew installation and hands you back to `brew uninstall` rather than
deleting files Homebrew tracks.

A repository pinned with `taiga project use --local` keeps that setting in its own `.git/config`,
which no uninstaller can find. Clear it there with `git config --local --remove-section taiga`.

## Installing from source

Requires Go 1.25.13 or newer, which is the first 1.25 without known standard-library advisories:

```sh
make install PREFIX="$HOME/.local"
```

A source installation reports version `dev`. Real release metadata is injected only by the
reproducible packaging pipeline.
