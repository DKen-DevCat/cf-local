---
phase: chore-2
title: check.md D-2 セクションの手順誤記修正
date: 2026-05-03
branch: chore/check-md-d2-fix
base: develop @ 8a8cd0f
status: draft
---

# Phase chore-2: check.md D-2 セクションの手順誤記修正

## 目的

Phase 4-B 実機検証 (2026-05-03) で `check.md` D-2「制御 API + β endpoint smoke test」セクションの curl 手順から `X-Test-Policy: default` header が抜けており、手順通りに辿ると `400 Bad Request` (`X-Test-Policy header required`) を返す誤記を発見した。期待 body も `body は空 or "ok" 等` と書かれているが、実態は `policy=default ...` を返す。

α テストの `requireUp` ヘルパー (`tests/integration/cache_key_test.go:152`) は内部で `X-Test-Policy: default` を付けているため自動テストは健全に動いており、手動 smoke test の手順だけが齟齬を起こしていた。手順を辿った人が「test endpoint が壊れている」と誤判断するリスクを取り除く。

## スコープ

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| chore-2-1 | curl コマンド + 期待 body 修正 | `check.md` (D-2 セクション L166-172) | `X-Test-Policy: default` header 追加、body 表記を実機 (`policy=default ...`) に合わせる |
| chore-2-2 (任意) | トラブルシューティング表に再発防止行追加 | `check.md` (L457-470 付近) | `X-Test-Policy` 抜けで 400 になる症状を 1 行 |
| chore-2-3 | 他ドキュメントの同様誤記点検 | `docs/`, `README.md`, `examples/` | grep 点検。複数あれば修正対象拡張 |

out of scope:

- njs / Go / docker-compose / nginx config の変更
- β endpoint 自体の挙動変更 (`X-Test-Policy` 必須化を緩めない)
- check.md の構成見直し

## 実装方針

- chore-2-1 を最優先。これだけで「手順誤記」の主目的は達成。
- chore-2-3 を chore-2-1 着手前に走らせ、対象範囲を確定させる (発見次第 task 追加)。
- chore-2-2 は実施判断を着手時に行う。実施しない場合は task を `deleted` に倒す。
- 修正後の手順を実機で 1 回再走 (`docker compose up` 状態で curl 実行 → 200 OK)。

## テスト方針

| レイヤー | 何をテストするか |
|---|---|
| Docs | 自動テスト不可。修正後手順を実機で 1 回辿って 200 OK と body 表記の整合を主観確認 |

## 完了条件

- [ ] 修正後 curl コマンドが実機で 200 OK を返す
- [ ] 期待 body 表記が実機出力 (`policy=default ...`) と一致
- [ ] `docs/`, `README.md`, `examples/` に同様誤記が無いことを grep で確認
- [ ] PR が develop に merge される

## コミット粒度

1 機能 1 コミット原則。本ドキュメント + tasks 更新 + plan.md (chore-2 エントリ追加) を本ブランチのキックオフコミットとする。chore-2-1 / chore-2-2 / chore-2-3 はそれぞれ別コミット (chore-2-3 で他 docs に修正が入った場合のみ別コミット)。

## リスク・未決事項

- 他 docs に同様誤記が複数ある可能性。chore-2-3 着手前 grep で範囲確定。
- chore-2-2 のトラブル表追記の費用対効果は微妙 (再発防止メリット小)。実施判断を着手時に行う。
