# Goal: Phase 4-F (Lambda@Edge response 系 2 フック) の品質レビューを完了し、ディベートで採用確定した指摘のみを修正タスク化する

`feat/phase-4f-viewer-response` ブランチ（develop に対し約 1726 行差分・PR 作成待ち）を 6 観点（コード構成 / 可読性 / 設計整合 / セキュリティ / パフォーマンス / その他品質）でレビューし、肯定派・否定派ディベートで**採用確定した指摘のみ**を実行可能タスクにする。/goal:exec-v5 がこの plan を唯一の真実として修正を適用し、stacked draft PR で終端する。

## 背景（なぜ / 要件 / 前提）

**なぜ**: Phase 4-F は 4 フック完全対応（viewer-request + origin-request + origin-response + viewer-response）で M4 達成・BL-LE1 解消の節目。develop へマージする前に品質を担保し、混入バグ・ドキュメント乖離を潰す必要がある。

**要件**:
- 6 観点でレビューし、各指摘を肯定派（不採用寄り）/ 否定派（採用寄り）でディベートしてから採否確定する（過剰修正を避ける = 本 PJ の「重複3回未満で抽象化は過剰抽象」「やらないリスト」哲学を尊重）。
- 採用指摘のみを修正タスク化。実ファイル file:line 根拠必須（推測禁止）。
- 設計整合は DESIGN.md / docs/conventions.md / .claude/rules/ / 着手中設計doc の明文に照らす。

**前提（調査で確定済み）**:
- HIGH 級 2 件（njs URI 正規化バグ + その回帰防止）は、調査 Workflow の 3 レンズ critic（coverage / grounding / risk）が 3 ラウンド全てで実ファイル根拠付きで一致採用。orchestrator が edge.js / conf.go / docs / test を **自分で再読して全件 ground 照合済み**。
- njs の `.test.js`（cache_key/ttl/cache_control）は **nginx `js_content` エンドポイント**で `tests/*.sh` が HTTP 駆動するもの。Node 単体実行できる edge.js unit test の前例は無く、CI（.github/workflows）に njs 機能テストは無い。→ njs ランタイム経路の**自動 CI 被覆は構造的に不可能**。
- PR topology は memory の standing 指示「review 指摘は worktree で B を切って B→A の stacked PR で消化（2026-05-05）」に従う。A = `feat/phase-4f-viewer-response`、B = `feat/phase-4f-review-fixes`。

## 調査結果（goal実態 / 現状実態 / ギャップ）

### goal 実態（高品質が満たすべき基準）
- Go: `fmt.Errorf("...: %w")` wrap・小文字メッセージ・gofmt 通過。テストはテーブル駆動・name 具体・httptest。
- njs: 機能単位分割・`export default` 1 オブジェクト・複雑ロジックは Go 側。同期制約（`js_header_filter`/`js_body_filter` は async 不可）→ `js_content` ベース topology。
- ドキュメントは実コードと一致（directive 名・フック対応状況・構成図）。
- スコープ: BL-LE5（header 改変 forward 反映）は本フェーズ out。セキュリティ機能（認証/認可/暗号化）は不要 = 混入は逆に不採用。

### 現状実態（実装の到達点）
- conf.go: 4 フック topology を正しく生成（writeServerBlock 分岐は viewer-response 最外段優先 = AWS 発火順と整合。addInner dedup も正）。`go test ./internal/...` 全 PASS・gofmt -l 空・go vet 空。
- edge.js: `runEdgeFunction` helper（4 利用箇所 = 適正抽象）・4 フック runner 実装済。`runOriginRequest` は URI 正規化を正しく実装（L331-335）。
- examples/lambda-edge-full・spike・docs・limitations の BL-LE1 解消マーク済。

### ギャップ（ディベート採否表）

| # | 指摘 (file:line) | 観点 | 肯定派(不採用) | 否定派(採用) | ジャッジ |
|---|---|---|---|---|---|
| **G1** | `runOriginResponse` が Lambda payload に内部 prefix 付き URI を渡す（edge.js:386-387 の `payloadExtra` に `request` キー無し → `snapshotRequest(r).uri = /_cf_oresp_<san>/path` が `cf.request.uri` に漏れる） | バグ/設計整合 | — | 本 PR 新規 regression。`runOriginRequest` L331-335 に対称な正解パターンがあるのに非対称。Lambda が path ベースのロジックを書くと無言で誤動作。修正 +2行/1行変更 | **採用** — 不可逆リスク級・3行で直る |
| **G2** | G1 の回帰防止が無い。alpha test は njs を経由せず（`invokeEdgeProxy` が `InvokeRequest` を Go edge-proxy へ直接 POST）、fake RIE（lambda_edge_alpha_test.go:58-65）は `r.Body` 未読 | テスト/品質 | Workflow 案の Go capture test は **njs を通らず tautological**（テスト自身が組んだ URI を assert するだけ）→ そのまま採用すると誤検出 | 修正の有効性を検証する手段がゼロのままは不可。ただし**自動 CI 被覆は構造的に不可能**なので、honest な手段（R-6 walkthrough を検証可能化）に置換して採用 | **採用（形式を補正）** — Go capture 案は破棄、walkthrough 経由に変更 |
| **G3** | docs/lambda-edge.md の実装状況・directive 名が古い（L3「viewer-response は後続タスク」= 実装済なのに stale / L9 フック列挙に viewer-response 欠落 / mermaid に viewer-response transient hop 欠落 / L133 `edge.runViewerRequest` = 実際は `edge.viewerRequest`（conf.go:573,634）） | ドキュメント/可読性 | L133 は元から在った可能性（責務外?） | 本 PR が +139 行 編集したファイルの user-facing な事実誤り。1〜数行で直る。利用者を誤誘導 | **採用** — 実コードと乖離・低コスト |
| G4 | `copyOriginHeadersToOut`（edge.js:180-181）は 1 行 thin wrapper・2 call sites | 抽象化 | semantic 命名（origin headers を外に出す意図明示）の価値。直呼びにすると逆に可読性低下 | 3回未満で過剰抽象 | **不採用** — 命名価値 > 削除便益 |
| G5 | 4 本のログ helper（各 1 call site） | 抽象化 | viewer-request special-case 分岐を含み単純連結以上の責務。本体に戻すと if が増える | 1 call site で過剰抽象 | **不採用** — インライン化で可読性低下 |
| G6 | `runEdgeFunction` の `short_circuit` 分岐が response hook では到達しない | 可読性 | plan.md L123 で決着済（response 系に独立 ShortCircuit 経路無し）。分岐は全フック共通の generic helper で dead ではない | njs コメントが無い | **不採用** — 決着済・generic 共有・コメントは marginal |
| G7 | origin body を `text()` で読む（バイナリ body 文字化け）edge.js:367 | パフォーマンス | spike-A README L63 で「streaming/sendBuffer の要否は別途判断」と**意図的に deferred** と記録済 | 注記コメントが実コードに無い | **不採用** — 設計上の既知 deferred。本フェーズ scope 外 |

## 実行計画

- [ ] **task-1**: `runOriginResponse` の URI 正規化バグ修正（G1）
  - 操作対象: `/Users/ooizumiyou/cf-local/nginx/njs/edge.js`（`runOriginResponse`, L348-396）
  - 操作内容: `tail` 計算（L353）の直後に `const request = snapshotRequest(r); request.uri = tail;` を挿入。L386-387 の `runEdgeFunction(r, 'origin-response', { payloadExtra: { response: snapshot }, ... })` を `payloadExtra: { request: request, response: snapshot }` に変更。`runOriginRequest`(L331-335) と対称にする。なぜそうするかの 1 行コメント（「runOriginRequest と対称: 内部 prefix を Lambda payload に漏らさない」）を添える。
  - 影響場所と効果: origin-response Lambda に届く `cf.request.uri` が内部 prefix（`/_cf_oresp_<san>/`）を含まない正規 path になる。`fetchPath`（L354）は `tail` のままで unix socket self-fetch に影響なし。viewer-response（外段発火・prefix 無し）は無関係で不変。
  - goalへの影響: 本 PR が抱える唯一の確定 regression を解消。Lambda の path ベースロジックが正しく動く。

- [ ] **task-2**: G1 の回帰防止を walkthrough 経由で honest に成立させる（G2・形式補正版）
  - 操作対象: `/Users/ooizumiyou/cf-local/examples/lambda-edge-full/lambdas/origin-response/index.js`（受信 URI を可視化）、`/Users/ooizumiyou/cf-local/examples/lambda-edge-full/README.md`（walkthrough 検証ステップ）、`/Users/ooizumiyou/cf-local/.claude/design/lambda-edge-remaining-hooks-2026-05-09.md`（被覆限界を明記）
  - 操作内容: example の origin-response Lambda が `event.Records[0].cf.request.uri` をレスポンスヘッダ（例 `X-CF-OResp-Seen-URI`）に echo するよう数行追加。README の R-6 walkthrough に「`X-CF-OResp-Seen-URI` が `/_cf_oresp_` prefix を含まない正規 path であること」を確認する手順を追加。設計doc に「njs ランタイム経路は docker 無し CI で自動被覆できない（`.test.js` は nginx 駆動エンドポイントで unit test ではない）／Go alpha capture 案は njs を経由しないため本バグを検出できず採らない」と silent-cap 回避の明記。
  - 影響場所と効果: プロダクションコード不変（example + doc のみ）。task-1 の修正を**実際に njs を通して**手動検証でき、将来の regression を walkthrough で捕捉。被覆の限界を正直に記録。
  - goalへの影響: HIGH-2（回帰防止）を誤検出（tautological test）無しで満たす。

- [ ] **task-3**: docs/lambda-edge.md の実装状況・directive 名の修正（G3）
  - 操作対象: `/Users/ooizumiyou/cf-local/docs/lambda-edge.md`（L3, L9, mermaid L16-34, L133）
  - 操作内容: L3 を「4 フック（viewer-request + origin-request + origin-response + viewer-response）完全対応」に更新。L9 の return 仕様列挙に viewer-response を追加。mermaid に viewer-response transient hop（最外段で cache を包む経路・cache 非書込）を追記。L133 の `js_content edge.runViewerRequest;` を `js_content edge.viewerRequest;` に修正（conf.go:573,634 が実際に emit する名）。
  - 影響場所と効果: ドキュメントが実コードと一致。利用者の誤誘導を解消。
  - goalへの影響: ドキュメント品質を実装の到達点（4 フック完全対応）に揃える。

## 未確定・要判断事項

- **⚖️ PR topology（standing 指示で解決済・要事後確認）**: memory「review 指摘は B→A stacked PR で消化（2026-05-05）」に従い `feat/phase-4f-review-fixes` → `feat/phase-4f-viewer-response` の draft PR にする。代替案 = 修正を `feat/phase-4f-viewer-response` に直接コミット（PR 未作成のため bug を phase-4f PR に載せない最短路）。standing 指示を優先し stacked を champion とした。draft PR は close で可逆なので、別案を望む場合は終端報告時に指示すれば切替可能。
- **不採用にした指摘（G4〜G7）**: 過剰抽象削除・dead 分岐コメント・origin body streaming は本フェーズ scope 外 / 既知 deferred / 決着済として不採用。異議があれば task 化する。
- **回帰防止の被覆限界**: task-2 は手動 walkthrough であり自動 CI ゲートではない。完全自動化には nginx+njs を回す統合テスト基盤（cache_key_test.sh 方式の edge.js 版 or docker compose CI）が必要で、3 行バグの修正に対しては過大。本 plan では採らない（明示）。

## PR仕様（/goal:exec-v5 はこの仕様どおりに draft PR 本文を書く）

- **PR-1: `feat/phase-4f-review-fixes` → `feat/phase-4f-viewer-response`（cf-local リポジトリ・stacked）**
  - 目的: Phase 4-F 品質レビューで採用確定した指摘（njs URI 正規化バグ + 回帰防止 walkthrough + docs 整合）を解消し、phase-4f を bug-free な状態で PR 化できるようにする。
  - 満たすべき要件: G1 修正が `runOriginRequest` と対称・`fetchPath` 不変・viewer-response 不変。G2 は honest な walkthrough 検証に置換（tautological な Go test を作らない）。G3 で docs が実コードと一致。既存 Go テスト・gofmt・go vet が緑のまま。
  - 着手前の立ち位置 / 完了後の立ち位置: 着手前 = origin-response Lambda が内部 prefix 付き URI を受け取る regression を抱え、docs が viewer-response 未対応と誤記。完了後 = `cf.request.uri` が正規 path・walkthrough で検証可能・docs が 4 フック完全対応を正しく記述。
  - 作業フロー図（mermaid）:
    ```mermaid
    flowchart TD
      A[feat/phase-4f-viewer-response: review 指摘 3 件] --> B[branch feat/phase-4f-review-fixes 作成]
      B --> T1[task-1: edge.js runOriginResponse URI 正規化修正]
      B --> T2[task-2: example+walkthrough で回帰防止 honest 化]
      B --> T3[task-3: docs/lambda-edge.md 整合修正]
      T1 --> V[go test / gofmt / go vet 緑 + 手動 walkthrough]
      T2 --> V
      T3 --> V
      V --> D[draft PR: review-fixes 〆 viewer-response]
    ```
  - 解決タスクと goal への効果: task-1（確定 regression 解消）/ task-2（修正の検証可能化・被覆限界の明示）/ task-3（docs 整合）。3 件で「採用指摘のみを修正し phase-4f を mergeable に」という goal を達成。
  - PR外への影響: なし（example + docs + 1 つの njs 関数のみ。Go プロダクションコード・conf.go・viewer-response 経路は不変）。
  - Verification: `go build ./...` / `go test ./...` / `gofmt -l .`（空であること）/ `go vet ./...`。njs の挙動修正（task-1）は自動 CI 被覆不可のため **R-6 walkthrough（examples/lambda-edge-full）で `X-CF-OResp-Seen-URI` を手動確認**。docs/example は手レビュー。
  - その他共有事項: per-task commit は対象ファイルのみを `git add`（`examples/lambda-edge-full/.terraform.lock.hcl` 等の untracked stray を巻き込まない）。stacked PR の base は `feat/phase-4f-viewer-response`。

---
plan.md を確認・編集のうえ /goal:exec を実行してください（本 /loop は続けて /goal:exec-v5 を自走起動します）。
