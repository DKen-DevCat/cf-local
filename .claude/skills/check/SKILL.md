---
name: check
description: go vet / gofmt -l / go test ./internal/... を並列実行して結果を集約表示
---

# /check

cf-local の Go コードに対する高速サニティチェック。以下 3 タスクを **単一メッセージ内で並列 Bash 実行** する。

## 実行タスク

| # | Task | Command |
|---|------|---------|
| 1 | go vet | `go vet ./...` |
| 2 | gofmt | `gofmt -l .` |
| 3 | go test (internal) | `go test ./internal/...` |

## 除外

- 統合テスト (`./tests/integration/...`) は重い & docker 依存のため含めない。必要時は個別に実行する
- Data Plane (njs / nginx) の検証はここでは行わない（spike か α regression で確認）
- `go build` は test に含まれるためここでは別建てしない

## 出力フォーマット

```markdown
### /check result

| Task             | Status | Time |
|------------------|--------|------|
| go vet           | PASS   |  3s  |
| gofmt -l         | PASS   |  1s  |
| go test internal | PASS   |  5s  |

### Failures

[gofmt -l]
internal/foo/bar.go

### Summary

3 passed, 0 failed.
```

- 全通過の場合は "Failures" セクションを省略し、"Summary" で全通過を明示する
- `gofmt -l` は出力があれば（=未整形ファイルがあれば）FAIL 扱い
- 失敗がある場合は、該当タスクの stderr/stdout 末尾を最大 30 行まで引用する（長ければ切り詰め）

## 制約

- 修正やコミットは一切行わない（検証専用）
- 失敗があった場合は内容を要約して報告し、修正はユーザー指示を待つ
- 失敗の原因をエージェントが即座に直そうとしないこと（スコープ外）
