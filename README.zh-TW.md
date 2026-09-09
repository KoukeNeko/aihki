<p align="center">
  <img src="assets/hero.png" alt="Taiga CLI — an independent command-line client for Taiga" width="100%">
</p>

<h1 align="center">Taiga CLI</h1>

<p align="center">
  <strong>獨立的 Taiga 命令列客戶端。</strong><br>
  給人閱讀的終端輸出，以及給 Shell、CI 與 Agent 使用的穩定 JSON contract。
</p>

<p align="center">
  <a href="https://github.com/KoukeNeko/taiga-cli/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/KoukeNeko/taiga-cli?style=for-the-badge&logo=github&label=RELEASE&color=2196F3"></a>
  <a href="https://github.com/KoukeNeko/taiga-cli/releases"><img alt="Release downloads" src="https://img.shields.io/github/downloads/KoukeNeko/taiga-cli/total?style=for-the-badge&logo=github&label=DOWNLOADS&color=4CAF50"></a>
  <a href="https://github.com/KoukeNeko/taiga-cli/actions/workflows/ci.yml"><img alt="CI status" src="https://img.shields.io/github/actions/workflow/status/KoukeNeko/taiga-cli/ci.yml?branch=main&style=for-the-badge&logo=githubactions&logoColor=white&label=CI"></a>
  <a href="COMPATIBILITY.zh-TW.md"><img alt="Verified against Taiga 6.10.2" src="https://img.shields.io/badge/TAIGA-6.10.2_VERIFIED-00A5A5?style=for-the-badge"></a>
  <a href="LICENSE"><img alt="MIT licence" src="https://img.shields.io/badge/LICENSE-MIT-4CAF50?style=for-the-badge&logo=github"></a>
</p>

<p align="center">
  <a href="README.md">English</a> · <strong>繁體中文</strong>
</p>

<p align="center">
  <a href="INSTALL.zh-TW.md">安裝</a>
  · <a href="#快速開始">快速開始</a>
  · <a href="https://github.com/KoukeNeko/taiga-cli/wiki">使用手冊</a>
  · <a href="CHANGELOG.zh-TW.md">版本紀錄</a>
  · <a href="COMPATIBILITY.zh-TW.md">相容性</a>
</p>

```sh
taiga issue list
taiga issue create --subject "Fix token refresh" --type Bug
taiga issue close 42 --status Closed

# 同一份資料，給 script、CI job 或 agent
taiga issue view 42 --json --fields ref,subject,status,version
```

```json
{"data":{"ref":42,"status":"Closed","subject":"Fix token refresh","version":8},"meta":{"contract":1}}
```

一個獨立的 [Taiga 6](https://taiga.io/) 命令列客戶端，與 Taiga 專案並無隸屬關係。它讓你不必離開終端機
就能操作專案、敏捷流程與 Wiki，會自動從 frontend 的 `conf.json` 找出 API 位置，包括部署在 `/taiga/`
子路徑的站台，登入後把 token 交給作業系統 keyring 保管，不寫進設定檔。

**同一個指令同時服務人與程式。** 直接執行時輸出對齊的表格；加上 `--json` 就得到帶版本號的 contract，
搭配固定 exit code 與 JSON Schema descriptor，可以放心讓 Shell script、CI job 或 LLM agent 驅動。輸出格式
不會因為被 pipe 就偷偷改變 —— 要 JSON 就得明講。

**寫入行為可預測。** work item 帶版本號，而 Taiga 是逐欄位檢查的：當你要改的欄位已被別人改過時會被拒絕
而非默默覆蓋，但對方改的是別的欄位時則會合併。這個工具本身從不替你自動合併 —— 被拒絕就停下來，讓你重讀後
自己決定。只有 idempotent 的 GET 會自動重試；連線在寫入途中斷掉時回報 `ambiguous_commit`，要求你先確認，
而不是盲目重送。

## 能做什麼

### 完整的 Taiga 工作流

Project、Epic、User Story、Task、Issue、Sprint 與 Wiki 的日常操作都在裡面：列表、檢視、建立、編輯、
指派、留言、關閉、刪除。加上成員與角色權限、Webhook、Custom field、八類 workflow metadata、Swimlane、
Tag、Due-date preset，以及跨專案的 Epic ↔ Story 關聯。

工作項目可以用裸 ref、`project#ref` 或直接貼 Taiga 網址來指定，三種寫法都通：

```text
42
example-project#42
https://taiga.example.com/taiga/project/example-project/issue/42
```

### 給自動化的穩定介面

`--json` 輸出 `meta.contract` 版本號，`--fields` 挑選欄位，`taiga schema <command>` 給出該指令的
input/output JSON Schema 與 safety/idempotency 標註 —— agent 可以據此判斷一個指令能不能自動執行。
Exit code 依錯誤種類固定分流，`--dry-run` 會完整解析並顯示將送出的變更，但保證不發出任何寫入請求。

```text
$ taiga schema issue create
{"data":{"command":"issue create","safety":"write","idempotency":"non_idempotent",
         "input_schema":{...,"required":["subject"]},"output_schema":{...}},"meta":{"contract":1}}
```

agent 讀 `safety` 與 `idempotency` 決定能否自動執行，讀 schema 組出並驗證呼叫——不必去刮 `--help`。

### 不會意外破壞資料

刪除工作項目與 metadata 後會回讀確認；附件與 CSV 下載走 streaming、核對雜湊、以 `0600` 暫存檔原子落盤，
且預設不覆寫既有檔案。非互動模式下的破壞性操作一律要求明確的 `--yes`。Webhook secret、application
token 的 auth code 與 ownership transfer token 都不會出現在任何輸出或 dry-run 裡。

### 可以放心自動化

一個會謊報資料下場的包裝層，比沒有包裝層更糟，因為腳本會照著那個答案行動。由此推出三件事：

- **被拒絕的寫入與寫錯的寫入，靠結構分辨而不是靠字面。** Taiga 兩者都回 HTTP 400、掛在同一個鍵下，只差在值
  的形狀；而且那句話會被翻譯，所以比對字面既會誤判，在非英文語系的伺服器上更會完全失效。
- **結果不明的寫入會明講。** 中斷一個已經送出的請求並不會把它收回去，所以 CLI 回報 `ambiguous_commit`、
  要求你先確認，而不是宣稱一個它無法證明的失敗。
- **並行寫入是實測過的，不是假設的。** 一份 end-to-end 測試讓十二個帳號同時操作同一個專案，檢查任何兩次被
  接受的寫入都不會看到相同的版本號。

[並行與衝突](https://github.com/KoukeNeko/taiga-cli/wiki/Work-Items-zh-TW) 說明 Taiga 會拒絕什麼、會合併什麼。

### 多站台與多專案

Profile 讓你在不同 Taiga 站台之間切換，各自記住 API URL 與預設專案。也可以把 profile 與 project 綁在
單一 Git repository 上，存進 `.git/config` 而不會被 commit：

```sh
taiga project use example-project --local
```

### 出事的時候查得出來

`taiga doctor` 逐項檢查 frontend discovery、API、authentication 與預設專案。需要求助時，
`taiga doctor bundle` 產生一份可以安心分享的診斷包 —— 只有版本資訊、設定「是否存在」的布林值與
狀態碼，不含任何 URL、使用者名稱、專案名稱或憑證，而且只在本機建立、不會自動上傳。

### 如何比較

一支 binary，讓人、shell script、CI job 與 agent 都能共用——最低共同介面，不必再多跑一個服務。

|                        | Web UI | 一般 CLI | Taiga CLI | MCP server |
| ---------------------- | :----: | :------: | :-------: | :--------: |
| 終端機前的人           |   ✅   |    ✅    |    ✅     |     —      |
| Shell script           |   —    |    ✅    |    ✅     |     —      |
| CI pipeline            |   —    |    ⚠️    |    ✅     |     ⚠️     |
| AI agent               |   —    |    ⚠️    |    ✅     |     ✅     |
| 穩定 JSON contract     |   —    |    ⚠️    |    ✅     |     ✅     |
| 每個指令的 JSON Schema |   —    |    —     |    ✅     |   varies   |
| Dry-run                |   —    |    ⚠️    |    ✅     |   varies   |
| Conflict-safe 寫入     |  n/a   |  varies  |    ✅     |   varies   |
| 不需額外 daemon        |   —    |    ✅    |    ✅     |     —      |

## 快速開始

1. **安裝。** macOS 與 Linux 用 Homebrew：

   ```sh
   brew install koukeneko/tap/taiga
   ```

   Windows 用 [Scoop](https://scoop.sh)：

   ```powershell
   scoop bucket add koukeneko https://github.com/KoukeNeko/scoop-bucket
   scoop install koukeneko/taiga-cli
   ```

   Debian/Ubuntu 或 Fedora/RHEL 可加簽章的 APT/DNF 倉庫自動升級，或直接下載 `.deb` / `.rpm`——見
   [INSTALL.zh-TW.md](INSTALL.zh-TW.md#linux-套件deb-與-rpm)。

   或用安裝腳本，它會先核對 release checksum 才安裝：

   ```sh
   curl -fsSL https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/install.sh | sh
   ```

   ```powershell
   irm https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/install.ps1 | iex
   ```

   Release archive、手動驗證 checksum 與從原始碼建置見 [INSTALL.zh-TW.md](INSTALL.zh-TW.md)。
   Windows 安裝後請開新的終端機，PATH 變更才會生效。

2. **登入**。直接執行，回答兩個問題：你的 Taiga 裡任何一頁的網址（預設為官方託管的 Taiga）、你的帳號怎麼登入。
   token 會存進 OS keyring：

   ```sh
   taiga auth login
   ```

   要跳過第一個問題，用 `--url` 貼上 Taiga 網頁應用裡任何一頁的網址，例如專案或 backlog 頁面；填 API 的位址
   也可以，而且只會接觸你輸入的那個站台。官方託管的 Taiga 在 `https://tree.taiga.io/`；`community.taiga.io`
   是論壇，帳號系統不同，貼了它的網址時會改問你要不要用託管版。

   ```sh
   taiga auth login --url https://taiga.example.com/taiga/ --profile company
   ```

   用 GitHub 或 Google 登入的帳號沒有 Taiga 密碼。在第二個問題選那個選項，或直接加 `--with-token`，taiga
   會改用網頁應用持有的 token：先在網頁登入，在那個分頁打開瀏覽器的 JavaScript console，執行這行把兩個
   token 一起放進剪貼簿：

   ```js
   copy(JSON.stringify({auth_token: JSON.parse(localStorage.token), refresh: JSON.parse(localStorage.refresh)}))
   ```

   然後在提示處貼上，或用 pipe 傳入：

   ```sh
   pbpaste | taiga auth login --url https://tree.taiga.io/ --with-token
   ```

   物件裡的 refresh token 讓這個登入能像密碼登入一樣自動更新。只貼 token 本身也可以，但登入只會維持到該
   token 過期，預設的 Taiga 6 是 24 小時。

   這樣匯入的 token 沒有附帶 refresh token，會在伺服器的 access token 過期時失效，預設的 Taiga 6 是 24 小時。
   `TAIGA_TOKEN` 是給 script 用的同一件事。

3. **選定專案**：

   ```sh
   taiga project list
   taiga project use example-project
   ```

4. **開始操作**：

   ```sh
   taiga issue list
   taiga issue create --subject "Fix token refresh" --type Bug
   taiga issue assign 42 --to alice
   taiga issue close 42 --status Closed
   ```

5. **接上自動化**：

   ```sh
   taiga issue view 42 --json --fields id,ref,subject,status,version --no-input
   ```

完整的指令參考、旗標說明與各子系統的行為細節，見
[使用手冊 Wiki](https://github.com/KoukeNeko/taiga-cli/wiki)，其中包含給 CI、shell 腳本與 agent 的
[自動化實例](https://github.com/KoukeNeko/taiga-cli/wiki/Automation-Recipes-zh-TW)。

## 相容性

- Taiga 6.10.2 已透過固定 image digest 的 Docker E2E 驗證
- macOS、Linux、Windows 的 `amd64` 與 `arm64`，純 Go 建置（`CGO_ENABLED=0`）
- 帳密登入、既有 bearer token 與 refresh token rotation

詳細矩陣與已知限制見 [COMPATIBILITY.zh-TW.md](COMPATIBILITY.zh-TW.md)。

---

## Technical reference

### 設定優先序

一般設定放在作業系統的使用者設定目錄，token 不會寫入設定檔：

```toml
current_profile = "company"

[profiles.company]
api_url = "https://taiga.example.com/taiga/api/v1/"
project = "example-project"
```

解析順序由高到低：

```text
command flag
→ TAIGA_PROFILE / TAIGA_API_URL / TAIGA_PROJECT / TAIGA_TOKEN
→ Git-local taiga.profile / taiga.project
→ current profile
→ safe defaults
```

### JSON contract

成功資料只寫 stdout，錯誤只寫 stderr。單筆用 `data`，列表用 `items` 與 `page`，兩者都帶 `meta.contract`：

```json
{
  "data": { "id": 123, "ref": 42, "subject": "Fix token refresh", "version": 7 },
  "meta": { "contract": 1 }
}
```

同一個 contract 版本內只會新增 optional 欄位；移除、改名或改變既有欄位型別必須提升版本並附遷移說明。

| Exit code | Meaning | 意義 |
| ---: | --- | --- |
| 0 | success | 成功 |
| 1 | unexpected internal failure | 未預期的內部錯誤 |
| 2 | usage / schema | 用法或 schema 錯誤 |
| 3 | authentication | 認證失敗 |
| 4 | forbidden | 權限不足 |
| 5 | not found | 找不到目標 |
| 6 | OCC conflict | 樂觀鎖衝突 |
| 7 | validation / ambiguity | 驗證失敗或指涉不明確 |
| 8 | throttled | 被限流 |
| 9 | transport / upstream | 傳輸或上游錯誤 |
| 10 | confirmation required | 需要確認 |
| 11 | ambiguous commit | 提交結果不明 |
| 130 | interrupted before finishing | 在完成前被中斷 |

`1` 代表 CLI 遇到了它沒有分類的狀況，值得當成 bug 回報。`130` 採用 shell 慣例的 128 加訊號編號，而不是在上
表裡再佔一個號碼，因為停掉一個指令是一個決定，不是指令失敗的一種方式；若中斷打斷的是進行中的寫入，回報的
會是 `11`。

### 安全原則

- 不接受 command-line password
- Authorization、password、token 不會出現在 verbose log
- 只有 GET 會進行有上限的自動重試，POST／PATCH 不會盲目重送
- 寫入途中連線中斷且結果不明時回報 `ambiguous_commit`
- OCC conflict 不會自動 merge 或覆寫
- 附件下載不會把 API bearer token 送往 media URL
- TLS verification 預設永遠開啟

### 開發與測試

不需要 Docker 的快速迴圈：

```sh
make test
make test-race
make lint
```

對真實 Taiga server 的 integration test：

```sh
make test-integration
```

Integration harness 使用獨立的 `taiga-cli-e2e` Compose project 與 `localhost:19000`，自行建立臨時帳號、
專案與 Issue，結束後只清除自己的 container 與 volume，不會動到日常使用的 Taiga 實例。

重建跨平台 release artifacts：

```sh
make release \
  VERSION=v0.1.0 \
  COMMIT="$(git rev-parse HEAD)" \
  SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)"
```

相同的 source、Go toolchain、version、commit 與 epoch 會產生位元完全相同的 Linux 與 Windows archive。
維護者的發布流程見 [RELEASING.md](RELEASING.md)。
macOS 是例外：notarization 要求 Apple 簽發的安全時間戳，因此 Developer ID 簽章本質上無法重現；除簽章外
其餘內容的建置方式完全相同。

<p>
  <img alt="Cobra" src="https://img.shields.io/badge/COBRA-CLI-00ADD8?style=for-the-badge&logo=go&logoColor=white">
  <img alt="Reproducible builds" src="https://img.shields.io/badge/BUILDS-REPRODUCIBLE-4CAF50?style=for-the-badge">
  <img alt="SPDX 2.3 SBOM" src="https://img.shields.io/badge/SBOM-SPDX_2.3-2196F3?style=for-the-badge">
  <img alt="Codacy code quality" src="https://img.shields.io/codacy/grade/b8f361063a874af3be58396ac7c7ad27?branch=main&style=for-the-badge&logo=codacy&label=CODE%20QUALITY">
</p>

## 贊助

如果 Taiga CLI 對你有幫助，可以考慮贊助開發者:

<a href="https://buymeacoffee.com/doershing"><img alt="Buy Me a Coffee" src="https://img.shields.io/badge/Buy%20Me%20a%20Coffee-doershing-FFDD00?style=for-the-badge&logo=buymeacoffee&logoColor=black"></a>

## 商標

Taiga 為其各自權利人之商標。本專案是獨立的客戶端，與 Taiga 專案及其維護者並無隸屬關係，未經其背書或
贊助；使用該名稱僅為描述本工具所搭配的軟體。Taiga 團隊已在[社群論壇](https://community.taiga.io/t/aihki-a-cli-client-for-taiga-plus-a-naming-question/8947)上確認，一個規模不大、獨立的第三方客戶端以此方式命名並無問題。

## License

[MIT](LICENSE) © KoukeNeko
