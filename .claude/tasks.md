# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase chore-2 check.md D-2 セクションの手順誤記修正 (2026-05-03) — `.claude/plan.md` の chore-2 セクションと `.claude/design/check-md-d2-fix-2026-05-03.md` を参照。タスク履歴は `.claude/tasks-archive/chore-2-2026-05-03.md`。前回 Phase 4b: `.claude/tasks-archive/phase-4b-2026-05-03.md`、Phase 4a: `.claude/tasks-archive/phase-4a-2026-05-02.md`、Phase chore-1: `.claude/tasks-archive/chore-1-2026-05-01.md`、Phase 3: `.claude/tasks-archive/phase-3-2026-04-30.md`)

---

## Phase 4c: 仕上げ — 進行中

ブランチ: `feat/phase-4c-polish`（develop @ `0eae0e7` 起点）

設計: [`.claude/design/polish-2026-05-03.md`](design/polish-2026-05-03.md)

kickoff 前相談で確定した方針:

- **A-1 エラー互換**: 中程度 (CloudFront `<ErrorResponse>` 形式 + 主要 Code)
- **A-2 ログ**: `log/slog` 人間可読 default + `CF_LOCAL_LOG_FORMAT=json` で JSON 切替
- **A-3 デバッグ UI**: 作らない (out)
- **D ResponseHeadersPolicy**: `CustomHeadersConfig` + `CorsConfig` のみ実装

### 実装タスク

- [x] **4c-1**: ResponseHeadersPolicy CRUD (`aws_cloudfront_response_headers_policy`) — `internal/api/responseheaderspolicy/` 新規 (store + bolt + handler + id) + `internal/api/xml/response_headers_policy{,_convert}.go`。全 5 sub-config を accept + 永続化、`SecurityHeadersConfig` / `ServerTimingHeadersConfig` / `RemoveHeadersConfig` は `log.Printf` で警告。Bolt bucket `response_headers_policies` 追加、reloader.Trigger 配線、route `POST/GET/PUT/DELETE /2020-05-31/response-headers-policy{/id}`
- [x] **4c-2**: distribution との関連付け (cache behavior 経由で `add_header` directive を nginx.conf に注入) — `internal/config/loader.go` (LoadResult.ResponseHeadersPolicies 追加) / `internal/nginx/{renderer,conf,response_headers}.go` (CustomHeaders + Cors のみ反映、Override=true で `proxy_hide_header` 注入、unsafe header name/value は drop) / `cmd/cf-local/main.go` snapshot で rhpStore 配線。CRLF / `"` injection guard 込みのテーブル駆動テスト 14 ケース + render-end-to-end テスト 2 件 PASS
- [x] **4c-3**: AWS 互換エラー形式 `<ErrorResponse><Error><Code>...</Code><Message>...</Message></Error></ErrorResponse>` の共通ヘルパー + 既存ハンドラ移行 — `internal/api/xml/codes.go` 新規 (typed `Code` 定数 + `statusForCode` + `WriteError` / `WriteInternalError`)。13 コード定数 (NoSuch*4, *AlreadyExists*4 + EntityAlreadyExists, InvalidArgument, MalformedXML, IllegalUpdate, InvalidIfMatchVersion, PreconditionFailed, InternalError) を追加。全 6 ハンドラ (cachepolicy / distribution / originrequestpolicy / responseheaderspolicy / invalidation / tagging) を `WriteError` / `WriteInternalError` に移行、`WriteXMLError` は escape hatch として保持。codes_test.go で status マッピング 17 ケース + envelope 2 ケース PASS
- [x] **4c-4** (BL-IM1): managed CachePolicy `IllegalUpdate` AWS 正規コード確認 — `aws-sdk-go-v2 v1.62.0/deserializers.go` で UpdateCachePolicy / DeleteCachePolicy が `IllegalUpdate` を valid response code として smithy spec 由来で登録していることを確認。SDK が解釈可能なコードを返している以上 Provider/CLI 側の regression は無い (実 AWS 挙動の正確な確認は phase-5 で差し替え検討)。handler のコメントを更新
- [x] **4c-5** (BL-OB1): HTTP request log middleware — `internal/api/middleware/log.go` 新規 (RequestLog wraps mux、recordingResponseWriter で status + bytes 捕捉、`X-Cf-Local-Request-Id` を response header と context に伝播)。`internal/api/server.go` に `Logger` field 追加 + Run 時に middleware を挿入。`cmd/cf-local/main.go` で `configureSlog(CF_LOCAL_LOG_FORMAT)` (default text / `json` 切替) を初期化。テスト 7 件 (attrs / 200 default / context propagation / error status / double WriteHeader / IDGen fallback / nil context) PASS
- [ ] **4c-6**: 既存 `log.Printf` を slog に統一 — repo 全体で段階的に置換、log level (debug/info/warn/error) 整理
- [ ] **4c-7** (BL-NX1): inner location の unix socket 化 — `nginx/nginx.conf` / docker-compose。`upstream self { server unix:/run/cf-local-inner.sock; }` + inner-only `server { listen unix:...; }`、127.0.0.1 バイパス穴を塞ぐ
- [ ] **4c-8** (BL-NX2): stress test (vegeta 1000 RPS / 1 分) — `tests/stress/` 新規 or `examples/`。unix socket 化前後で計測 → ベースライン記録
- [ ] **4c-9** (BL-LD1): Go loader sanitize — `internal/store/loader.go` 等。njs validation 相当 (path / method / header 名の正規化) を Go 側で再実装
- [ ] **4c-10**: `docs/limitations.md` 完成 — phase-3/4a/4b/4c で判明した未対応項目をすべて反映
- [ ] **4c-11**: 積みタスク BL-W1/W2/IV1/IV2/PP1/NX3 を `docs/limitations.md` に明記 (実装しない / 制限ありを明示)
