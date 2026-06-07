# Goal: phase-4e — Lambda@Edge 残り 3 フック対応（origin-request / origin-response / viewer-response、BL-LE1 解消 / M4 完全化）

> 生成: /goal:plan（2026-06-07）。調査 Workflow（goal-plan-investigate, runId wf_348276a5-7e4）の findings/critic を Opus が統合 + 核心 claim を公式ソースで裏取りして作文。
> 再設計の深さ = ユーザー確定「既存設計を精緻化」。ただし**検証で『精緻化の枠を超える設計ブロッカー』が確定したため、要判断事項に格上げ**（後述）。
> このファイルは計画のみ。承認・編集のうえ `/goal:exec` で実装に入る。

---

## 背景（なぜ / 要件 / 前提）

**なぜ**: viewer-request（phase-4d MVP）だけでは auth ショートサーキットしか動かず、本物 CloudFront 構成のローカル再現が頭打ち。M4「Lambda@Edge 含めた完全構成」の完成（BL-LE1 解消）が v0.2 筆頭優先（plan.md §v0.2）。cf-local の本丸＝nginx データプレーンを太らせる本丸作業。

**要件（当初）**:
- 3 フックの event 構築 / RIE 連携 / 改変反映 / 短絡応答 / エラー応答が動く
- origin-response の Lambda 改変結果は cache に格納（AWS 仕様準拠）／ viewer-response は cache 不変（transient）
- `rie_client.go` を 4 フック共通 generic invocation に refactor、`server.go` dispatch 拡張
- Terraform `lambda_function_association.event_type` 4 種すべて受理 → 関数が呼ばれる
- α 統合テスト 9 ケース（3 フック × Continue/ShortCircuit/LambdaError）
- `examples/lambda-edge-full/` 新設、docs 4 フック拡張、BL-LE1 解消マーク
- viewer-request 既存テストの regression 維持

> ⚠ 検証の結果、要件のうち 2 つは AWS / nginx の実仕様により**そのままでは達成不能**。再解釈が要る（→ 未確定・要判断事項 F2 / F4）。

**前提（確定）**:
- データプレーン = nginx + njs の **2-hop 構造**（outer `:8080` / inner `unix:/run/cf-local-inner.sock`、`upstream self` で self-loop）。**proxy_cache は outer/forward hop**（`@cf_le_<san>_forward`）、**origin 接続は inner hop**（`writeInnerLocation` の `proxy_pass http://<upstream>/`）。inner の `js_header_filter ttl.computeAndInject` 出力が outer の proxy_cache に upstream header として見える（2-1 spike 既知＝ X-Accel-Expires による TTL 駆動の prior-art）。
- edge-proxy(:4569) sidecar 経由で AWS 公式 Lambda RIE。njs→edge-proxy は `ngx.fetch`。
- control-plane / BoltDB / lookup は **既に 4 event_type 透過**（`handler.go::collectFunctions` は EventType フィルタ無し、edge-proxy `findByEvent` が event_type 一致で引く）→ **3 フック追加で変更不要**。
- `internal/edgefunc/types.go` は 4 event_type 定数を phase-4d で宣言済（`EventOriginRequest/OriginResponse/ViewerResponse` は宣言のみ・未使用）。
- ARN→RIE 解決は `parts[6]`（7番目＝関数名、`parts[5]=="function"`）。findings 間の 6 vs 7 食い違いは **7（index 6）が正**（`handler.go:162-175` 実読で確定）。

---

## 調査結果（goal実態 / 現状実態 / ギャップ）

### 検証で確定した 3 つの設計ブロッカー（公式ソース付き）

| # | 事実 | 出典 | 影響 |
|---|---|---|---|
| **B1** | `js_header_filter`/`js_body_filter` は同期専用。*"supports only synchronous operations. Thus, asynchronous operations such as r.subrequest() or setTimeout() are not supported."* `ngx.fetch` も async ゆえ filter phase で**呼べない**。`ngx.fetch` が使えるのは `js_content`/`js_access`/`js_periodic` 等 | nginx.org `ngx_http_js_module`（逐語確認） | design doc の origin-response/viewer-response = `js_body_filter`+`js_header_filter` 発火（§実装方針 nginx 側 / 4e-3/4/7）は**構造的に不成立**。response 2 フックは **js_content ベースの別トポロジに再アーキ**が要る |
| **B2** | proxy_cache はコンテンツ処理段で応答を保存し、header/body filter はその後。filter での改変はクライアント応答だけ変え cache 格納コピーは未改変。**ただし** 2-hop なら inner hop の改変が outer の proxy_cache に upstream として乗る（2-1 spike prior-art） | nginx cache 動作 + 既存 2-1 spike（`nginx/njs/spike/`） | 要件「origin-response 改変を cache 格納（AWS 準拠）」は **inner hop で同期的に改変してから outer が cache** すれば達成可。ただし改変呼び出しが ngx.fetch（async）なので **inner hop を `proxy_pass`→`js_content` に再アーキ**する必要（B1 と連動） |
| **B3** | *"Lambda@Edge does not expose the body that is returned by the origin server to the origin-response trigger."* origin-response event の `response` は `{status, statusDescription, headers}` のみ（**body フィールド無し**）。Lambda は body を**読めず**、生成 / 削除のみ可 | AWS `lambda-updating-http-responses` / `lambda-event-structure`（逐語確認） | 要件「response の status/**body** 改変が cache 格納」のうち **body 読取→書換は AWS でも不可**。本フェーズは status/headers 改変（+ body 生成/削除）に限定で再解釈する（F4） |

### event schema（AWS 公式で網羅、4e-2/3/4 の golden 根拠）

- **origin-request**: `request{clientIp, method, uri(rw), querystring(rw), headers(rw), origin(rw){custom|s3: customHeaders, domainName, path, port, protocol, ...}, body?}`。origin events のみ `origin` を持つ。
- **origin-response**: `request{...origin...}` + `response{status, statusDescription, headers(rw)}`（**body 無し**）。
- **viewer-response**: `request{...(origin 無し)}` + `response{status, statusDescription, headers(rw)}`。viewer-response は status code 改変不可等の制約あり（translator に反映）。

### 現状実態（実コード grounding 済 — grounding lens「十分」）

- **BL-LE1 のガードは 3 箇所のみ**: (1) `event.go:117` の `if req.EventType != EventViewerRequest { return error }`、(2) `server.go:163` の `BuildViewerRequestEvent` 固定呼び出し、(3) `conf.go:307 hasViewerRequestAssociation` の viewer-request 限定（`lambda_edge_test.go:229` が「非 viewer-request は ignore」を明示テスト）。
- **`rie_client.go::Invoke(ctx, endpoint, payload []byte)` は既に event_type 非依存の generic**。design doc Q3「専用関数→generic 化」は実態と乖離。4e-5 の真の作業は **`server.go` の dispatch + `event.go` の builder/translator を 4 フック化**。
- **njs `edge.js` は `viewerRequest` 1 関数**（`js_content` 経由 / async `ngx.fetch`）。response 系関数・filter 実装は皆無。
- **golden file テスト方式確立済**（`event_test.go` newCFRequestID stub で byte-stable）→ 3 golden を同型で追加可。
- **α テスト `tests/integration/lambda_edge_alpha_test.go`** の `stage()` は `EventTypeViewerRequest` ハードコード（line 79）。event_type パラメタ化で 9 ケースへ拡張可。
- **InvokeRequest（types.go）は `Request` のみ**で、response 系フックが Lambda に渡す origin/viewer response を njs→edge-proxy で運ぶフィールドが無い → **wire contract 拡張が必要**。

### ギャップ要約

| 領域 | 現状 | goal | 性質 |
|---|---|---|---|
| control-plane / lookup / BoltDB | 4 フック透過済 | 変更不要 | ✅ ギャップ無し |
| Go event builder / translator | viewer-request 1 系統 | 4 系統 | 精緻化（builder/translator/golden 追加） |
| Go dispatch（server.go） | viewer-request 固定 | event_type テーブル | 精緻化 |
| wire contract（types.go） | request のみ | response snapshot 追加 | 中規模（後方互換で追加可） |
| nginx renderer（**conf.go**） | viewer-request 専用 | 4 フック発火 | **再アーキ**（response 系は js_content トポロジ） |
| njs（edge.js） | viewerRequest 1 関数 | 4 関数 + 共通 helper | **再アーキ**（filter 不可 → js_content/async） |
| α テスト | viewer-request 3 | 9 ケース | 精緻化（stage パラメタ化） |
| examples / docs | viewer-request | 4 フック | 精緻化 |

---

## 実行計画

> 依存順: **spike（task-1）がアーキ確定ゲート**。spike 完了まで data-plane 実装（task-8/9）を着手しない。Go 側（task-3〜7）は spike と並行可。

- [ ] **task-1（旧 4e-1 を再定義）**: response-hook 連携アーキ確定 spike（「directive 選定」から格上げ）
  - 操作対象: `nginx/njs/spike/` に検証用 nginx.conf + njs（実機 docker）
  - 操作内容: B1（filter で ngx.fetch 不可）を前提に、(a) **origin-request/origin-response**: inner hop を `proxy_pass`→`js_content`（`ngx.fetch` で origin 取得 + `ngx.fetch` で edge-proxy 呼び + 同期適用）に置換する案、(b) **viewer-response**: outer の上に transient な js_content hop を足す **3-hop** 案、の実機成立性を確認。2-1 spike（inner header→outer proxy_cache 可視）を origin-response cache-write の prior-art として転用検証。各フックに割当てる directive と hop 構成を memo に確定
  - 影響場所と効果: data-plane トポロジを確定。後続 task-8/9 の唯一の実装根拠
  - goalへの影響: 3 フックが「動く」ための前提。NG なら F1/F2 の retreat を発火
- [ ] **task-2**: design doc / tasks の誤記修正（exec の対象誤認防止）
  - 操作対象: `.claude/design/lambda-edge-remaining-hooks-2026-05-09.md`, `.claude/tasks.md`
  - 操作内容: 4e-6 主対象 `renderer.go`→**`conf.go`**、4e-8 テスト `lambda_edge_test.go`→**`lambda_edge_alpha_test.go`**、Q3「rie_client.go generic 化」→「server.go dispatch + event.go の 4 builder/4 translator 化（rie_client.Invoke は既に generic）」、Q1/Q2 を B1〜B3 に合わせ再記述
  - 影響場所と効果: 設計ドキュメントを実態に一致させる
  - goalへの影響: exec-ready 化の前提
- [ ] **task-3**: wire contract 拡張（types.go）
  - 操作対象: `internal/edgefunc/types.go`, `nginx/njs/edge.js`（snapshot 側）
  - 操作内容: `InvokeRequest` に response snapshot フィールド（origin-response/viewer-response が Lambda に渡す `status`/`headers`）を **optional 追加**。phase-4d viewer-request 経路は request のみで影響なし（後方互換）
  - 影響場所と効果: response 系フックの event を組める前提データを edge-proxy に運ぶ
  - goalへの影響: origin-response/viewer-response の builder（task-5/6）の入力源
- [ ] **task-4（4e-2）**: `BuildOriginRequestEvent` + `testdata/origin-request.golden.json`
  - 操作対象: `internal/edgefunc/event.go`, `internal/edgefunc/testdata/`, `event_test.go`
  - 操作内容: AWS schema 準拠で origin-request event 構築。`origin` object の充足は **F3 の決定に従う**（MVP: distribution origin config から最小構築 or 省略）。`CloudFrontRequest` に `origin`/`customHeaders` を追加（現 event.go:86-89 は意図的省略）
  - 影響場所と効果: origin-request の Go event 形式を確定（golden で固定）
  - goalへの影響: origin-request フックの中核
- [ ] **task-5（4e-3）**: `BuildOriginResponseEvent` + golden
  - 操作対象: `internal/edgefunc/event.go`, `testdata/`, `event_test.go`
  - 操作内容: `response{status, statusDescription, headers}` を構築（**body フィールドは作らない＝B3 準拠**、docstring 明記）。`CloudFrontResponse` を origin-response 用に拡張
  - 影響場所と効果: origin-response の Go event 形式を確定
  - goalへの影響: origin-response フックの中核
- [ ] **task-6（4e-4）**: `BuildViewerResponseEvent` + golden
  - 操作対象: `internal/edgefunc/event.go`, `testdata/`, `event_test.go`
  - 操作内容: `response{status, statusDescription, headers}`。viewer-response の AWS 制約（status 変更不可等）を翻訳側で reject/ignore
  - 影響場所と効果: viewer-response の Go event 形式を確定
  - goalへの影響: viewer-response フックの中核
- [ ] **task-7（4e-5）**: dispatch テーブル化 + translator 3 種
  - 操作対象: `internal/edgefunc/server.go`, `internal/edgefunc/event.go`
  - 操作内容: `server.go::invoke` の `BuildViewerRequestEvent`/`TranslateViewerRequestResponse` ハードコード（163/187）を `event_type → (builder, translator)` テーブル dispatch に。`TranslateOriginRequest/OriginResponse/ViewerResponse` 3 translator 追加。`event.go:117` の viewer-request ガード解除
  - 影響場所と効果: 4 event_type を 1 経路で捌く。viewer-request regression を unit で担保
  - goalへの影響: 全フックの edge-proxy 側実体
- [ ] **task-8（4e-6）**: nginx renderer（**conf.go**）拡張 ※ task-1 spike 依存
  - 操作対象: `internal/nginx/conf.go`, `internal/nginx/lambda_edge_test.go`
  - 操作内容: `hasViewerRequestAssociation`→4 event_type 判定（`behaviorView` を bool→フラグ群へ）。spike 結果で各フックの発火 location/directive を生成（origin-request/response: **inner hop の js_content 化**、viewer-response: **追加 hop**）。`lambda_edge_test.go:229` の「非 viewer-request は ignore」テストを「発火する」へ書換
  - 影響場所と効果: 生成 nginx.conf が 4 フックを発火する
  - goalへの影響: data-plane が Lambda を正しい段で呼ぶ
- [ ] **task-9（4e-7）**: njs `edge.js` 拡張 ※ task-1 spike 依存
  - 操作対象: `nginx/njs/edge.js`
  - 操作内容: `runOriginRequest`/`runOriginResponse`/`runViewerResponse` 追加。**filter ではなく js_content/async 経路**で `ngx.fetch`。`ngx.fetch`/fail-open/status decode を `runEdgeFunction(eventType, ...)` 共通 helper に切出し（viewerRequest と共通化）
  - 影響場所と効果: njs が response snapshot を取り edge-proxy と往復、改変を適用
  - goalへの影響: data-plane の Lambda 連携本体
- [ ] **task-10（4e-8）**: α 統合 9 ケース
  - 操作対象: `tests/integration/lambda_edge_alpha_test.go`
  - 操作内容: `stage()` を event_type パラメタ化。3 フック × Continue/ShortCircuit/LambdaError。viewer-request 既存 3 ケース全 PASS 維持（R-3）
  - 影響場所と効果: 3-server httptest で回帰検出
  - goalへの影響: 「動く」ことの自動保証
- [ ] **task-11（4e-9）**: `examples/lambda-edge-full/` 新設
  - 操作対象: `examples/lambda-edge-full/`（新規）, `docker-compose.lambda.yml`（or 自己完結 compose）
  - 操作内容: 4 フック組合せのサンプル Lambda + compose（自己完結 or override = F5）+ README。R-5（4 RIE 並列メモリ）の実測手順を明記
  - 影響場所と効果: 本物 CloudFront 構成相当の体験
  - goalへの影響: 利用者が 4 フックを再現できる
- [ ] **task-12（4e-10）**: docs + 完了メモ
  - 操作対象: `docs/lambda-edge.md`, `docs/limitations.md`, design doc
  - 操作内容: lambda-edge.md を 4 フック構成図/設定/return 仕様に拡張。limitations.md の BL-LE1 を **解消 or 部分解消マーク**（F1/F2 次第で `BL-LE-Cache1`/viewer-response 残を起票）。B1〜B3 を DESIGN.md 更新候補として完了メモに列挙
  - 影響場所と効果: 制約と使い方を実態に一致
  - goalへの影響: M4 到達の対外宣言

---

## 未確定・要判断事項

> **F1 が最重要**（goal の括り＝M4 達成タイミングに直結）。F2〜F4 は要件の再解釈で、検証により当初要件のままでは達成不能なもの。

### F1 [scope / goal 影響] phase-4e の括り方 — **要判断**
検証で response 2 フック（origin-response / viewer-response）は filter 不可（B1）で **js_content への再アーキ**が必要と確定。「精緻化」の枠を超える。括り方:
- **(A) 推奨 — 分割**: phase-4e = **spike + origin-request の縦スライス end-to-end**（origin-request は inner hop の js_content 化で達成可・viewer-request と同質の中規模）。response 2 フック（再アーキ大）は **phase-4f** に分離。→ 各フェーズが runnable（CLAUDE.md「動く範囲を少しずつ」）、リスク段階的、BL-LE1 は phase-4f で完全解消
- **(B) 一括**: 3 フット全部 phase-4e。task-1 spike を「アーキ確定ゲート」にして進む。M4 を 1 フェーズで完成できるが、PR が重く response 再アーキの不確実性を全部抱える
- 判断根拠: 実装難易度の勾配は origin-request（中） < origin-response（中〜大、cache-write 絡む） < viewer-response（大、3-hop）。**(A) 推奨**

### F2 [要件再解釈] origin-response の cache-write（旧 Q2）
要件「Lambda 改変結果が cache に格納（AWS 準拠）」は B2 により inner-hop js_content 再アーキで**達成可能**だが中〜大コスト:
- **(A)** inner-hop 再アーキで AWS 準拠 cache 格納を達成（2-1 spike prior-art あり）
- **(B)** v0.2 は status/headers の transient 改変のみ、cache-write は `BL-LE-Cache1` 起票で v0.3。retreat は M4「完全」をやや弱める
- F1=A なら本論点は phase-4f で決着。推奨: **spike でコスト確認 → 許容なら (A)、balloon なら (B) へ retreat**

### F3 [要件] origin-request の `origin` object 充足
origin-request event は `origin{custom/s3,...}` を持つが、現状 njs snapshot も control-plane LookupResult も origin 情報を運ばない:
- **(A)** distribution origin config から `custom.domainName`/`customHeaders` 等を最小構築（新データ経路: LookupResult 拡張 or renderer 埋め込み）
- **(B) 推奨(MVP)** `origin` を省略 or 最小（dynamic origin selection 非対応、BL 起票）。典型の request 改変（uri/qs/headers）は (B) でも動く

### F4 [要件・AWS 制約] origin-response の body
B3 により origin Lambda は origin body を**読めない**（生成/削除のみ）。本フェーズを **status/headers 改変（+ body 生成/削除）に限定で確定してよいか**。→ **Yes 推奨**（AWS 仕様そのもの。「body 改変＝書換」は AWS でも不可）

### F5 [小] examples の compose 方式
`examples/lambda-edge-full/` を **自己完結 compose**（推奨・full 例の明快さ）にするか、現 basic 方式（ルート `docker-compose.lambda.yml` override 参照）にするか

---

## PR仕様（/goal:exec はこの仕様どおりに PR 本文を書く）

> 下記は **F1=(A) 分割** を前提とした推奨形。F1=(B) を選ぶ場合は PR-3 を phase-4e に取り込み統合する。
> 全 PR 共通規約（memory）: review 指摘は worktree で stacked PR、PR 本文は一時ファイル + `--body-file`（heredoc 禁止）、Mermaid で構造図必須。base = develop、ブランチ `feat/phase-4e-...`。

### PR-1: phase-4e — spike + Go 3-hook scaffolding + 誤記修正（cf-local）
- 目的: response-hook アーキを spike で確定し、Go 側（event builder/translator/dispatch/wire）を 4 フット対応にする。data-plane はまだ origin-request のみ後続で結線
- 満たすべき要件: task-1（spike memo）/ task-2（誤記修正）/ task-3（wire）/ task-4・5・6（3 builder + golden）/ task-7（dispatch + translator、viewer-request regression unit PASS）。`go test ./...` + golden diff 緑
- 着手前の立ち位置: viewer-request のみ（Go も data-plane も）。response hook 不能が未確定
- 完了後の立ち位置: アーキ確定 memo あり。Go は 4 event_type を unit で往復可（data-plane 未結線なので挙動は viewer-request のまま＝既存 regression 無し）
- 作業フロー図:
  ```mermaid
  flowchart TD
    A[viewer-request 専用Go + 設計誤記] --> S[task-1 spike: 連携アーキ確定]
    S --> B[task-2 誤記修正]
    B --> C[task-3 wire: InvokeRequest に response snapshot]
    C --> D[task-4/5/6 3 builder + golden]
    D --> E[task-7 dispatch テーブル + 3 translator + guard解除]
    E --> F[go test 緑: 4 event_type を unit で往復]
  ```
- 解決タスクと goal への効果: task-1/2/3/4/5/6/7。3 フックの Go 基盤が揃い、PR-2/3 の data-plane を載せる土台になる
- PR外への影響: data-plane（nginx/njs）は無変更なので **稼働中の viewer-request 挙動に影響なし**。新 Go コードは golden/httptest で exercise（dead code ではない）
- その他共有事項: spike が NG なら本 PR 内で F1/F2 の retreat を提起して停止

### PR-2: phase-4e — origin-request 縦スライス end-to-end（cf-local、PR-1 に stack）
- 目的: origin-request フックを実機で動かす（inner hop の js_content 化）
- 満たすべき要件: task-8（conf.go: origin-request の inner-hop js_content 発火 + `lambda_edge_test.go` 書換）/ task-9（edge.js `runOriginRequest` + 共通 helper）/ task-10（α origin-request 3 ケース）/ examples・docs の origin-request 分。Terraform で `event_type=origin-request` 受理→関数呼び（実機 walkthrough、R-6）
- 着手前の立ち位置: Go 基盤あり・data-plane は viewer-request のみ
- 完了後の立ち位置: viewer-request + **origin-request** が end-to-end 稼働。cache MISS 時に origin 接続前へフック
- 作業フロー図:
  ```mermaid
  flowchart TD
    A[Go基盤 + viewer-request data-plane] --> B[task-8 conf.go: inner-hop を js_content 化]
    B --> C[task-9 edge.js: runOriginRequest async]
    C --> D[task-10 α: origin-request 3ケース]
    D --> E[Terraform 実機 walkthrough]
    E --> F[viewer-request + origin-request 稼働]
  ```
- 解決タスクと goal への効果: M4 を request 側で完成（残り response 側は phase-4f）
- PR外への影響: inner hop の構造変更は viewer-request の continue 経路（forward→inner）にも触れるため、**viewer-request regression を α で必ず確認**（R-3）
- その他共有事項: REV-12（`$uri$is_args$args`）と origin-request の URI 合成（R-7）をここで設計

### PR-3: phase-4f — response 側 2 フック（origin-response / viewer-response）※ F1=(A) 時は別フェーズ
- 目的: origin-response（inner-hop 同期改変→outer cache 格納、F2 決定に従う）と viewer-response（transient な追加 hop）を動かし BL-LE1 完全解消
- 満たすべき要件: response 系の renderer/njs 再アーキ + α 6 ケース + examples/docs 仕上げ + BL-LE1 解消マーク。F2/F4 の確定方針を反映
- 着手前の立ち位置: viewer-request + origin-request 稼働
- 完了後の立ち位置: **4 フット完全カバー（M4 完成）**
- 作業フロー図:
  ```mermaid
  flowchart TD
    A[request側2フック稼働] --> B[origin-response: inner-hop js_content 同期改変]
    B --> C[outer proxy_cache が改変後を格納 - F2=A]
    A --> D[viewer-response: 追加hop で transient 改変]
    C --> E[α 6ケース + examples/docs]
    D --> E
    E --> F[BL-LE1 解消 / M4 完成]
  ```
- 解決タスクと goal への効果: M4 完全構成の達成
- PR外への影響: 2-hop→（viewer-response 用）3-hop 化は全 cacheable behavior の応答経路に影響し得るため stress / regression 必須
- その他共有事項: F2=(B) retreat 採用時は cache-write を `BL-LE-Cache1` に積み、本 PR は transient のみで BL-LE1 を「部分解消」マーク

---

plan.md を確認・編集のうえ `/goal:exec` を実行してください。特に **F1（分割するか一括か）** を決めてから exec に入るのを推奨します（PR 構成が変わるため）。
