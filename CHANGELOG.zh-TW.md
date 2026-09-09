# Changelog

*[English](CHANGELOG.md) · **繁體中文***

本檔案記錄每個版本的重要變更。格式參考 [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)，版本號遵循 [Semantic Versioning](https://semver.org/spec/v2.0.0.html)。

Release workflow 會把對應版本的段落與英文版 [CHANGELOG.md](CHANGELOG.md) 的同一段落一起發布為 GitHub Release 說明，因此兩份都必須記錄每個已發布的版本。

## [未發布]

## [0.6.0] - 2026-09-09

### 改回 Taiga CLI

專案重新命名回 **Taiga CLI**，執行檔為 `taiga`。Taiga 團隊在[社群論壇](https://community.taiga.io/t/aihki-a-cli-client-for-taiga-plus-a-naming-question/8947)上確認：一個明確獨立、規模不大的 side project 以 Taiga 之名描述自己並不成問題；他們在意的是已退役的「TaigaNext」名稱，而不是第三方 client。用來描述此 client 連線對象的名稱，如今也成為 client 自己的名稱，難打的 `aihki` 指令就此走入歷史。

0.2.0 改名時引入的每個識別字都還原成原本的樣子。keyring service 與設定目錄為 `taiga-cli`，在 `.git/config` 裡綁定的 repository 讀取 `taiga.profile` 與 `taiga.project`，環境變數覆寫使用 `TAIGA_` 前綴，而 OS keyring、設定檔與 Git-local 段都直接使用這些名稱，不再經過 fallback。

0.2.0 為了把 Aihki 時期的設定往前搬而加入的單向相容轉換已移除，不再往另一個方向保留，因此在 `aihki` 底下儲存的登入不會被搬移；請執行一次 `taiga auth login`。**你需要調整的地方：** 以 `brew install koukeneko/tap/taiga` 取代 Aihki 的 formula，release archive 命名為 `taiga_<version>_<os>_<arch>`，completion 檔為 `taiga.bash`、`_taiga`、`taiga.fish` 與 `taiga.ps1`。

## [0.5.2] - 2026-09-08

### 變更

- 以 token 登入而沒有帶到 refresh token 時，現在會說明怎麼補。原本只告知這組憑證會過期，而只講這件事等於把人留在原地；因此該提示現在附上一次複製兩個 token 的 console 指令——就是精靈給第三方登入帳號的那一行。

## [0.5.1] - 2026-09-08

### 修正

- 0.2.0 的更名說明寫著 Taiga 維護者曾要求分家專案放棄該名稱，但公開紀錄裡並沒有這件事。有紀錄的是：Taiga 製作方改用 MPL-2.0 是為了取得對商標的更多控制權，以及後繼專案交由另一家公司接手時 Kaleidos 保留了品牌、該專案改以自己的名字發布。該段落現在只陳述這些，既查得到，也是本專案該有自己名字的更有力理由。

## [0.5.0] - 2026-09-07

兩件呼叫端原本不開口就不會知道的事，現在都有答案了。`aihki auth login` 即使 profile 已經存過網址，仍會先問要登入哪一個 Taiga，換到另一個 Taiga 不再需要知道 `--url`。而 `aihki schema` 會描述列表項目與單筆回應裡的欄位，由指令實際輸出的型別推導，並標示哪些是可選的，於是「某一列沒有這個欄位」不會被讀成「這個指令沒有這個欄位」。圍繞著這兩件事：表格若只裝得下一頁會直說，參數錯誤會附上該指令的 usage，根層說明則指引無人值守的呼叫端去看 `schema`。

### 新增

- `aihki schema` 現在會描述列表裡裝的是什麼，以及單筆回應裡有什麼，而不只是說「這是一個列表」。每個列表指令的 `output_schema` 都帶著單一項目的形狀——編碼後的欄位名、型別，以及哪些欄位一定會被寫出——由該指令實際輸出的型別推導而來，因此不會與真正送出的內容脫節。其中「可選」的資訊最關鍵：`story history` 會聲明 `comment` 是一個可能缺席的欄位，而這正是「這一列沒有留言」與「這個指令沒有這個欄位」的差別，只讀一列是分不出來的。`tag list` 與 `integration providers` 的每一列是就地組出來的，沒有型別可依據，因此仍然只描述外層信封。 同樣的形狀現在也涵蓋回應單一項目的指令：`story view`、`project view`、`stats project`、所有 `watch` 與 `vote`，以及其他三十幾個，都帶著它們送出的欄位。至於把回應就地組成 map 的指令仍然只描述外層信封，因為沒有型別可以據以推導。
- 互動式 `auth login` 收到的網址在 `taiga.io` 底下卻不是網頁應用（最常見的是社群論壇）時，會提議改用 `https://tree.taiga.io/`，回答 yes 就接續登入。在回答之前不會接觸你輸入以外的任何站台；script 仍然會拿到錯誤，因為不該有任何東西替它決定目的地。
- `auth login --with-token` 除了單一 token，也接受網頁應用持有的 JSON 物件 `{"auth_token": …, "refresh": …}`。帶著 refresh token 的匯入登入能像密碼登入一樣自動更新，不再於 access token 過期時失效；精靈與 README 提供一次複製兩者的 console 指令。結果會標示是否存入了 refresh token。

### 變更

- CLI 現在會直接說明它期待什麼，而不是留給你去查。參數錯誤會附上該指令的 usage，所以 `custom-field list` 會指名 `<epic|story|task|issue>`，不再只是數少了幾個；`--fields` 的說明寫明傳一個不存在的名稱就會列出可用欄位，而那份清單是唯一完整的來源——取樣時剛好沒出現的欄位仍然會列在裡面；根層說明則會指引無人值守的呼叫端去看 `aihki schema <command>`，取得該指令的 schema、safety 與 idempotency。這三點都是實際操作這個 CLI 時走錯的路，現在都在走錯的當下回答，而不是寫進 README。
- 在終端機執行 `auth login` 時，即使 profile 已經存過網址，現在仍會先問要登入哪一個 Taiga，並以存好的網址作為預設值，按 Enter 就沿用，而且不會接觸任何站台。登入正是有人要換到另一個 Taiga 的時機，在此之前存好的網址會被無聲沿用，只有知道 `--url` 的人改得動。這次執行以 `--api-url` 或環境變數指名的 API 網址仍然照用不問；script 沒有對象可問，會沿用存好的網址。

### 修正

- 表格若只裝得下整份清單的其中一頁，現在會在 stderr 說明，格式為 `showing 30 of 54; use --limit to see more`。在此之前，表格在頁面大小處停住的樣子跟清單真的到此為止一模一樣，於是 `story history`、`story list` 以及其他所有分頁表格都在無聲地誘導你用一部分資料下結論。提示不會寫到 stdout，表格照樣可以接管線；整份都顯示得下時不會出現；`--quiet` 之下靜音。JSON 輸出不變，它本來就帶著 `page`。
## [0.4.0] - 2026-09-03

這一版全部關於第一次登入。`aihki auth login` 現在會問你的 Taiga 裡任何一頁的網址（預設為官方託管的 Taiga），再問帳號怎麼登入；用 GitHub 或 Google 的帳號會被帶去貼 token，而不是被要求一個它沒有的密碼。`--url` 取代 `--host`（舊旗標仍可用），在終端機貼上 token 後按 Enter 即可，不再需要 Ctrl-D。

### 新增

- 直接執行 `aihki auth login` 會問兩個一般人答得出來的問題：你的 Taiga 裡任何一頁的網址（預設為官方託管的 Taiga，直接按 Enter 即可），以及你的帳號怎麼登入（密碼、GitHub 或 Google 等第三方、既有的 token）。在要求任何憑證之前，會先顯示憑證將送往的站台與 API；script 沒給網址時會被告知要傳 `--url`，而不是被提問。
- `--url` 接受 Taiga 網頁應用裡任何一頁的網址，例如專案或 backlog 頁面。程式會在該路徑及其上層逐層尋找網頁應用的 `conf.json`，它指名的 API 必須以 Taiga 的方式回應才算數；API 本身的位址也接受。只會接觸你輸入的那個站台：站台內的 redirect 會跟隨，導向其他站台的 redirect 會被回報，而且不會帶著任何憑證送出。找不到 Taiga 時，錯誤會列出試過的每一個網址與各自的回應、說明沒有送出任何憑證，`taiga.io` 底下的主機則會被指名託管的網頁應用在 `https://tree.taiga.io/`。
- 密碼被拒絕時，現在會說明用 GitHub、Google 等第三方登入的帳號沒有密碼，並給出改用 `--with-token` 的指令。

### 變更

- `auth login` 與 `doctor` 指定 Taiga 站台的旗標改為 `--url`；`--host` 仍可用，並會提示改用 `--url`。這個旗標接受的是帶 scheme 與路徑的 URL，叫 host 名不符實。

### 修正

- 在終端機執行 `auth login --with-token` 時會提示輸入 token，並像密碼一樣不回顯地讀取一行，貼上後按 Enter 即可。原本會等到輸入結束，在終端機上那代表 Ctrl-D，看起來就像卡住。從 pipe 輸入的行為不變。

## [0.3.2] - 2026-09-03

針對請求層的修正，來自對照真實 Taiga 的一輪審查。`aihki` 執行檔改變的是它回報的內容，不是它送出的東西：傳輸不再於三十秒後被切斷；不是 Taiga 拒絕的 refresh 失敗會照實回報；每一個讀回驗證的刪除共用同一份實作；指向非 Taiga 網頁應用的 `--host` 會被指名。

### 修正

- 中斷 `role delete`、`swimlane delete`、`due-date delete` 或工作流程狀態刪除時，不再回報「什麼都沒發生」。這四個操作沒有走共用的 `Delete` 包裝 —— 它們帶有 `moveTo` query，或共用「刪除後再驗證」的輔助函式 —— 因此直接呼叫請求層。0.3.0 把「這是不是 GET」的旗標改成由每次呼叫自行宣告「這個請求可能提交什麼」時，這三處呼叫仍然傳入 `false`：它原本的意思是「一般的 GET」，改版後卻變成「這不會提交任何東西」。於是刪除途中按 Ctrl-C 會以 130 離開並宣稱指令被中斷，刪除途中斷線則以 9 離開並標記為可重試，但那個刪除 Taiga 很可能已經執行了。現在這四個操作都和其他寫入一樣回報 `ambiguous_commit` 並以 11 離開；該旗標也不再是 bool，同樣的錯誤再犯會直接無法編譯。
- Taiga 換發 token 後，新 token 若寫不進作業系統鑰匙圈，指令現在會說明已儲存的憑證已經失效並請你執行 `aihki auth login`，而不是重複回報觸發換發的那個 `401`。Taiga 在簽發新 token 的同時就會作廢舊的 refresh token，所以磁碟上的登入從那一刻起就已經死了；這段說明文字 0.3.0 就寫好了，但請求層一直把它丟掉，改回報原本的拒絕 —— 只寫著「expired」，讓人無從得知下一次為什麼還是失敗。
- 留在改名前位置的設定檔若格式錯誤，錯誤訊息現在會指向它自己的路徑。原本的解析錯誤指向目前的位置，而那個檔案要到下次儲存才會存在。
- 附件上傳與下載、CSV 匯出與專案 dump 不再於三十秒後被切斷。HTTP client 原本帶有涵蓋整個傳輸的整體期限，所以線路在這段時間內搬不完的檔案每次都會失敗：下載回報可重試的 transport 錯誤，上傳則回報 `ambiguous_commit`，儘管 Taiga 根本沒收到那個檔案。每個 JSON 請求仍然每次嘗試以三十秒為限；傳輸改為在任一方向六十秒內沒有任何資料移動時才放棄，而且錯誤訊息會說明這一點。這樣死掉的對端仍會讓指令結束，大檔案則要多久就多久。
- 沒有送達 Taiga、回應無法解讀或被限流的 refresh，現在會照實回報，而不是回報觸發它的那個 `401`。那個拒絕會讓人在斷線時去重新登入，但磁碟上的 refresh token 其實還是好的；只有 Taiga 明確拒絕 refresh 時才保留原本的拒絕。
- `project import` 在串流 dump 之前會先用一次憑證。其他指令都先以 JSON 查詢接觸 Taiga，過期的 token 在那裡就會換發；匯入沒有自己的查詢，所以隔了一天後的第一個指令若是匯入，就會以 `auth` 失敗。
- 在附件或 CSV 下載進行中按下中斷，現在和第一個位元組之前中斷一樣以 `130` 離開，而不是標記為可重試的 `9`。
- `auth login --host` 與 `doctor --host` 指向不是 Taiga 網頁應用的位址時（例如社群論壇），原本只說「Not Found」。錯誤訊息現在會寫出實際嘗試的 `conf.json` 網址、對方回了什麼，以及 `--host` 應該填什麼。回應是網頁而不是 JSON 的主機現在回報 `validation`，而不是內部失敗。

### 變更

- 每一個「刪除後再讀回確認」的操作 —— 工作項目、Sprint、wiki 連結、工作流程中繼資料、到期日與 swimlane —— 改為共用同一份實作，取代原本五份相同的往返。唯一移動的措辭：Sprint 讀回的訊息現在和它的姊妹訊息一樣寫成小寫的「sprint」。

## [0.3.1] - 2026-09-03

工具與文件變更。`aihki` 執行檔的行為與 0.3.0 相同：close 與 comment 指令改寫成共用同一份實作，而 38 個指令的 help 文字、每一個 dry-run 標籤與每一個錯誤代碼都與 0.3.0 逐一比對過，確認呼叫端看得到的東西沒有任何改變。

### 新增

- 退出碼表補上 `1` —— CLI 遇到無法分類的狀況時回報的代碼，值得當成 bug 回報 —— 以及 0.3.0 加入卻未記錄的 `130`。中文表新增一欄翻譯，因為原本半數列是英文術語、半數是中文。
- 一小節說明為什麼這些寫入可以放心自動化，並連到說明 Taiga 會拒絕什麼、會合併什麼的頁面。
- gosec 納入本機 lint，新的弱雜湊、寫死憑證或以使用者輸入組成的子行程會在推送前被擋下，而不是推送後才發現。每一條關閉的規則都寫明了理由。
- CI 會回報測試覆蓋率。

### 變更

- 從原始碼建置需要 Go 1.25.13 或更新版本 —— 那是 1.25 系列中第一個沒有未修補標準函式庫弱點的版本。發布的執行檔本來就是用 1.26 建置的。

### 修正

- README 原本聲稱所有 mutation 都帶版本號。membership、webhook 與附件中繼資料都沒有，因此該敘述改為僅限 work item，而它描述的逐欄位行為現在由並行測試實際驗證，不再只是文字宣稱。

## [0.3.0] - 2026-09-02

### 修正

- 被拒絕的寫入現在會說明被拒絕的原因。Taiga 透過 Django REST Framework 回報驗證錯誤，會逐一指名被拒的欄位，而 CLI 把這些全部丟掉、只印 HTTP 狀態文字，所以少填 subject 和指派給不存在的人讀起來都是 `Bad Request`。巢狀 serializer 錯誤、以及 bulk create 收到的逐列格式現在也會渲染，並且有長度上限，大回應不會用單行灌滿終端機。
- 分辨「過期寫入」與「錯誤寫入」不再依賴 Taiga 訊息的措辭。兩者都是 HTTP 400、都掛在同一個 `version` 鍵下，只差在形狀：Taiga 自己的並行檢查送一個句子，而欄位格式錯誤送的是一個陣列。舊規則是在訊息裡找 "version" 這個字，會把 `Version must be specified` 判成別人的編輯 —— 要求呼叫端重讀後重試一個永遠不會成功的請求 —— 而且在非英文語系的伺服器上會完全認不出真正的衝突，因為 Taiga 會翻譯那個句子。
- 中斷寫入不再謊稱指令有 bug。寫入途中按 Ctrl-C 會印 `unexpected failure: context canceled` 並以 1 離開，那等於說寫入沒有發生；但 Taiga 不會因為 client 不聽了就回滾，所以現在回報 `ambiguous_commit` 並要求你先確認。送出前就取消的請求仍然是單純的中斷。
- 中斷一個不會留下任何東西的請求（登入、對專案按讚）不再回報可能的提交。「這個呼叫可能提交什麼」現在由知道端點的地方明確宣告，而不是從 HTTP 動詞去猜。
- Taiga 已經回應的附件上傳或專案匯入不再被回報成傳輸失敗。Taiga 拒絕或判定過大的上傳時不會把 body 讀完，導致送出端失敗；而那個失敗被優先檢查，於是一個已完成的回應（包含 `201`）被整個丟棄，附件明明存在卻告訴呼叫端上傳失敗。
- 取消 CSV 或附件下載不再被回報成可重試的伺服器故障 —— 那會誘使 agent 重新開始一個操作者剛剛才停掉的下載。

### 新增

- 退出碼 `130` 與契約代碼 `interrupted`、`timeout`，代表指令在完成前被停止。這採用 shell 慣例的 128 加訊號編號，而不是在分區表裡再佔一個號碼，因為中斷是一個決定，不是指令失敗的一種方式。結果未知的寫入仍然回報 `ambiguous_commit` 與退出碼 11，即使起因是中斷。
- 一份 end-to-end 壓力測試：十二個帳號無節流地同時操作同一個專案，混合競爭欄位的編輯、非競爭欄位的編輯、留言、批次建立與刻意的錯誤操作。它斷言每一次被接受的競爭寫入都拿到自己的版本號、沒有任何失敗被回報成內部缺陷或裸狀態文字，並且當一輪測試沒能製造出衝突、驗證錯誤與找不到資源時，它會失敗而不是在什麼都沒驗證到的情況下通過。

## [0.2.3] - 2026-09-02

僅工具與文件變更，`aihki` 執行檔與 0.2.2 相同。

### 新增

- 安裝腳本現在會實際寫入 shell completion，不再只是印出產生指令。它只寫入**已存在**的標準目錄，並逐一告知寫到哪裡；不會建立目錄 —— 替沒在用該 shell 的人建空資料夾只是製造垃圾。Windows 維持只印提示，因為安裝 PowerShell completion 需要修改使用者的 profile，侵入性遠高於放一個檔案。
- 解除安裝會精準移除這些檔案，並跳過看起來不是自動產生的檔案，避免誤刪同名的手寫 completion。

### 修正

- CI 現在會在三個作業系統上以「安裝後立即解除」的往返方式測試。先前版本宣稱有這項覆蓋，但當時的修改靜默地沒有套用，實際上該 job 從未執行過解除安裝。

## [0.2.2] - 2026-09-02

### 修正

- 告訴使用者「下一步該執行什麼」的錯誤訊息仍使用改名前的指令名 —— 缺少憑證時會建議 `taiga auth login` 與 `TAIGA_TOKEN`，而非 `aihki auth login` 與 `AIHKI_TOKEN`。11 個檔案中的 17 處訊息現在指向真實存在的指令。

### 變更

- 端對端測試改用 `AIHKI_TOKEN` 認證（先前從未被測到），並新增一個案例證明 `TAIGA_TOKEN` 在真實執行檔中仍可認證。只有單元測試覆蓋的回退路徑，等於沒有人真的跑過。

## [0.2.1] - 2026-09-02

僅工具與文件變更，`aihki` 執行檔的功能與 0.2.0 相同。

### 新增

- macOS、Linux 與 Windows 的解除安裝腳本，涵蓋執行檔、Windows 的 PATH 項目，以及選擇性的設定目錄與 OS keyring 憑證。預設保守，因為解除安裝常常只是升級的其中一步：`--purge`（Windows 為 `-Purge`）才會一併移除設定與憑證，含改名前舊名稱留下的資料；`--dry-run` 只列出將移除的項目而不做變更。
- POSIX 解除安裝腳本會偵測 Homebrew 安裝並引導改用 `brew uninstall`，不直接刪除 Homebrew 管理的檔案。兩個腳本都會說明 `project use --local` 綁定存放在哪裡 —— 任何解除安裝程式都無法從該 repository 之外找到它。
- Repository 首頁的 hero image。

### 變更

- INSTALL.md 補上中英雙語的解除安裝說明。
- CI 在三個作業系統上以「安裝後立即解除」的往返方式測試，確保解除安裝移除的正是安裝寫入的內容。

## [0.2.0] - 2026-09-02

### 更名為 Aihki

專案更名為 **Aihki**，執行檔為 `aihki`。Taiga 以 MPL-2.0 釋出，其第 2.3 節明文不授予商標與 logo 權利；製作方自己說明過，改用這份授權是為了對 Taiga 商標取得更多控制權。而後繼專案交由另一家公司接手時，Kaleidos 保留了品牌，該專案改以自己的名字發布。把他人的商標當作本專案自己的識別，從來就不是那份授權支持的事；因此該名稱現在只用於描述本客戶端所搭配的軟體。

與 Taiga 的整合沒有任何改變，改變的是這個工具本身的識別。

**既有安裝可以繼續使用。** 所有已儲存的狀態都會在首次使用時遷移，而不是被丟棄：

- 舊 `taiga-cli` keyring service 中的憑證，首次讀取時會被接收到 `aihki`，不會被登出。
- `aihki/` 沒有設定檔時會讀取 `taiga-cli/` 下的舊檔，下次儲存時寫入新位置。
- 以 `taiga.profile` 或 `taiga.project` 綁定的 repository 仍然有效；兩者並存時 `aihki.*` 優先。
- `TAIGA_TOKEN`、`TAIGA_PROFILE`、`TAIGA_API_URL`、`TAIGA_PROJECT` 仍然被接受，`AIHKI_*` 優先。

**你需要調整的部分**：改用 `brew install koukeneko/tap/aihki`；release archive 更名為 `aihki_<version>_<os>_<arch>`；completion 檔名為 `aihki.bash`、`_aihki`、`aihki.fish`、`aihki.ps1`。

## [0.1.0] - 2026-09-02

首個正式版本。以 Go 實作的 Taiga 6 命令列工具，提供人類可讀的終端輸出，以及給 Shell、CI 與 Agent 使用的穩定 JSON contract。

### 安裝

```sh
brew install koukeneko/tap/aihki
```

或從下方下載對應平台的 archive，並以 `SHA256SUMS` 驗證後解壓縮。

### 功能

- Project、Epic、User Story、Task、Issue、Sprint 與 Wiki 的完整日常操作。
- 成員與 Role 權限、Webhook、Custom field、八類 workflow metadata、Swimlane、Tag、Due-date preset。
- 跨專案的 Epic ↔ Story 關聯，以及 watch、vote、comment 編輯與歷史。
- Project 匯出／匯入、ownership transfer、CSV export 與第三方 importer gateway。
- Search、Timeline，以及 backlog velocity、Issue 趨勢、成員貢獻與 Sprint 燃盡統計。
- 附件的 streaming 上傳／下載，下載時核對 SHA-1 與大小。
- Epic、Story、Task、Issue 的批次建立，以及 Story／Task／Issue 的批次搬移與排序。

### 自動化介面

- `--json` 輸出帶 `meta.contract` 版本號的封包，目前為 contract 1。
- `--fields` 挑選欄位，`aihki schema <command>` 提供 JSON Schema 與 safety／idempotency 標註。
- 依錯誤種類分流的固定 exit code；`--dry-run` 完整解析但保證不送出寫入請求。
- Bash、Zsh、Fish、PowerShell 四種 shell completion，含 5 分鐘快取與離線 stale-on-error。

### 安全性

- 密碼不接受來自命令列；token 存於 OS keyring，不寫入設定檔。
- Webhook secret、application token auth code 與 ownership transfer token 不出現在任何輸出。
- 只有 idempotent 的 GET 會自動重試；寫入結果不明時回報 `ambiguous_commit` 要求人工確認。
- 樂觀鎖衝突時停止，不自動 merge 或覆寫。
- 附件下載不會把 API bearer token 送往 media URL。

### 發布產物

- macOS binary 以 Developer ID 憑證簽署並經 Apple notarization，下載後可直接執行。
- Linux 與 Windows archive 具位元可重現性；相同 source、toolchain、version、commit 與 epoch 會產生相同的 bytes。macOS 因 notarization 要求安全時間戳而不可重現。
- 每個 release 附 `SHA256SUMS` 與 SPDX 2.3 SBOM。

### 已知限制

- Taiga 6 REST API 將 `archived_code` 設為唯讀且無 archive action，因此 `project archive|unarchive` 會回報 `unsupported_capability`，需由站台管理者處理。
- Notarization ticket 無法 staple 到裸執行檔，Gatekeeper 需線上查驗；完全離線的首次執行仍可能被擋。
- `stats system` 僅在站台啟用 Taiga `STATS_ENABLED` 時存在，否則回報 `not_found`。

### 相容性

已針對 Taiga 6.10.2 以固定 image digest 執行完整 Docker E2E 驗證。支援 macOS、Linux、Windows 的 `amd64` 與 `arm64`。

[未發布]: https://github.com/KoukeNeko/taiga-cli/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.6.0
[0.5.2]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.5.2
[0.5.1]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.5.1
[0.5.0]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.5.0
[0.4.0]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.4.0
[0.3.2]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.3.2
[0.3.1]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.3.1
[0.3.0]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.3.0
[0.2.3]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.2.3
[0.2.2]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.2.2
[0.2.1]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.2.1
[0.2.0]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.2.0
[0.1.0]: https://github.com/KoukeNeko/taiga-cli/releases/tag/v0.1.0
