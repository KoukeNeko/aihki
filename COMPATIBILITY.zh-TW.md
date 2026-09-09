# 相容性矩陣

*[English](COMPATIBILITY.md) · **繁體中文***

更新日期：2026-09-03

## Taiga server

| Server | 狀態 | 證據／限制 |
| --- | --- | --- |
| Taiga 6.10.2 | Verified | CI 使用固定 image digest 執行完整 Docker E2E，包括 auth、Project、敏捷流程、Wiki、附件、bulk、stats 與 export/import。 |
| Taiga 6.10.x | Expected compatible | API family 相同；仍應先執行 `taiga doctor`。 |
| 較早的 Taiga 6 | Unverified | 基礎 `/api/v1` 可能可用，但不承諾所有 endpoint 與 serializer 欄位。 |
| TaigaNext | Unsupported | API contract 不同，尚未提供相容層。 |

`project archive|unarchive` 目前仍回報 `unsupported_capability`，因已驗證的 Taiga REST contract 沒有 CLI 可安全使用的 archive action。

## 作業系統與架構

Release packaging 產生以下純 Go（`CGO_ENABLED=0`）binary：

- macOS `amd64`、`arm64`
- Linux `amd64`、`arm64`
- Windows `amd64`、`arm64`

CI 會在 Ubuntu、macOS、Windows 編譯，並在 Linux 驗證完整 release archive、checksum、SBOM、embedded version metadata 與 reproducibility。

macOS binary 以 Developer ID 憑證簽署並經 Apple notarization（Team ID `33832Z66QU`）。因 notarization 要求安全時間戳，darwin archive 不具位元可重現性；Linux 與 Windows archive 則有。

## Authentication

| 模式 | 狀態 |
| --- | --- |
| Taiga normal username/password | Verified |
| 既有 bearer token／`TAIGA_TOKEN` | Verified |
| Refresh token rotation | Verified by automated test |
| SSO／LDAP 外掛 | 不提供通用互動登入；可在站台允許時匯入既有 token |
| 以 GitHub、Google 等第三方登入的帳號 | 匯入 token：把網頁應用存在瀏覽器 Local Storage 的 `{"auth_token": …, "refresh": …}` 交給 `auth login --with-token`，refresh token 讓登入能自動更新。只貼 token 本身則效期為一個 access token 的壽命，預設的 Taiga 6 為 24 小時 |

## Machine contract migration

CLI JSON contract 目前為 `meta.contract: 1`。同一 contract 版本內只新增 optional field；移除、重新命名或改變既有欄位型別時，必須提升 contract version 並在 Release Notes 提供 migration 說明。

Diagnostic bundle format 目前為 `manifest.format: 1`，與 CLI JSON contract 分開演進。Bundle 不含 runtime identifier 或 secret，且永遠只在本機建立、不主動上傳。
