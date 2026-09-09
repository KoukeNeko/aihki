# 發行流程

## 前置條件

- `main` 與 `origin/main` 同步且工作樹乾淨。
- Release commit 已用 Git config 的 signing key 簽署，`git verify-commit HEAD` 與 `git log --format='%H %G? %s' -1` 都通過。
- `make verify` 與 `make test-integration` 通過。
- Version 使用 `vMAJOR.MINOR.PATCH`；pre-release 可使用 SemVer suffix。
- Repository 已有經 owner 選定的 `LICENSE`（目前為 MIT）；workflow 會在缺少此檔時主動拒絕發布。

## 本機重建套件

```sh
version=v0.1.0
commit="$(git rev-parse HEAD)"
epoch="$(git show -s --format=%ct HEAD)"
make release VERSION="$version" COMMIT="$commit" SOURCE_DATE_EPOCH="$epoch"
```

輸出位於 `dist/<version>/`：六個平台 archive、SPDX 2.3 SBOM 與 `SHA256SUMS`。Release build 固定使用 `CGO_ENABLED=0`、`-trimpath`、`-buildvcs=false`、空 Go build ID、排序後的 archive entry，以及 commit timestamp。相同 source、Go toolchain、version、commit 與 epoch 應產生相同 bytes。

macOS archive 是唯一例外：release 會以 Developer ID 憑證簽署並送交 notarization，而 notarization 要求
安全時間戳，因此 darwin archive 每次建置的 bytes 都不同。Linux 與 Windows archive 不受影響，CI 仍逐位元
驗證 Linux archive。未提供簽署設定時（例如本機重建）darwin archive 同樣是可重現的。

Packager 只接受不存在或空的 output directory，避免先前版本或失敗建置的 stale asset 被上傳。若要重建同一版本，先將舊目錄移到備份位置，再重新執行。

## 建立 signed tag

禁止使用 lightweight 或未簽署 tag：

```sh
git tag -s "$version" -m "Taiga CLI $version"
git verify-tag "$version"
git push origin "$version"
```

Tag workflow 會透過 GitHub API 要求 annotated tag 與其 commit 的 `verification.verified` 都為 true，然後重新測試、封裝、驗證 checksums 與 embedded metadata，最後才執行 `gh release create --verify-tag --generate-notes`。推送 tag 前應先確認版本號，因發布後的 release asset 不應被覆寫。

## Pre-release

Pre-release 使用 SemVer suffix，流程與正式版完全相同：

```sh
version=v0.1.0-rc.1
git tag -s "$version" -m "Taiga CLI $version"
git verify-tag "$version"
git push origin "$version"
```

Workflow 會偵測 tag 名稱中的 `-` suffix 並加上 `gh release create --prerelease`，因此 pre-release 不會被標記為
Latest，也不會出現在 repository 首頁的 release 位置。缺少這個判斷時 `gh` 會把 pre-release 當成正式版發布。

Pre-release 的 asset、checksum、SBOM 與版本 metadata 驗證方式與正式版一致。

## Homebrew tap

正式版 tag 發布成功後，release workflow 會自動以 `scripts/render-homebrew-formula.sh` 產生 formula 並
推送到 [`KoukeNeko/homebrew-tap`](https://github.com/KoukeNeko/homebrew-tap) 的 `Formula/taiga.rb`。

- 這一步需要 repository secret `HOMEBREW_TAP_TOKEN`，其權限只需對 `homebrew-tap` 有 `contents: write`。
- Pre-release **不會**更新 tap，因為 `brew install taiga` 不應解析到 release candidate。
- Formula 內容與 release archive 的 SHA256SUMS 綁定，四個平台（darwin/linux × arm64/amd64）各自釘住
  自己的 archive 與雜湊。
- 需要在不發新版的情況下修正 formula，或替既有版本補上 formula 時，手動執行 `Update Homebrew tap`
  workflow 並指定版本號。

## winget

winget 套件不會隨 release 自動更新，每次**正式版**發布後手動送一次 manifest 更新到 [`microsoft/winget-pkgs`](https://github.com/microsoft/winget-pkgs)。套件識別碼為 `KoukeNeko.TaigaCLI`。

Manifest 位於 `manifests/k/KoukeNeko/TaigaCLI/<version>/`，共四個檔：`KoukeNeko.TaigaCLI.yaml`（version）、`KoukeNeko.TaigaCLI.installer.yaml`、`KoukeNeko.TaigaCLI.locale.en-US.yaml`、`KoukeNeko.TaigaCLI.locale.zh-TW.yaml`。改版時只需更新：

- `PackageVersion`（四個檔一致）。
- installer 的兩個 Windows zip：`InstallerUrl`（指向新 tag）與 `InstallerSha256`（取自 release 的 `SHA256SUMS`，習慣用大寫）。
- installer 的 `NestedInstallerFiles.RelativeFilePath`：`taiga_<version>_windows_<arch>\taiga.exe`，`PortableCommandAlias` 為 `taiga`。
- locale 的 `ReleaseNotesUrl`。

雜湊取法：

```sh
gh release download "$version" --repo KoukeNeko/taiga-cli --pattern SHA256SUMS --output - | grep windows
```

送出（在 Windows 上，`winget` 與 `wingetcreate` 為 Windows 工具）：

```
winget validate --manifest <manifest 資料夾>
wingetcreate submit --token <github-token> <manifest 資料夾>
```

或 fork `microsoft/winget-pkgs`，把四個檔放到上述路徑後開 PR。

- 只在**正式版**送 winget；pre-release 不送。
- 每次改版是**新開一個提交**（PackageVersion 不同的新資料夾），不是改舊 PR。
- zip 走 `zip` + `portable` nested installer，指向 zip 內的 `taiga.exe`。
- 送出後 PR 需等 winget-pkgs moderator 核准（會有 msftbot 留言說明），不代表失敗。

## Scoop

Scoop bucket 在 [`KoukeNeko/scoop-bucket`](https://github.com/KoukeNeko/scoop-bucket)，manifest 為 `bucket/taiga-cli.json`（Scoop 官方 `extras` 已有無關的 `taiga`，故命名 `taiga-cli`，安裝用 bucket 限定名）。

- manifest 已內建 `checkver`（追 GitHub releases）與 `autoupdate`（依 `v$version` 組 URL、雜湊取自 release 的 `SHA256SUMS`），改版時可用 Scoop 的更新工具（`checkver`/Excavator）自動 bump，或手動更新 `version` 與兩個平台的 `hash`。
- 只追**正式版**；zip 是巢狀結構，靠 `extract_dir: taiga_<version>_windows_<arch>` 攤平後 `bin: taiga.exe` 上 PATH。
- 使用者：`scoop bucket add koukeneko https://github.com/KoukeNeko/scoop-bucket` 後 `scoop install koukeneko/taiga-cli`。

## APT/DNF 倉庫

**Package repo** workflow（`.github/workflows/package-repo.yml`）在 Release 成功後自動重建簽章的 APT/DNF 倉庫並部署到 GitHub Pages（`https://koukeneko.github.io/taiga-cli/`），讓使用者能 `apt`/`dnf` 安裝並自動升級。

- 依賴 repo secret `APT_GPG_PRIVATE_KEY`（armored、無 passphrase 的 GPG 私鑰）與 Pages 設為 **GitHub Actions** 部署。
- 每次重建會抓**所有正式版**的 .deb/.rpm 重建整個倉庫；pre-release 不納入。
- 建置腳本 `scripts/build-package-repo.sh` 產 APT（suite `stable`、component `main`、amd64+arm64）與 DNF metadata，並簽章 Release、repomd 與每個 .rpm；公鑰輸出為 `taiga-cli.gpg.key`。
- 不隨 release 時要重建（換金鑰、修 metadata），到 Actions 手動跑 **Package repo**。
- 金鑰輪替：產新金鑰、更新 `APT_GPG_PRIVATE_KEY`、手動重跑；使用者下次 `apt`/`dnf` 更新時取得新公鑰。

## 發布後驗證

```sh
gh release view "$version"
gh release download "$version" --dir /tmp/taiga-release-check
cd /tmp/taiga-release-check
sha256sum --check SHA256SUMS
```

至少在一個下載 archive 執行 `taiga version --json` 與 `taiga doctor --json`。若 artifact 或簽章不正確，不要以同名檔案覆寫已發布 asset；應撤回 release、調查後使用新的版本號。
