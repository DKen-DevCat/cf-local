---
phase: chore-1
title: Claude 開発フロー強化 (review infra)
date: 2026-04-30
branch: chore/claude-flow
base: develop @ 8d69fb2
status: in-progress
---

# Phase chore-1: Claude 開発フロー強化 (review infra)

## 目的

Phase 3 完了直後に整備した cf-local の `.claude/` 配下（`/check` + `/review-diff` + `.claude/rules/`）の品質を一段引き上げる。具体的には `/review-diff` の subagent_type を `general-purpose` から専用 `code-reviewer` agent に切替えて、レビュー指摘の粒度・正確性・領域カバレッジを Phase 4a 着手前に上げておく。

**コアバリューとの関係**: cf-local は OSS 公開 (Phase 5) を見据えた段階的 PR ドリブン開発。Phase 4a (Terraform 対応) ではハンドラ層 / XML マーシャリング / BoltDB ストア層が一気に増えるため、各 PR の自動レビューが効くと「粒度の細かい指摘で実装を直す」サイクルが回る。今ここで投資する。

## スコープ

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| chore-1-1 | code-reviewer agent 新規作成 | `.claude/agents/code-reviewer.md` | Go + njs/nginx + Markdown を担当。`.claude/rules/` を参照する形で書く |
| chore-1-2 | review-diff の subagent 切替 | `.claude/skills/review-diff/SKILL.md` | Step 3 の `subagent_type: general-purpose` を `code-reviewer` に。プロンプト調整も併せて |
| chore-1-3 (任意) | rules 領域別分割の判断 | `.claude/rules/code-style.md` (分割なら新規 `go.md` / `njs-nginx.md` / `markdown.md`) | 1-1 / 1-2 完了後に試走して必要性を再評価 |
| chore-1-4 (任意) | ドッグフード | (実 PR 上で `/phase-review --pr <番号>`) | 観測結果を `~/.claude/docs/phase-flow-comparison.md` §4 にフィードバック |

## 実装方針

### 1. 段階リリース（必須 → 任意）

- **chore-1-1 → chore-1-2** が必須セット。これだけで `/review-diff` が動く構成になる
- **chore-1-3 / chore-1-4** は試走後の判断。先に commit せず、必要性が見えたら追加コミットで対応

### 2. code-reviewer agent の構造

nestify の `code-reviewer.md` は TypeScript / Bun 前提なので直輸入不可。cf-local 用に Go + njs/nginx 観点で書き直す。最小構成:

```yaml
---
name: code-reviewer
description: cf-local の差分を rules / DESIGN.md / conventions.md に照らしてレビューする
tools: Glob, Grep, Read
---
```

責務:
- 入力: 差分 + 適用ルール抜粋 + 観点指示（差分は呼び出し側 SKILL.md Step 3 がプロンプトに埋め込む。agent 側で git を叩かない）
- 出力: `severity / confidence / file:line / 説明 / 該当ルール / 修正案` 形式の指摘リスト（confidence ≥ 80 のみ）
- write 系・Bash も持たせない（read-only レビュー / プロンプト経由で受領した差分のみを参照）

### 3. review-diff 側の調整

Step 3 で 3 並列起動する subagent を `general-purpose` → `code-reviewer` に切替。それに合わせてプロンプトも `code-reviewer` の責務契約に揃える（上記の I/O 形式を要求）。

### 4. rules 分割判断 (chore-1-3) の指針

判断は試走後に：

- 試走で「Markdown 規則違反を指摘されすぎる」「Go 規則と njs 規則が混じって精度が落ちる」等の症状が出たら **分割採用**
- 出ない場合は **据え置き** (今 2 ファイルで足りている可能性が高い)

分割するなら:
- `.claude/rules/go.md` ← code-style.md の Go セクション
- `.claude/rules/njs-nginx.md` ← code-style.md の njs / nginx セクション
- `.claude/rules/markdown.md` ← code-style.md の Markdown / docs セクション
- `.claude/rules/quality.md` はそのまま（クロスカット）

## テスト方針

| レイヤー | 対象 |
|---|---|
| Tooling | 自動テストは無し。**実 PR 上で動かして観測** |

ドッグフード手順:
1. chore-1-1 + chore-1-2 をコミット → push → PR 化
2. PR 上で `/phase-review --pr <この PR の番号>` を実行
3. 出力された指摘を主観評価（before: general-purpose 時の指摘との比較）
4. 改善が見えたら chore-1 完了。粗ければ agent prompt を追加調整

## 完了条件

- `.claude/agents/code-reviewer.md` が配置されている
- `.claude/skills/review-diff/SKILL.md` が `subagent_type: code-reviewer` を呼ぶ構成になっている
- 実 PR で `/phase-review --pr <番号>` を 1 回走らせ、指摘の質が `general-purpose` 比で改善している
- (chore-1-3 採用なら) rules 分割が完了し、review-diff の Read 対象が更新されている
- (chore-1-4 採用なら) フィードバックが `~/.claude/docs/phase-flow-comparison.md` に追記されている

## コミット粒度

1 機能 1 コミット原則:

- C1: `feat(chore-1): add code-reviewer agent` (chore-1-1)
- C2: `feat(chore-1): switch /review-diff subagent to code-reviewer` (chore-1-2)
- C3 (任意): `refactor(chore-1): split .claude/rules/ by domain` (chore-1-3)
- C4 (任意): `docs(chore-1): record /phase-review dogfood result` (chore-1-4)

本ドキュメント + tasks 更新を本ブランチのキックオフコミットとする。

## リスク・未決事項

- **agent prompt の初版精度**: nestify からの輸入不可なので Go 用に新規。試走で粗ければ繰り返し調整
- **rules 分割の要否 (chore-1-3)**: 未決。試走後に判断
- **ドッグフード対象 PR (chore-1-4)**: 本 chore の PR 自体でやるか、Phase 4a の最初の PR でやるかは未決。前者の方が早くフィードバックが回るが、本 chore は変更がほぼ `.claude/` のみなのでレビュー材料が少なめ
