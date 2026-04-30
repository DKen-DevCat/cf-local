---
name: review-diff
description: Claude Code 自身がローカル差分または PR をレビューし、--fix 指定時は承認ベースで修正+コミットまで行う
argument-hint: [--pr <番号>] [--fix]
---

# /review-diff

Claude Code 自身が cf-local のルール（`.claude/rules/`）と `CLAUDE.md` / `DESIGN.md` / `docs/conventions.md` に照らして変更をレビューする。外部 CLI には依存しない。

## 引数

$ARGUMENTS

サポートする引数:

- `--pr <番号>` : `gh pr diff <番号>` の出力をレビュー対象にする（指定しない場合はローカル差分）
- `--fix` : 採用とした指摘について、ユーザー承認を得てから修正 + `/check` + commit を行う

---

## 実行ステップ

### Step 1. 対象 diff の収集

引数を解析する（`--pr <番号>`, `--fix` の有無）。

- `--pr <番号>` が指定されていれば:
  - `gh pr view <番号> --json title,body,author,baseRefName,headRefName` で PR メタ情報を取得
  - `gh pr diff <番号>` でパッチ本文を取得
- 指定がなければ（ローカル差分モード）:
  - `git diff HEAD` でワーキングツリーとステージ済みの差分を取得
  - `git status --porcelain` で未追跡ファイル一覧を取得し、該当ファイルの内容を Read で確認
  - 差分が空の場合は `git diff develop...HEAD` にフォールバック（ブランチ全体）

変更ファイル一覧と追加/削除行数を集計する。

### Step 2. 適用ルールの特定

変更ファイルのパスから、適用するルールを決定する。

- 常に Read で読む: `CLAUDE.md`, `DESIGN.md`, `docs/conventions.md`, `.claude/rules/code-style.md`, `.claude/rules/quality.md`
- 変更ファイルに `cmd/**` または `internal/**` が含まれる場合 → Go パートに重点
- 変更ファイルに `nginx/**` または `*.js` (njs) が含まれる場合 → njs / nginx パートに重点
- 変更ファイルに `cf-local/**` JSON / `examples/**` が含まれる場合 → `docs/config-schema.md` も追加 Read

### Step 3. 3 並列で code-reviewer サブエージェントを起動

Agent ツール（subagent_type: `general-purpose`）を 3 本、**単一メッセージ内で並列**起動する。

**Agent A — スタイル / 規約遵守**

- 担当: `code-style.md` + `docs/conventions.md`
- 観点: 命名、ファイル分割、エラーラップ、import 順序、gofmt 通過、njs スタイル、Markdown ルール
- プロンプトに含める: 差分全文 + Agent A 担当ルールの抜粋 + 「confidence 0-100 で採点し、**80 以上のみ**報告。報告フォーマット: `severity / confidence / file:line / 説明 / 該当ルール / 修正案`」

**Agent B — 設計 / スコープ / テスト**

- 担当: `quality.md` + `CLAUDE.md` + `DESIGN.md`
- 観点: スコープ外機能の混入、過剰抽象化、テストファースト遵守（cache_key / TTL / config loader 等）、フェーズ単位の動作可否、DESIGN.md からの逸脱
- プロンプトに含める: 差分全文 + Agent B 担当ルールの抜粋 + 「confidence ≥ 80 のみ報告」

**Agent C — バグ・型・セキュリティ**

- 担当: 論理バグ、Go 型 / nil 扱い、レース条件（goroutine / sync）、njs 特有の落とし穴、パフォーマンス劣化、IO リソースリーク
- 観点: コードそのものの正しさ。**ローカル開発ツールなのでセキュリティ機能の欠如は指摘しない**（DESIGN.md 参照）。ただし「うっかり認証情報をログ出力」のような事故レベルは報告する
- プロンプトに含める: 差分全文 + 観点 + 「confidence ≥ 80 のみ報告」

### Step 4. 結果の統合

- 3 エージェントの出力を受け取り、重複する指摘（同じ `file:line` かつ同じ趣旨）は 1 件に統合
- Critical (confidence ≥ 90) / Important (80-89) でグループ化
- 各指摘に次を含める:
  - 説明
  - `file_path:line`
  - 該当ルール（ルール違反の場合）
  - 具体的な修正案

### Step 5. 出力

以下のフォーマットで会話に返す。**`.claude/plan.md` への追記や自動コミットは絶対に行わない**。

````markdown
### Review summary

- Target: local diff （または `PR #<番号>`）
- Files changed: N files (+LLL / -DDD)
- Rules applied: <読み込んだルールファイルのカンマ区切り>
- Issues found: M (confidence ≥ 80)

### Critical

1. <説明>
   `path/to/file.go:42`
   Rule: `.claude/rules/quality.md` (テストファースト: cache_key の新規分岐に対応するテストがない)
   Fix: <具体的な修正案>

### Important

...

### No issues found

（該当なしの場合はこれのみ）
````

### Step 6. `--fix` が指定されている場合のみ

1. **`--pr` と `--fix` の同時指定は禁止**。両方指定されていたら、「PR への自動修正はサポートしない。ローカルで PR ブランチを checkout してから実行してください」と警告して中断する
2. 各指摘に対して、Edit/Write で適用する修正案を diff 形式で提示する
3. ユーザーに「どの指摘を修正しますか？（番号指定 / `all` / `none`）」と問い、応答を待つ
4. 選択された指摘のみ Edit で適用する
5. `/check` スキルをランタイム起動し、結果を待つ（インラインで `go vet` 等を直接書かない）
6. すべて通ったら、Conventional Commit スタイルのコミットメッセージ案を提示し、ユーザーに承認を求める
7. 承認後、`git add` + `git commit` を実行する
8. 検証が失敗した場合は、追加修正を試みるか、失敗内容をユーザーに報告して中断する（**コミットは作らない**）

---

## 制約

- **自動コミット禁止**。必ずユーザー承認を挟むこと
- **`.claude/plan.md` への追記禁止**
- confidence < 80 の指摘は報告しない（ノイズ削減のため）
- `--fix` と `--pr` の同時指定は禁止
- 未追跡ファイルがある場合も対象に含める（レビュー漏れを防ぐ）
- セキュリティ機能の欠如（auth / 暗号化）は **指摘しない**。cf-local はローカル開発ツールという DESIGN.md 前提に従う
