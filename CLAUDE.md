# Claude Code Instructions

このドキュメントは、Claude Codeがこのリポジトリで作業する際の指針。**ユーザー（プロジェクトオーナー）と協働しながら、1フェーズずつ実装を進めていく**。

## 必読順序

新しい作業セッションを始めるたびに、以下を順に読むこと。

1. `README.md` - プロジェクト概要
2. `DESIGN.md` - 設計と判断理由（**特に判断理由を理解することが重要**）
3. `.claude/plan.md` - フェーズレジストリ（全体の地図）
4. `.claude/tasks.md` - 進行中フェーズの作業タスク
5. `.claude/design/<active>.md` - 着手中フェーズの設計ドキュメント
6. `docs/conventions.md` - コーディング規約

`/phase-*` 系 skill はこの構造を前提に動く。

## Skills config

`/phase-*`・`/review-diff`・`/check` 等の skill が読む設定。

```yaml
phase:
  base_branch: develop
  phase_registry: .claude/plan.md
  tasks_file: .claude/tasks.md
  tasks_archive_dir: .claude/tasks-archive
  design_dir: .claude/design
  design_filename_pattern: <slug>-<YYYY-MM-DD>.md
  branch_pattern: feat/<phase-id>-<slug>
  commit_msg_hook_requires_tasks: false
```

`commit_msg_hook_requires_tasks: false` の根拠: cf-local は `.git/hooks/commit-msg` を導入していないため、`/phase-review --fix` のコミット時に `tasks.md` への追記は強制しない。

## このプロジェクトの作業哲学

### 1. 設計判断は DESIGN.md に従う

DESIGN.mdに書かれた設計判断は、十分な検討の上で決められたもの。逸脱が必要だと感じたら、**コードを書く前にユーザーに確認する**こと。

### 2. 動くものを優先する

各フェーズの完了時点で「実際に動かして確認できる」状態を必須とする。Phase 4-Aの途中で「Goの構造はできたけどまだapplyが通らない」状態は許容しない。**動かないコードを増やすより、動く範囲を少しずつ広げる**方針。

### 3. 推測で進めない

AWSの仕様で曖昧な点、njsの機能で確信が持てない点、Terraformの実挙動で予想外の点。これらは**コードを書く前に必ず確認する**こと。確認手段は:

- AWS公式ドキュメントをWeb検索
- 実際にAWSで動かしてみる
- ユーザーに質問

「たぶんこうだろう」で書いたコードは、後で大きな手戻りを生む。

### 4. テストファースト（特にコアロジック）

cache key計算とTTL決定ロジックは、振る舞いが微妙で間違いやすい領域。これらは必ず**テストを先に書いてから実装**すること。テーブル駆動テストが推奨。

### 5. 1コミット = 1論理単位

フェーズの途中でも、動く粒度で区切ってコミット。「Phase 4-Aを完了してから1コミット」ではなく、「ストア層を追加」「distributionハンドラを追加」のように刻む。

## やらないこと

- **大規模リファクタリング**: フェーズ完了まで構造を頻繁に変えない
- **過剰な抽象化**: ベタに書いて、重複が3回出てから抽象化する
- **スコープ外機能の実装**: DESIGN.mdの「やらない」リストにあるもの
- **セキュリティ機能の実装**: これはローカル開発ツールなので、認証・認可・暗号化は不要

## 困ったときの行動指針

| 状況 | 行動 |
|---|---|
| 設計に迷う | DESIGN.md再読 → 該当箇所がなければユーザーに質問 |
| AWS仕様に迷う | 公式ドキュメント検索 → 不明ならユーザーに質問 |
| 実装方針に迷う | conventions.md再読 → 不明ならユーザーに質問 |
| テストの書き方に迷う | 既存のテストを参考にする → 不明ならユーザーに質問 |
| 「これスコープ外かも」と思う | ユーザーに質問してから進む |

**質問することはコストではなく、品質への投資**。推測で進めて手戻りする方がよっぽどコスト。

## 現在の進捗状況

`.claude/plan.md` のステータス表を見て、今どのフェーズにいるかを確認すること。新しいフェーズに着手するときは `/phase-kickoff` を使い、その時点で `.claude/design/<slug>-<date>.md` を起こす。詳細仕様が固まっていないフェーズ（Phase 1以降は概要のみ）は、ユーザーと相談しながら詳細仕様を作成してから着手する。

## ファイル/ディレクトリ規約（実装が始まったら）

```
cmd/cf-local/main.go          # Control Planeエントリポイント
internal/                     # アプリケーションロジック
  api/                        # AWS API互換ハンドラ
  store/                      # 設定ストア (BoltDB)
  nginx/                      # nginx.conf生成・reload
  managed/                    # Managed Cache Policies定義
  invalidation/               # 非同期パージワーカー
  edgefunc/                   # Lambda@Edge連携 (Phase 4-D)
nginx/                        # Data Plane (nginxイメージ用)
  Dockerfile
  njs/                        # nginx JavaScript
docs/                         # ユーザ向けドキュメント
examples/                     # 利用例
.claude/                      # フェーズ管理 (skill が読み書きする)
  plan.md                     # フェーズレジストリ
  tasks.md                    # 進行中フェーズの作業タスク
  design/                     # フェーズごとの設計ドキュメント
tests/                        # 統合テスト・E2Eテスト
```

## 進捗の記録

各フェーズ完了時に以下を更新すること。

1. `.claude/plan.md` のステータス表（該当フェーズを「完了」へ）
2. `.claude/design/<slug>-<date>.md` の最後に「Phase完了時メモ」セクションを追加
   - 想定外だった点
   - 次フェーズへの引き継ぎ事項
   - DESIGN.md更新が必要な点
3. `.claude/tasks.md` の進行中セクションを片付け、必要なら次フェーズの予定だけ残す

これらは将来の自分（と他のClaude Codeセッション）への手紙。
