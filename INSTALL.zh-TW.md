# 安裝與升級

*[English](INSTALL.md) · **繁體中文***

## 安裝腳本

macOS 與 Linux：

```sh
curl -fsSL https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/install.sh | sh
```

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/install.ps1 | iex
```

腳本會偵測平台、抓取最新**正式版**、下載 `SHA256SUMS` 並**核對雜湊後才安裝** —— 雜湊不符或檔案未列於
`SHA256SUMS` 都會中止並保留原有安裝。預設安裝位置為 `~/.local/bin`（Windows 為
`%LOCALAPPDATA%\Programs\taiga`，並自動加入使用者 PATH）。

Windows 安裝後需要**開一個新的終端機**，使用者 PATH 的變更才會生效。

指定版本或安裝位置：

```sh
TAIGA_VERSION=v0.1.0 TAIGA_INSTALL_DIR=/usr/local/bin sh install.sh
```

Windows 要傳參數就必須先把腳本存成檔案，而 PowerShell 的執行原則預設會封鎖從網路下載的 `.ps1`。上面
`irm | iex` 的寫法不受影響（它執行的是字串而非檔案），但存檔後執行需要明確放行：

```powershell
irm https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/install.ps1 -OutFile install.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1 -Version v0.1.0 -InstallDir C:\Tools\taiga
```

若不想每次都加 `-ExecutionPolicy Bypass`，也可以先用 `Unblock-File .\install.ps1` 移除下載標記。

## Homebrew（macOS 與 Linux）

```sh
brew install koukeneko/tap/taiga
```

升級：

```sh
brew upgrade taiga
```

Tap 只追蹤**正式版**，不會安裝 pre-release。Formula 安裝的是 release archive 中的 binary，因此 macOS
使用者得到的就是已簽署並 notarize 的執行檔，同時會一併安裝 Bash、Zsh 與 Fish 的 completion。

要試用 pre-release 請依下一節手動下載 archive。

## Scoop（Windows）

```powershell
scoop bucket add koukeneko https://github.com/KoukeNeko/scoop-bucket
scoop install taiga
```

升級：

```powershell
scoop update taiga
```

Manifest 追蹤 GitHub releases 並自動更新到新的正式版。Scoop 會用 release checksum 核對每次下載，並把
`taiga` 加進 PATH。

## Linux 套件（.deb 與 .rpm）

每個 release 都附上 x86-64 與 ARM64 的 `.deb` 與 `.rpm`，與 archive 使用相同的 binary 建置。可從簽章的
APT/DNF 倉庫安裝（`apt upgrade` / `dnf upgrade` 會自動更新），或直接下載單一檔案。套件名稱為 **`taiga-cli`**，
指令為 `taiga`，裝在 `/usr/bin/taiga`，並附上 Bash、Zsh、Fish completion。

### 倉庫（自動升級）

Debian / Ubuntu：

```sh
sudo install -d /etc/apt/keyrings
sudo curl -fsSL https://koukeneko.github.io/taiga-cli/taiga-cli.gpg.key -o /etc/apt/keyrings/taiga-cli.gpg.key
echo "deb [signed-by=/etc/apt/keyrings/taiga-cli.gpg.key] https://koukeneko.github.io/taiga-cli/deb stable main" | sudo tee /etc/apt/sources.list.d/taiga-cli.list
sudo apt update && sudo apt install taiga-cli
```

Fedora / RHEL：

```sh
sudo tee /etc/yum.repos.d/taiga-cli.repo >/dev/null <<'EOF'
[taiga-cli]
name=Taiga CLI
baseurl=https://koukeneko.github.io/taiga-cli/rpm
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=https://koukeneko.github.io/taiga-cli/taiga-cli.gpg.key
EOF
sudo dnf install taiga-cli
```

倉庫 metadata 與每個套件都經 GPG 簽章。新版透過一般的 `apt upgrade` / `dnf upgrade` 取得。

### 直接下載

到 [最新 release](https://github.com/KoukeNeko/taiga-cli/releases/latest) 下載你架構對應的檔案，再於下載目錄
安裝：

```sh
# Debian / Ubuntu
sudo apt install ./taiga-cli_*.deb

# Fedora / RHEL
sudo dnf install ./taiga-cli-*.rpm
```

直接安裝不會自動升級；有新版時下載新檔重裝即可。

## 官方 release archive

從 GitHub Release 下載符合平台的 archive，以及同一版本的 `SHA256SUMS`：

| 作業系統 | 架構 | Archive |
| --- | --- | --- |
| macOS | Intel | `taiga_<version>_darwin_amd64.tar.gz` |
| macOS | Apple silicon | `taiga_<version>_darwin_arm64.tar.gz` |
| Linux | x86-64 | `taiga_<version>_linux_amd64.tar.gz` |
| Linux | ARM64 | `taiga_<version>_linux_arm64.tar.gz` |
| Windows | x86-64 | `taiga_<version>_windows_amd64.zip` |
| Windows | ARM64 | `taiga_<version>_windows_arm64.zip` |

Linux 驗證：

```sh
sha256sum --check SHA256SUMS
```

macOS 驗證：

```sh
shasum -a 256 --check SHA256SUMS
```

Windows PowerShell 可用 `Get-FileHash -Algorithm SHA256 <archive>`，並與 `SHA256SUMS` 對照。驗證後解壓縮，將 `taiga`（Windows 為 `taiga.exe`）移到 `PATH` 中的目錄。每個 archive 也包含 README、相容性文件、SPDX SBOM 與四種 shell completion。

## macOS Gatekeeper

Release 的 macOS binary 已用 Developer ID 憑證簽署並通過 Apple notarization，正常情況下直接執行即可，
不需要任何額外步驟。可自行確認：

```sh
codesign --verify --strict --verbose=2 ./taiga
spctl -a -vvv -t install ./taiga
```

`spctl` 顯示 `accepted` 且 `source=Notarized Developer ID` 即為正常。

Notarization ticket 無法 staple 到裸執行檔（`stapler` 只支援 `.app`、`.dmg`、`.pkg`），因此 Gatekeeper 會
**線上**查驗。若首次執行時完全沒有網路，仍可能被擋；連上網路後再執行一次即可，或移除隔離屬性：

```sh
xattr -d com.apple.quarantine ./taiga
```

以 `curl` 或 `wget` 下載的檔案不會被加上隔離屬性，本來就不會遇到這個情況。無論哪種方式，都應先用
`SHA256SUMS` 驗證檔案完整性再執行。

## Shell completion

Archive 的 `completions/` 包含：

- Bash：`taiga.bash`
- Zsh：`_taiga`
- Fish：`taiga.fish`
- PowerShell：`taiga.ps1`

安裝腳本會在**標準 completion 目錄已存在**時自動寫入，並逐一告知寫到哪裡。它不會建立這些目錄 —— 替沒在
用該 shell 的人建一個空資料夾只是製造垃圾。解除安裝時會精準移除這些檔案。

也可以自行產生：

```sh
taiga completion bash
taiga completion zsh
taiga completion fish
taiga completion powershell
```

## 升級

1. 先閱讀該版本 Release Notes 與 [COMPATIBILITY.zh-TW.md](COMPATIBILITY.zh-TW.md)。
2. 下載並驗證新 archive。
3. 以新 binary 取代舊 binary；設定檔與 OS keyring credential 不需搬移。
4. 執行 `taiga version --json` 確認版本、commit 與平台。
5. 執行 `taiga doctor --json` 確認 API、authentication 與預設 Project。

降級時同樣只需換回已驗證的舊 binary。若 Release Notes 標示設定 migration，應先備份作業系統使用者設定目錄中的 Taiga CLI 設定檔。

## 解除安裝

Homebrew：

```sh
brew uninstall taiga
```

Scoop：

```powershell
scoop uninstall taiga
```

其他方式：

```sh
curl -fsSL https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/uninstall.sh | sh
```

```powershell
irm https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/uninstall.ps1 | iex
```

預設只移除執行檔，**設定與 OS keyring 中的憑證會保留** —— 因為解除安裝常常只是升級的其中一步。加上
`--purge`（Windows 為 `-Purge`）才會一併移除，含改名前舊名稱留下的資料。`--dry-run` 只列出將移除的項目，
不做任何變更。

POSIX 腳本會偵測 Homebrew 安裝並引導你改用 `brew uninstall`，而不是直接刪掉 Homebrew 管理的檔案。

以 `taiga project use --local` 綁定的 repository，設定存在該 repo 自己的 `.git/config` 裡，任何解除安裝
程式都找不到。請在該 repo 執行 `git config --local --remove-section taiga` 清除。

## 從原始碼安裝

需要 Go 1.25.13 或更新版本 —— 那是 1.25 系列中第一個沒有已知標準函式庫弱點的版本：

```sh
make install PREFIX="$HOME/.local"
```

原始碼安裝預設顯示 `dev` 版本。正式 release metadata 只由可重現 packaging 流程注入。
