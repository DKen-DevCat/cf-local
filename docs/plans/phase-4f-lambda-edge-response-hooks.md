# Goal: phase-4f — Lambda@Edge response 系 2 フック対応（origin-response / viewer-response、BL-LE1 完全解消 / M4 完成）

> 生成: /goal:plan（2026-06-07）。調査 Workflow（goal-plan-investigate, runId wf_d9c1e5fd-10d, 5 agents / 461k tok）の findings/critic を Opus が統合 + 実コード精査で核心 claim を裏取り + `Skill(dme)` で構造判断して作文。
> このファイルは計画のみ。確認・編集のうえ `/goal:exec` で実装に入る。
> **前段**: `docs/plans/phase-4e-lambda-edge-remaining-hooks.md`（F1=A 分割の根拠 / B1〜B3 設計ブロッカーの公式裏取り）。phase-4e で **origin-request 縦スライス + 4 フック分の Go/edge-proxy 層**を PR #23（develop merge 済, HEAD `e0c0273`）で完了。本フェーズはその続編＝**response 2 フックのデータプレーン結線**。

---

## 背景（なぜ / 要件 / 前提）

**なぜ**: M4（Lambda@Edge 含めた完全構成）の完成。phase-4d で viewer-request MVP、phase-4e（PR #23）で origin-request 縦スライス。残り 2 フック（origin-response / viewer-response）を結線すれば 4 フック完全カバーで本物 CloudFront 構成をローカル再現でき、`BL-LE1` を完全解消できる。v0.2 筆頭優先（`.claude/plan.md`）。cf-local の本丸＝nginx データプレーンを太らせる作業。

**確定済み判断（ユーザー intake 2026-06-07）**:
- **F2=A**: origin-response の Lambda 改変結果（status/headers）は inner hop で同期改変してから **outer `proxy_cache` が改変後を格納**する AWS 準拠経路（cache-write 完全対応）。
- **grouping=A**: 1 フェーズ phase-4f 内で **stacked PR**（PR-1 origin-response → PR-2 viewer-response）。
- **origin-response は status/headers のみ**（B3 により origin body は trigger に露出されない＝読取不可。生成/削除のみ）。
- **examples = 自己完結 compose**（`examples/lambda-edge-full/` を 4 フック組合せに拡張）。

**要件**:
1. **origin-response**: cache MISS 時 origin 接続後・`proxy_cache` 格納前に Lambda を呼び、status/headers 改変結果を cache に格納（F2=A）。LambdaError は fail-open（origin 応答素通し）。body 読取は不可（B3）。
2. **viewer-response**: cache HIT/MISS 共通で viewer 返却直前に Lambda を呼び **headers のみ** transient 改変（status は AWS 制約で不変＝Go の `TranslateViewerResponseResponse` が `original.Response.Status` を強制）。**cache には書き込まない**。
3. **nginx topology**: B1（`js_header_filter`/`js_body_filter` は同期専用で `ngx.fetch` 不可）を前提に `js_content` ベースで実装。
4. **njs edge.js**: `runOriginResponse` / `runViewerResponse` 追加。response snapshot（status/headers）を `InvokeRequest.Response`（既存 wire field）に詰めて edge-proxy へ `ngx.fetch`。`ngx.fetch`/fail-open/action-dispatch の重複（現状 viewer-request + origin-request の 2 箇所、本フェーズで 4 箇所＝CLAUDE.md「3 回以降抽象化」充足）を `runEdgeFunction` helper に切出し。
5. **regression 維持**: viewer-request（phase-4d）+ origin-request（PR #23）の既存 α テスト全 PASS。topology 変更が継続経路を壊さないこと。
6. **Terraform**: `lambda_function_association.event_type=origin-response / viewer-response` が受理され実機で関数が呼ばれる（walkthrough R-6 必須、phase-4d W-1/W-2 と同方針）。
7. **examples/docs**: `examples/lambda-edge-full/` を 4 フック自己完結 compose に、`docs/lambda-edge.md` を 4 フック構成に、`docs/limitations.md` の `BL-LE1` を**完全解消マーク**。

**前提（実コードで grounding 済）**:
- **Go/edge-proxy 層は 4 フック完備・変更不要**（PR #23）: `internal/edgefunc/event.go` の `BuildOriginResponseEvent`(L208) / `BuildViewerResponseEvent`(L260) / `TranslateOriginResponseResponse`(L416, `ActionContinue`+flatten) / `TranslateViewerResponseResponse`(L438, status 強制保持・headers のみ採用)、`internal/edgefunc/server.go` の `eventHandlers` dispatch 表(L55-72, 4 event_type)、`internal/edgefunc/types.go` の `InvokeRequest.Response *InvokeRawResponse`(L38) / `InvokeRawResponse{Status,StatusDesc,Headers}`(L79)。control-plane / BoltDB / lookup（`handler.go::collectFunctions` / edge-proxy `findByEvent`）も 4 event_type 透過済。
- **α テストは Go 境界のみ**: `tests/integration/lambda_edge_alpha_test.go` の `TestLambdaEdgeAlpha_OriginResponse_Continue/LambdaError`(L308-368)・`ViewerResponse_Continue/LambdaError`(L370-427) は edge-proxy / builder / translator を 3-server httptest で検証するが、**nginx/njs データプレーンの結線は検証していない**。データプレーン疎通の保証は renderer unit（`lambda_edge_test.go`）+ 実機 walkthrough（R-6）で別途取る。
- **2-hop アーキ（実コード）**: outer server `listen 8080`（`writeServerBlock` conf.go:418）/ inner server `listen unix:/run/cf-local-inner.sock`(conf.go:433)。outer は behavior ごとに `writeOuterLocation`(通常: proxy_cache + `proxy_pass http://self<InnerPrefix>$request_uri`) か `writeLambdaEdgeOuter`(viewer-request: `js_content edge.viewerRequest`) + `writeForwardLocation`(`@cf_le_<san>_forward`: internal, proxy_cache + proxy_pass)。inner は policy ごとに inner-A(`/_cf_or_<san>/`: `js_content edge.runOriginRequest`, `writeOriginRequestLocation` conf.go:574) + inner-B(`/_cf_inner_<san>/`: `js_header_filter ttl.computeAndInject` + `proxy_pass http://<named-upstream>/`, `writeInnerLocation` conf.go:585)。
- **origin-request は origin を nginx `proxy_pass`(inner-B) で取得**。`runOriginRequest`(edge.js:221) の `ngx.fetch` は **edge-proxy(Lambda)** を叩くのみで origin は叩かない。Lambda 呼びは **origin fetch の前**（`spike/origin-request/README.md` で MISS 時のみ発火・キャッシュ可を 2026-06-07 実機確認）。
- **origin URL は named upstream として隠蔽**: inner-B は `proxy_pass http://origin_<san>/`（`originView.UpstreamName`）。njs に実 origin URL は渡っていない。

---

## 調査結果（goal実態 / 現状実態 / ギャップ）

### 現状実態（grounded — critic grounding lens=sufficient）

| 層 | 状態 | 変更要否 |
|---|---|---|
| Go event builder/translator（4 フック） | `event.go` に 4 builder + 4 translator 完備 | ✅ 変更不要 |
| Go dispatch（server.go） | `eventHandlers` 4 event_type 完備 | ✅ 変更不要 |
| wire contract（types.go） | `InvokeRequest.Response` snapshot 完備 | ✅ 変更不要 |
| control-plane / lookup / BoltDB | 4 event_type 透過 | ✅ 変更不要 |
| α テスト（Go 境界） | OriginResponse/ViewerResponse の Continue+LambdaError 済 | ⚠ Go 境界のみ。データプレーンは未検証 |
| **njs（edge.js）** | `viewerRequest` + `runOriginRequest` のみ。response 系皆無。helper 未抽出で重複 | **❌ 本フェーズ対象** |
| **nginx renderer（conf.go）** | viewer-request + origin-request の topology のみ。response 系の view/association/writer 皆無 | **❌ 本フェーズ対象** |
| examples / docs | viewer-request + origin-request まで | ❌ 本フェーズ対象 |

### gap（実装対象 = データプレーンのみ）

- **edge.js**: `runOriginResponse` / `runViewerResponse` 追加 + `export default` 追記。`runEdgeFunction` helper 抽出（`ngx.fetch`+fail-open+action-dispatch の重複集約）。upstream 応答 snapshot を `InvokeRequest.Response` 形に変換する `snapshotResponse` 追加。
- **conf.go**: `behaviorView` / `innerView` に `LambdaEdgeOriginResponse` / `LambdaEdgeViewerResponse` フラグ追加（L84-118）。`hasOriginResponseAssociation` / `hasViewerResponseAssociation` 追加（L325-355 と同型）。`hasLambdaEdge`(L62-68) の条件式に response 系を追加（**未修正だと edge.js import + resolver directive が emit されず、response フックだけ attach の behavior が動かない**）。`writeServerBlock`(L412) と新 writer（origin-response の inner topology / viewer-response の transient hop）。`lambda_edge_test.go` の renderer テスト追加。
- **examples/lambda-edge-full**: `main.tf` に origin-response / viewer-response の `lambda_function_association` 追加、`docker-compose.yml` に 2 RIE 追加（計 4）、`lambdas/` に 2 関数追加。
- **docs**: `lambda-edge.md` 4 フック構成化、`limitations.md` の `BL-LE1` 完全解消マーク（現状 L125/L190「部分解消」・`BL-LE-Cache1` L198）。

### goal実態（critic が突いた未証明の核心 — これが本フェーズの最大リスク）

**既存 spike は response 系の topology を証明しない**（critic consensus INCOMPLETE: coverage/risk が unsufficient、根拠は力学の反転）:

| spike が証明したこと | response 系が必要とすること | 差分 |
|---|---|---|
| 2-1 spike: inner `proxy_pass`+`js_header_filter` の出力 → outer `proxy_cache` に upstream header として可視（TTL 透過） | origin-response: inner `js_content` の **`r.return()` 合成応答** → outer `proxy_cache` が格納 | filter 出力の透過 ≠ 合成応答のキャッシュ可否 |
| origin-request spike: inner `js_content`+`ngx.fetch(edge-proxy)`+`internalRedirect` → inner-B `proxy_pass(origin)` | origin-response: **origin fetch の後**に Lambda を呼び改変結果を返す | Lambda 呼びの**前後が反転**。internalRedirect→proxy_pass では改変できない（B1/B2 の filter 問題に逆戻り） |
| （viewer-response の prior-art なし） | viewer-response: cache **HIT/MISS 共通**で viewer 返却直前に transient hop | 全 cacheable 応答経路に乗る＝regression blast radius 大。topology 完全未設計 |

→ **phase-4f は topology spike を「アーキ確定ゲート」として先頭に置く**（phase-4e の task-1 spike が origin-request の確定ゲートだったのと同型）。spike NG なら retreat（F2→B 等、後述 F-A）を発火して停止。

---

## 実行計画

> 依存順: 各 PR は**自分の spike をアーキ確定ゲート**として持ち、spike 成立まで当該 PR の実装タスクを着手しない。PR-2 は PR-1 に stack（PR-1 の topology 変更後の outer/inner 構造を前提にするため）。

### PR-1: origin-response 縦スライス

- [ ] **task-1**: spike-A — origin-response cache-write topology アーキ確定ゲート
  - 操作対象: `nginx/spike/origin-response/`（新規。spike nginx.conf + njs + `docker-compose.spike.yml` + mock-edge-proxy + mock-origin、`spike/origin-request/` を雛形に）
  - 操作内容: 候補α（outer `proxy_cache` → 新 inner-C `js_content` が `ngx.fetch(inner-B via self-loop unix socket)` で origin 応答取得 → `ngx.fetch(mock-edge-proxy)` → `r.headersOut` に改変 status/headers 設定 → `r.return()`）を実機 docker で検証。**acceptance**: (a) outer `proxy_cache` が inner-C の `r.return()` 合成応答を**キャッシュ**するか（2 回目 HIT・mock-edge-proxy 呼び 1 回）、(b) `ngx.fetch` が inner **unix socket** (`http://unix:/run/cf-local-inner.sock:/_cf_inner_<san>/...`) に到達するか、(c) inner-C で `r.headersOut['X-Accel-Expires']` / `Cache-Control` を再設定したとき outer `proxy_cache` の TTL に効くか。NG 時の fallback 候補β（inner-B を js_content 化 + `$cf_origin_url` 注入）と、(b) NG 時の inner loopback TCP 復活案（→ F-B の security ⚖️）も spike で評価。結論を `README.md` に memo（確定 topology + directive 割当 + 採否根拠）
  - 影響場所と効果: data-plane topology を確定。後続 task-2/3 の唯一の実装根拠
  - goalへの影響: F2=A（cache-write）が「動く」ための前提。NG なら F-A retreat を発火
- [ ] **task-2**: conf.go — origin-response の view / association / topology 生成
  - 操作対象: `internal/nginx/conf.go`, `internal/nginx/lambda_edge_test.go`
  - 操作内容: `behaviorView`/`innerView` に `LambdaEdgeOriginResponse bool` 追加（L84-118）。`hasOriginResponseAssociation`(L325-355 同型) 追加。`hasLambdaEdge`(L62-68) 条件に `LambdaEdgeOriginResponse` 追加。`buildBehaviorViews`(L160) で association 判定 + `addInner` 反映。spike-A 確定 topology に基づき origin-response の inner-C location writer（候補α: `/_cf_oresp_<san>/` js_content）を生成。**origin-request + origin-response 同一 behavior 時の inner-hop 実行順序**（inner-A origin-request → inner-B proxy_pass → inner-C origin-response）を topology として確定し、outer の `proxy_pass` ターゲット切替（`writeOuterLocationBody` L562-565 の `targetPrefix` 分岐拡張）を実装。`lambda_edge_test.go` に origin-response renderer テスト追加（現状 origin-response テスト皆無）
  - 影響場所と効果: 生成 nginx.conf が origin-response を正しい段で発火し、改変後を cache 格納
  - goalへの影響: origin-response フックの data-plane 中核
- [ ] **task-3**: edge.js — runOriginResponse + runEdgeFunction helper 抽出
  - 操作対象: `nginx/njs/edge.js`
  - 操作内容: `runOriginResponse` 追加（spike-A 経路: 上流応答取得 → response snapshot を `InvokeRequest.Response` に詰め edge-proxy 呼び → 改変 status/headers 適用 → 格納される応答として返す）。`snapshotResponse(upstreamResp)` ユーティリティ追加（`InvokeRawResponse` 相当へ変換）。`viewerRequest`/`runOriginRequest`/`runOriginResponse` に重複する `ngx.fetch`+JSON parse+fail-open+action-dispatch を `runEdgeFunction(r, eventType, {payloadExtra, onContinue, failOpen})` helper に集約（CLAUDE.md 3 回以降抽象化に合致＝4 利用箇所）。`export default` に追記。**fail-open は origin 応答素通し**（response 系は black-hole させない）
  - 影響場所と効果: njs が origin 応答を Lambda と往復し改変を適用
  - goalへの影響: origin-response の njs 連携本体。helper 抽出で viewer-response（task-8）の実装コストも下げる
- [ ] **task-4**: examples（origin-response 分）+ docs（origin-response 分）+ walkthrough R-6
  - 操作対象: `examples/lambda-edge-full/{main.tf,docker-compose.yml,lambdas/origin-response/index.js,README.md}`, `docs/lambda-edge.md`, `.claude/design/lambda-edge-remaining-hooks-2026-05-09.md`(walkthrough 追記)
  - 操作内容: origin-response の `lambda_function_association`(event_type=origin-response) を main.tf に追加。docker-compose に `lambda-origin-response` RIE 追加 + `CF_LOCAL_LAMBDA_FUNCTIONS` に endpoint 追記。デモ関数 = **レスポンスヘッダ付与**（例: `X-Origin-Processed: cf-local` / security headers）。docs に origin-response の topology 図 + return 仕様 + cache-write 挙動（F2=A）を追記。Terraform apply → curl で MISS 時に Lambda 発火 + 改変ヘッダが**キャッシュされる**ことを実機確認（R-6, phase-4d W 手順を踏襲）
  - 影響場所と効果: origin-response が end-to-end で実機動作することを保証 + 利用者が再現できる
  - goalへの影響: M4 を response 側の半分（origin-response）で達成

### PR-2: viewer-response 縦スライス（PR-1 に stack）

- [ ] **task-5**: spike-B — viewer-response transient hop topology アーキ確定ゲート
  - 操作対象: `nginx/spike/viewer-response/`（新規）
  - 操作内容: outer location を `js_content runViewerResponse` 化し、`ngx.fetch(/_cf_vr_fwd_<san>/ = forward(proxy_cache))` で **HIT/MISS 共通**に応答取得 → mock-edge-proxy → headers 適用（status 不変）→ `r.return()`（cache 書込み無し）を実機検証。**acceptance**: (a) `@named` location は ngx.fetch 不可 → 内部 prefix location(`/_cf_vr_fwd_<san>/`) で forward(proxy_cache) を包めるか、(b) cache **HIT 時**に viewer-response hop が発火し改変が効くか（MISS 時も）、(c) 改変が **cache に漏れない**（次回 HIT で改変前が返る）こと、(d) 既存 viewer-request / origin-request 経路の regression が無いこと。結論を `README.md` に memo
  - 影響場所と効果: viewer-response の transient topology を確定
  - goalへの影響: viewer-response が「動く」ための前提。NG なら F-C を発火
- [ ] **task-6**: conf.go — viewer-response の view / association / topology 生成 + 組合せ
  - 操作対象: `internal/nginx/conf.go`, `internal/nginx/lambda_edge_test.go`
  - 操作内容: `behaviorView` に `LambdaEdgeViewerResponse bool` 追加。`hasViewerResponseAssociation` 追加。`hasLambdaEdge` 条件拡張。spike-B 確定 topology に基づき viewer-response の outer transient hop（`js_content runViewerResponse` + `/_cf_vr_fwd_<san>/` 内部 forward location）writer を生成。**viewer-request + viewer-response 同一 behavior 時の outer location 組合せ**（`writeLambdaEdgeOuter` viewer-request と viewer-response transient hop の合成）を topology 確定。`lambda_edge_test.go` に viewer-response renderer テスト + viewer-request/viewer-response 組合せテスト追加
  - 影響場所と効果: 生成 nginx.conf が viewer-response を HIT/MISS 共通で発火（cache 不変）
  - goalへの影響: viewer-response フックの data-plane 中核
- [ ] **task-7**: edge.js — runViewerResponse
  - 操作対象: `nginx/njs/edge.js`
  - 操作内容: `runViewerResponse` 追加（task-3 の `runEdgeFunction` helper + `snapshotResponse` を再利用）。viewer-response の AWS 制約（status 変更不可）に整合し **status は上書きせず headers のみ適用**（Go の translator が既に status を強制保持しているので njs 側も status を触らない）。`export default` 追記。fail-open は上流応答素通し
  - 影響場所と効果: njs が cache/origin 応答を viewer-response Lambda と往復し headers を transient 適用
  - goalへの影響: viewer-response の njs 連携本体
- [ ] **task-8**: examples 4 フック完成 + docs 4 フック化 + BL-LE1 完全解消 + walkthrough + 完了メモ
  - 操作対象: `examples/lambda-edge-full/{main.tf,docker-compose.yml,lambdas/viewer-response/index.js,README.md}`, `docs/lambda-edge.md`, `docs/limitations.md`, `.claude/design/lambda-edge-remaining-hooks-2026-05-09.md`, `.claude/plan.md`, `.claude/tasks.md`
  - 操作内容: viewer-response の `lambda_function_association` 追加 → main.tf が **4 フック組合せ**に。docker-compose に `lambda-viewer-response` RIE 追加（計 4 RIE。**R-5: 4 RIE 並列のメモリ枯渇を実測し README に注記**）。デモ関数 = **CORS / `Timing-Allow-Origin` ヘッダ付与**（transient の好例）。`docs/lambda-edge.md` を 4 フック構成図・設定・return 仕様に完成。`docs/limitations.md` の `BL-LE1` を**完全解消マーク**（`BL-LE-Cache1` は F2=A 達成で解消 or topology が候補β/部分なら残課題化）。Terraform 4 フック apply → walkthrough R-6。design doc に Phase 完了メモ（spike-A/B 結果・B1〜B3 を DESIGN.md 更新候補として列挙）。**bookkeeping**: `.claude/plan.md` ステータス表で phase-4e を「完了(PR #23)」へ・phase-4f を追加、`.claude/tasks.md` の旧採番セクション整理
  - 影響場所と効果: 4 フット完全カバー（M4 完成）+ 制約と使い方が実態に一致
  - goalへの影響: BL-LE1 完全解消 = M4 達成の対外宣言

> **note（短絡応答の用語）**: 当初要件の「origin-response の短絡応答(status 直接生成)」は実コードで決着済 — `TranslateOriginResponseResponse`(L416) は常に `ActionContinue + Response`、`TranslateViewerResponseResponse`(L438) は status 強制保持で headers のみ。**response 系に独立した ShortCircuit 経路は無い**（Lambda が status/headers を生成しても「改変済み応答を cache 格納/transient 適用」に畳まれる）。よって response 系の α は **Continue + LambdaError の 2 ケース**で正しく（merge 済 α と一致）、ShortCircuit ケースは追加しない。

---

## 未確定・要判断事項

> F-A〜F-C は **spike が一次解決者**（実機で潰す）。ただし spike NG 時の retreat は goal/security に効くので人間判断に返す。F-D/F-E は構造判断。

### F-A [goal 影響・spike-gated] origin-response cache-write が spike-A で不成立だった場合の retreat
F2=A は「outer `proxy_cache` が js_content `r.return()` 合成応答をキャッシュする」ことに依存。これは既存 spike の射程外で**未証明**。spike-A acceptance(a) が NG の場合:
- **(A 続行)** fallback 候補β（inner-B 自体を js_content 化 + `$cf_origin_url` 注入 + ttl filter 手動再現）で AWS 準拠 cache-write を追う。コスト増・複雑度増。
- **(B retreat)** origin-response を **transient 改変のみ**（cache には origin 原本を格納）に落とし、cache-write を `BL-LE-Cache1` として v0.3 へ先送り。M4「完全」がやや弱まる。
- → **spike-A の結果を見て判断**。NG なら停止してユーザーに retreat 可否を確認（goal の M4 完全性に効く）。

### F-B [security 影響・spike-gated] inner self-fetch が unix socket に届かない場合
候補αは `ngx.fetch` が inner **unix socket** に到達することを前提。spike-A acceptance(b) が NG の場合:
- **(A)** inner server に loopback TCP listener（例 `listen 127.0.0.1:8081;`）を**復活**させ self-fetch に使う。→ `BL-NX1`（4c-7 で inner を unix socket 専用化しバイパス穴を構造的に削除）の **security regression**。
- **(B)** 候補β（origin 直 fetch）に倒し inner 構造を温存。
- → unix socket 不達なら停止して「security hardening を一部戻すか / 候補β か」をユーザー確認。

### F-C [goal 影響・spike-gated] viewer-response の 3-hop が spike-B で regression を出す場合
viewer-response hop は全 cacheable 応答経路（HIT/MISS 共通）に乗るため blast radius が大きい。spike-B で viewer-request/origin-request の regression、または HIT 経路での hop 不発火が出た場合は、PR-2 を停止し topology を再設計（最悪 viewer-response を phase-4g に再分離）。**PR-1（origin-response）は PR-2 と独立に merge 可能**なので、F-C が起きても M4 の半分は確保される。

### F-D [構造] spike を PR 内ゲートにするか PR-0 で前倒すか
- **(A 推奨)** 各 PR の先頭タスク（task-1 / task-5）を spike ゲートにする。origin-response を早く ship でき、grouping=A（stacked）と整合。
- **(B)** spike-A + spike-B を **PR-0** にまとめ、両 topology を先に de-risk してから実装。phase 全体の不確実性を前倒しで潰せるが origin-response ship が遅れる。
- → 推奨は (A)。spike-A と spike-B は独立なので分離してよい。

### F-E [小・要件再解釈] origin-response の body 生成/削除の扱い
B3 で body **読取**は不可だが、AWS では origin-response Lambda が body を**生成/削除**は可能。本フェーズは要件を **status/headers 改変に限定**（body 生成/削除は対象外、要望時 BL 起票）でよいか。→ **Yes 推奨**（典型用途はヘッダ付与。body 生成は別 BL）。

### 解決済み（moat から降ろした論点）
- **短絡応答の用語**: response 系は Continue+Response のみ（ShortCircuit 経路なし）。実コードで決着 → α は Continue+LambdaError の 2 ケースで確定。
- **docs/limitations.md 参照行**: critic が誤参照を疑ったが、実ファイルで `BL-LE1`「部分解消」は L125/L190、`BL-LE-Cache1` は L198 に**実在・正確**。

---

## PR仕様（/goal:exec はこの仕様どおりに PR 本文を書く）

> 全 PR 共通規約（memory）: base = develop、ブランチ `feat/phase-4f-...`。review 指摘は worktree で stacked PR。PR 本文は一時ファイル + `--body-file`（heredoc 禁止）。Mermaid で構造図必須。

### PR-1: phase-4f — origin-response 縦スライス（cf-local）
- **目的**: origin-response フックを実機で動かす。cache MISS 時 origin 接続後に Lambda を呼び、改変 status/headers を **outer `proxy_cache` が格納**する（F2=A, AWS 準拠）。
- **満たすべき要件**: task-1（spike-A memo / アーキ確定）/ task-2（conf.go origin-response topology + renderer テスト）/ task-3（edge.js `runOriginResponse` + `runEdgeFunction` helper 抽出）/ task-4（examples・docs の origin-response 分 + R-6 walkthrough）。`go test ./...` 緑 + viewer-request/origin-request α regression 維持。
- **着手前の立ち位置**: Go 層 4 フック完備・data-plane は viewer-request + origin-request のみ。origin-response の cache-write topology は未証明。
- **完了後の立ち位置**: viewer-request + origin-request + **origin-response** が end-to-end 稼働。origin 応答の Lambda 改変が cache に乗る。
- **作業フロー図**:
  ```mermaid
  flowchart TD
    A[Go層4フック完備 / data-plane=viewer+origin-request] --> S[task-1 spike-A: origin-response cache-write topology 確定ゲート]
    S -->|成立| B[task-2 conf.go: inner-C origin-response topology + renderer test]
    S -->|NG| R[F-A/F-B retreat をユーザー確認して停止]
    B --> C[task-3 edge.js: runOriginResponse + runEdgeFunction helper 抽出]
    C --> D[task-4 examples/docs origin-response + R-6 walkthrough]
    D --> E[origin-response が cache-write で稼働 / α regression 維持]
  ```
- **解決タスクと goal への効果**: task-1〜4。M4 を request 側 + origin-response で前進（残り viewer-response は PR-2）。
- **PR外への影響**: outer の `proxy_pass` ターゲット切替と inner-C 追加は origin-request の inner 経路に隣接するため、**viewer-request/origin-request の α regression を必ず確認**（要件 5 / R-3）。Go 層・control-plane は無変更。
- **その他共有事項**: spike-A が NG なら本 PR 内で F-A/F-B の retreat を提起して停止（勝手に候補βや TCP 復活へ倒さない）。

### PR-2: phase-4f — viewer-response 縦スライス（cf-local、PR-1 に stack）
- **目的**: viewer-response フックを実機で動かす。cache HIT/MISS 共通で viewer 返却直前に Lambda を呼び **headers を transient 改変**（status 不変・cache 不変）。4 フット完全カバーで `BL-LE1` 完全解消・M4 完成。
- **満たすべき要件**: task-5（spike-B memo / アーキ確定）/ task-6（conf.go viewer-response transient hop + viewer-request 組合せ + renderer テスト）/ task-7（edge.js `runViewerResponse`）/ task-8（examples 4 フック完成 + docs 4 フック化 + `BL-LE1` 完全解消マーク + R-5 メモリ実測 + R-6 walkthrough + bookkeeping）。
- **着手前の立ち位置**: viewer-request + origin-request + origin-response 稼働。viewer-response の transient topology は未設計。
- **完了後の立ち位置**: **4 フック完全カバー（M4 完成）**。`examples/lambda-edge-full/` が 4 フック自己完結 compose。
- **作業フロー図**:
  ```mermaid
  flowchart TD
    A[origin-response まで稼働] --> S[task-5 spike-B: viewer-response transient hop 確定ゲート]
    S -->|成立| B[task-6 conf.go: viewer-response transient hop + viewer-request 組合せ + renderer test]
    S -->|NG/regression| R[F-C: 再設計 or phase-4g 再分離をユーザー確認]
    B --> C[task-7 edge.js: runViewerResponse - helper 再利用]
    C --> D[task-8 examples 4フック完成 + docs + BL-LE1 完全解消 + walkthrough + bookkeeping]
    D --> E[4フック完全カバー / M4 完成]
  ```
- **解決タスクと goal への効果**: task-5〜8。BL-LE1 完全解消 = M4 達成の対外宣言。
- **PR外への影響**: outer location の js_content 化（transient hop）は**全 cacheable behavior の応答経路**に乗るため、viewer-request/origin-request/origin-response 全経路の α regression + spike-B での HIT/MISS 検証が必須（F-C / 要件 5）。
- **その他共有事項**: PR-1 と独立に評価可能（origin-response は PR-1 で確保済）。F2=A で `BL-LE-Cache1` が解消できたか、topology が部分的なら残課題化を docs に明記。4 RIE 並列のメモリ実測（R-5）を README に注記。

---

plan.md を確認・編集のうえ /goal:exec を実行してください。特に **F-A〜F-C（spike NG 時の retreat 方針）** と **F-D（spike を PR 内ゲートにするか PR-0 か）** を頭に入れてから exec に入るのを推奨します（spike 結果次第で PR 構成と goal 完全性が動くため）。
