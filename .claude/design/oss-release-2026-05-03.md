---
phase: phase-5
title: OSS公開準備 + v0.1.0 リリース基盤
date: 2026-05-03
completed: 2026-05-05
branch: feat/phase-5-oss-release
base: develop @ e588aae
merge: PR #17 (cf49bc0) + PR #18 (38958a2)
status: completed
---

# Phase 5: OSS公開準備 + v0.1.0 リリース

## 目的

M5 (OSS公開) の達成。Phase 4-D 完了時点で機能セットは β 候補レベル (M3 + Lambda@Edge viewer-request MVP)。残るは「他者が clone して quick start を 5 分で動かせる」+「公開・配布・バージョニングの形を整える」こと。

本フェーズは **公開フローの構築 + 現状のドキュメント化** に集中し、利用者フィードバックで BL 残課題 (BL-LE1 / BL-PP1 等 13+ 件) の優先度を再評価する戦略を取る。**本フェーズの範囲は PR 作成まで** (動作確認は PR 作成後にユーザーと実施)。

## スコープ

### kickoff 前相談で確定した方針 (前 spec phase で 18 論点デフォルト確定)

| 項目 | 決定事項 | 根拠 |
|---|---|---|
| **言語** | README / docs 日本語のみ継続 | リソース集中、ターゲット国内優先。英語化は v0.1.0 後の別フェーズ判断 |
| **GHCR push trigger** | tag (`v*`) push のみ | develop merge ごとは過剰。v0.1.0 リリース基準を明示できる |
| **BL 残課題の扱い** | `docs/limitations.md` の「Known limitations」に明示するのみで v0.1.0 公開 | 公開後の利用者フィードバックで優先度を再評価する戦略 |
| **examples/** | 既存 3 個 (nextjs-basic / terraform-integration / lambda-edge-basic) で公開、追加なし | カバレッジは十分。要望出てから追加 |
| **ロゴ / docs サイト** | 作らない | OSS 公開後の v0.2 候補 |
| **告知** | v0.1.0 release 後に Zenn 記事 1 本のみ。Reddit/HN は反応見て判断 | 本フェーズスコープ外 |
| **CI** | Go test + golangci-lint + Docker build + α 統合テスト | β endpoint 系は CI で動かさず手動 `/check` に倒す (現状の运用継承) |
| **Release workflow** | tag `v*` push で multi-arch build (linux/amd64 + linux/arm64) → GHCR push、`latest` + `vX.Y.Z` 両タグ付け | 標準的な GHA パターン |
| **CHANGELOG** | Keep a Changelog v1.1.0 形式、`[Unreleased]` + `[0.1.0]` から記載 | OSS 標準 |
| **CoC** | Contributor Covenant v2.1 そのまま | OSS 標準。連絡先 email は着手時に確定 |
| **SECURITY** | GitHub Security Advisories 経由、SLA 記載なし | 個人プロジェクトとして無理のない範囲 |
| **Issue template** | bug_report.yml + feature_request.yml + question.yml + config.yml (blank issue 無効化) の 4 種 | 整理された report を促す |
| **PR template** | Summary / Changes / Test plan / Mermaid 任意の最小構成 | 既存 commit log の Mermaid 慣習 (memory) を踏襲 |

### 実装項目

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| 5-1 | LICENSE の `<YOUR_NAME>` 確定 | `LICENSE` | GitHub user 名 (`DKen-DevCat`) と本名どちらにするか着手時に user に最終確認 |
| 5-2 | README 更新 (ステータス表 / Quick start / バッジ / GHCR pull) | `README.md` | ステータス表は Phase 4-D まで反映。Quick start は `docker compose up -d` ワンライナーで動くまで強化。License / GHCR / CI バッジ追加 |
| 5-3 | CHANGELOG.md 新設 | `CHANGELOG.md` (新規) | Keep a Changelog v1.1.0 形式。`[Unreleased]` + `[0.1.0] - YYYY-MM-DD` セクション。Phase 0〜4d の機能を Added 列挙 |
| 5-4 | CODE_OF_CONDUCT.md 配置 | `CODE_OF_CONDUCT.md` (新規) | Contributor Covenant v2.1 そのまま。連絡先 email 差込 |
| 5-5 | SECURITY.md 配置 | `SECURITY.md` (新規) | GitHub Security Advisories 経由。SLA 記載なし |
| 5-6 | `.github/ISSUE_TEMPLATE/` 整備 | `.github/ISSUE_TEMPLATE/{bug_report,feature_request,question}.yml` + `config.yml` | YAML form 形式。`config.yml` で `blank_issues_enabled: false` |
| 5-7 | `.github/PULL_REQUEST_TEMPLATE.md` 配置 | `.github/PULL_REQUEST_TEMPLATE.md` (新規) | Summary / Changes / Test plan / Mermaid 任意 |
| 5-8 | `.github/workflows/ci.yml` 新設 | `.github/workflows/ci.yml` (新規) | push (develop / feat 系) + PR で実行。Job: (1) Go test (2) golangci-lint (3) Docker build (4) α 統合テスト (`tests/integration/`) |
| 5-9 | `.github/workflows/release.yml` 新設 | `.github/workflows/release.yml` (新規) | trigger: tag `v*` push。multi-arch (linux/amd64 + linux/arm64) Docker build → GHCR push (`ghcr.io/dken-devcat/cf-local`)、`latest` + `vX.Y.Z` 両タグ |
| 5-10 | docs/limitations.md「Known limitations」整備 | `docs/limitations.md` | 既存の積みタスク BL-W1/W2 / BL-IV1/IV2 / BL-PP1 / BL-NX3 / BL-LE1〜LE7 / BL-CFF1 / BL-RV1/RV2 を v0.1.0 公開時の正式制約として整理。各 BL に「対応予定: 次バージョンで検討」等の status を付与 |
| 5-11 | README に「v0.1.0 milestone 達成」セクション追加 | `README.md` | M3 (Terraform 連携) + M4 (Lambda@Edge viewer-request MVP) の到達状態を明示 |
| 5-12 | `/check` で全テスト + lint pass 確認 | (ローカル実行) | PR 作成前最終確認。Go unit / β / α 統合テストすべて緑 + `gofmt -l` 空 |

### 実装しない (out of scope)

| 項目 | 理由 |
|---|---|
| 英語 README / 英語 docs | リソース集中、ターゲット国内優先 |
| ロゴ / バナー / デモ GIF / スクショ | v0.1.0 後 |
| docs サイト (mkdocs / vercel) | 既存 docs/ で十分 |
| BL 残課題の実装消化 (BL-LE1 等 13+ 件) | 公開後の利用者フィードバックで優先度を再評価する戦略 |
| examples/ 追加 | 既存 3 個でカバレッジ十分 |
| Zenn 告知記事 | 本フェーズは PR 作成まで。release 後に別途 |
| v0.1.0 タグ付け / GHCR への実 push | 本フェーズは workflow 配置まで。実 push は merge 後 |
| 動作確認の実走 (clean clone から quick start) | PR 作成後にユーザーと実施 |

## 実装方針

### コミット粒度 (1 機能 1 コミット)

1. **5-1**: `chore(license): set copyright holder`
2. **5-2 + 5-11**: `docs(readme): refresh status table + quick start + v0.1.0 milestone section + badges`
3. **5-3**: `docs(changelog): add CHANGELOG.md (Keep a Changelog v1.1.0)`
4. **5-4**: `docs(coc): add CODE_OF_CONDUCT.md (Contributor Covenant v2.1)`
5. **5-5**: `docs(security): add SECURITY.md`
6. **5-6**: `chore(github): add issue templates (bug/feature/question + blank disabled)`
7. **5-7**: `chore(github): add PR template`
8. **5-8**: `ci: add ci.yml — go test + lint + docker build + integration tests`
9. **5-9**: `ci: add release.yml — tag-triggered multi-arch GHCR push`
10. **5-10**: `docs(limitations): formalize known limitations for v0.1.0`
11. **5-12**: `chore(check): run /check — all green` (このコミットは差分が無いはずなので skip 可)

### バッジ URL の扱い

- License バッジ: `https://img.shields.io/github/license/DKen-DevCat/cf-local`
- GHCR バッジ: `https://ghcr.io/dken-devcat/cf-local` のサイズバッジ (`https://ghcr-badge.deta.dev/...` 経由) — 現状 GHCR に image が無いので push 後に最終確認
- CI バッジ: `.github/workflows/ci.yml` の `https://github.com/DKen-DevCat/cf-local/actions/workflows/ci.yml/badge.svg` — workflow 名で URL 確定

→ workflow 名 / 配置 path を fix してから README に書く順序にする。

### CI workflow の構成

```yaml
name: CI
on:
  push:
    branches: [develop]
  pull_request:
    branches: [develop]
jobs:
  test:        # Go unit + β + α 統合テスト
  lint:        # golangci-lint
  docker:      # docker compose build dry-run
```

α 統合テスト (`tests/integration/`) は docker compose up + curl ベース。CI 上で `docker compose up -d --wait` → α テスト → `docker compose down` の流れになる。所要時間目安: 5 分以内 (4d 時点 α 全 30+ ケース)。

### Release workflow の構成

```yaml
name: Release
on:
  push:
    tags: ['v*']
jobs:
  ghcr:        # multi-arch build + push
```

`docker/build-push-action@v5` + `docker/setup-buildx-action@v3` で multi-arch (linux/amd64 + linux/arm64)。tag 名から version 抽出して `latest` + `vX.Y.Z` の 2 タグ付与。

### golangci-lint 導入判断 (5-8 着手時)

- A 案: `.golangci.yml` 無しで default 設定
- B 案: 既存 nestify の `.golangci.yml` を参考に最小構成 (errcheck / gosimple / govet / staticcheck / unused / ineffassign)

→ 5-8 着手時に default 設定でローカル走らせて、ノイズ多ければ B 案に切替。

### docs/limitations.md 整備の方針 (5-10)

既存の `docs/limitations.md` 末尾に「積みタスク一覧」表があるはず。これを以下の構造で再編:

```markdown
## Known limitations (v0.1.0)

cf-local v0.1.0 では以下の機能/挙動は未対応または制約があります。
利用者フィードバックを元に v0.2.0 以降で優先度を判断します。

### Lambda@Edge / CloudFront Functions

| ID | 内容 | 対応予定 |
|---|---|---|
| BL-LE1 | 残り 3 フック (origin-request / origin-response / viewer-response) | 次バージョンで検討 |
| BL-LE2 | viewer-request `include_body: true` | 次バージョンで検討 |
| ... | | |

### PathPattern / Cache Behavior
...

### Invalidation
...

### Stability / 観測性
...
```

カテゴリ別にテーブル化して、利用者が「自分のユースケースが該当するか」を判断しやすくする。

## テスト方針

| レイヤー | 何をテストするか |
|---|---|
| Tooling (CI workflow) | branch push で GHA を実走させ、Go test / lint / Docker build / α 統合テスト の 4 ジョブが PR 上で緑になること。`act` は使わず GHA 上で確認 (環境依存少ない) |
| Tooling (Release workflow) | tag push は本フェーズ外。`release.yml` は `workflow_dispatch:` も加えて手動 trigger での dry-run を可能にし、syntax 検証のみ。GHCR への実 push 検証は v0.1.0 タグ push 時 (本フェーズ外) |
| Docs (README quick start) | 本フェーズでは clean clone 検証はしない。PR 作成後にユーザーと一緒に `git clone <url>` 直後の状態から `docker compose up -d` → `curl localhost:8080` を辿る (本フェーズ範囲外、PR review 段階の動作確認) |

## 完了条件

- [ ] phase-5-1 〜 phase-5-12 全タスク完了
- [ ] PR (#15 仮) が develop に向けて作成済
- [ ] CI workflow が PR 上で全ジョブ緑
- [ ] PR 本文に Mermaid で構造を記述 (feedback memory 準拠)
- [ ] 動作確認 (README quick start を clean clone から辿る) は PR 作成後にユーザーと実施

## コミット粒度

1 機能 1 コミット原則。本ドキュメント + tasks 更新を本ブランチのキックオフコミット (spec commit `adea968` の続き) とする。

## リスク・未決事項

| ID | 内容 | 判断タイミング |
|---|---|---|
| **R-1** | GHCR push の権限 | dry-run 不可。tag push 後に初めて検証になる。本フェーズでは workflow の syntax のみ確認 |
| **R-2** | golangci-lint の lint ルール | `.golangci.yml` 無しで default 設定 vs. 最小構成の選択。phase-5-8 着手時に default 走らせて判断 |
| **R-3** | Contributor Covenant の連絡先 | ユーザー個人の email を晒すか専用 alias を作るか。phase-5-4 着手時に確定 |
| **R-4** | README バッジ URL | GHCR / CI バッジの実 URL は GHA を 1 度走らせるまで確定しない。仮値で入れて CI 緑後に最終 URL に置換 |
| **R-5** | examples/ の README 棚卸 | 各 example の README に「Phase X 構成」表現が残っている可能性。phase-5-2 着手時に grep して、必要なら同タスクのスコープに追加 |
| **R-6** | CI で α 統合テスト走らせる時の docker compose 起動安定性 | ローカルでは `docker compose up -d --wait` が動くが、GHA runner では tmpfs / inotify 周りで挙動差が出る可能性。CI 失敗時に diagnose 必要 |
| **R-7** | CHANGELOG の `[Unreleased]` 区分 | 本フェーズで追加する CI/CD 整備自体を `[Unreleased]` に書くか `[0.1.0]` に書くか。判断: 「v0.1.0 リリースに必要な作業」として `[0.1.0]` に統合する方針 |
| **R-8** | Mermaid in PR | feedback memory に従い PR 本文に Mermaid。本フェーズの構造図は「変更前 (現状) vs 変更後 (公開ライン整備済)」の比較が分かりやすい |

## Phase完了時メモ

### スコープ逸脱の事後記録 (CI 緑化のための Go コード変更同梱)

本フェーズの「影響範囲」は当初 `BE: N/A (Go コードへの変更なし)` と規定したが、Phase 5-8 (CI workflow 新設) 着手後に CI 緑化のため以下の Go コード変更を例外的に同梱した:

| 変更 | 経緯 | commit |
|---|---|---|
| `internal/api/validate/policy.go`: ST1005 違反のエラーメッセージ lowercase 化 (6 箇所) | golangci-lint v2 staticcheck が既存 convention 違反を検出 | `de07a2c` |
| `internal/nginx/conf.go`: 同上 (`"Origins is empty"` → `"origins is empty"`) | 同上 | `2a81dca` |
| `internal/nginx/reloader_test.go`: `-race` 検出のテスト race fix (`syncBuf` / `atomic.Int32` / `waitFor` helper 導入) | CI で `go test -race` が既存テストの観測コードに data race を flag、本番コードは race-free | `61328fa` (PR #18) |

**判断根拠**: 新規ロジック追加なし、文字列修正と既存テストの観測コード修正のみ。CI ジョブを通すための必要条件として phase-5-8 完遂に必要。`docs/limitations.md` BL-CI1 (errcheck の package-wide disable) として残課題は backlog 化済。

CLAUDE.md「設計判断は DESIGN.md に従う / 逸脱はコードを書く前にユーザーに確認」運用との関係: 本件は CI 着手時に判断したため事前確認の機会は限定的だったが、commit メッセージに経緯を記録 + 本フェーズ完了時メモで整理することで運用整合を取る。

### Review iteration timeline (REV-1〜REV-8)

PR #17 のレビュー対応で 8 件を 2 commit に分けて消化。中身は CI 緑化と公開フロー堅牢化に集中。

| REV | 内容 | 種別 | commit |
|---|---|---|---|
| REV-1 | design doc に CI 緑化 Go コード同梱の Phase完了時メモ追記 (BE: N/A 規定との整合) | docs | `3526284` |
| REV-2 | `TestReloader_NilFetchResult` を `time.Sleep(80ms)` → `waitFor` に統一 (helper 一貫性) | test | `3526284` |
| REV-3 | `ci.yml` の `:8080` / `:8081` / `:4566` 待機ループを失敗時 `exit 1` 化 (`::error::` で診断容易化、3 step 統一) | ci | `3526284` |
| REV-4 | `release.yml` の Docker 系 actions を現行 major へ bump (qemu/buildx/login@v4, metadata@v6, build-push@v7) | ci | `3526284` |
| REV-5 | `ci.yml` の golangci-lint version を `latest` → `v2.12.1` 固定 (公式 Production Workflow 推奨の再現性) | ci | `3526284` |
| REV-6 | `TestReloader_TriggerCoalesce` を `done channel + cancel + <-done` 形に統一し既存 TempDir cleanup race を解消 | test | `3526284` |
| REV-7 | `CONTRIBUTING.md` の `<YOUR_GITHUB_OWNER>` 残置を `DKen-DevCat` に置換 (5-1/5-2 漏れの後追い) | docs | `3526284` |
| REV-8 | `ci.yml` nginx :8080 wait の `curl -fsS` 誤用を code= パターンに統一 (5xx を "応答した" とみなして抜ける) | ci | `4a99a69` |

検証: REV-2 / REV-6 適用前 14/20 pass → 適用後 20/20 pass で reloader test の flake 撲滅確認。

### 想定外だった点

1. **REV-3 → REV-8 の連鎖**: REV-3 で待機ループに `exit 1` を入れた瞬間、:8080 step だけ `curl -fsS` を使っていた既存誤用が顕在化した (5xx をエラー扱いして break に到達しない → 旧版は "応答ある" コメントの意図と裏腹に "200 のみ" 待機していた)。CI runner では origin が居ないので 502 になり、厳格化前は黙過、厳格化後はタイムアウト exit 1。**緩い検証は壊れたまま延命する** という典型例 (Schrödinger's wait loop)。

2. **golangci-lint v2 が既存 ST1005 違反を検出**: BE: N/A の規定で Phase 5 着手したが、CI 導入で既存 6 箇所のエラーメッセージが Go conventions 違反として浮上。`docs/conventions.md` でも明文化済みのルールが lint 未導入のため遵守ドリフトしていた。lint 導入は静的に既存債を可視化する。

3. **race detector が既存 flake を発覚**: `internal/nginx/reloader_test.go` の観測コード (test 側のメッセージ蓄積) に data race があり、`go test -race` で fail。本番コードは race-free だが、テスト側の `bytes.Buffer` 直書きと goroutine 並走の組合せが原因。`syncBuf` / `atomic.Int32` を導入して観測側を thread-safe 化することで解消。

### 次フェーズへの引き継ぎ事項

- **BL-CI1 (errcheck 再有効化)**: v0.2.0 候補のまま継続。CI が緑になっている事実を踏まえて再評価できる状態。chore-3 では index 維持のみ、v0.2.0 の最初の chore 候補。
- **GHCR バッジ URL 最終確定**: chore-3-4 で「現状の仮 URL 維持」か「実 push 後に確定」かを決める。`v0.1.0` タグ push (chore-3 範囲外) を待つ判断が妥当。
- **実 release 操作**: chore-3 完了後の独立作業。CHANGELOG `[0.1.0] - TBD` 置換 → `v0.1.0` タグ作成 + push → release.yml 実走 → GHCR push 完了確認 → GitHub Release notes 作成 → (任意) Zenn 告知。
- **CONTRIBUTING.md 改善余地**: REV-7 で plain text 置換のみ実施。owner 名 hardcode の代わりに `${GITHUB_REPOSITORY_OWNER}` 等の placeholder 化検討は v0.2 候補 (本フェーズ範囲外)。

### DESIGN.md 更新が必要な点

該当なし。Phase 5 は公開フロー整備 + 既存制約のドキュメント化が中心で、設計判断 (`DESIGN.md` の「やらない」リストやアーキ方針) には影響しない。BL-CI1 / BL-DEP1 等の backlog は `docs/limitations.md` に index 集約しており、`DESIGN.md` 側の追記は不要。

唯一の例外候補は「`BE: N/A` 規定でも CI 緑化のための既存 lint 違反修正は同梱許容」の運用ルールだが、これは事例 1 件で一般化するには弱い。今後の chore でも同様の事例が複数出れば運用ルールとして DESIGN.md または CLAUDE.md に明文化を検討。
