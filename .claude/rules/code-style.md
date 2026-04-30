# Code Style

`docs/conventions.md` の抜粋 + レビュー観点用の補足。一次情報は conventions.md 側を優先する。

## Go

### 構造

- パッケージは `internal/` 配下に置く（外部から import 不可にする）
- パッケージ名は単数形（`api`, `store`, `nginx`、`apis` ではない）
- ファイル名は機能単位の snake_case（`distribution.go`, `cache_policy.go`）

### 命名

- 公開関数・型は PascalCase / 非公開は camelCase
- 略語は大文字統一（`URL`, `HTTP`, `XML`、`Url` / `Http` は不可）
- レシーバ名は型名の小文字 1〜2 文字（`func (l *Loader) ...`）

### エラーハンドリング

- `errors.New("...")` ではなく `fmt.Errorf("...: %w", err)` で wrap する
- エラーメッセージは小文字で始める（"failed to..." であって "Failed to..." ではない）
- panic は初期化失敗時のみ。リクエストハンドラ内では使わない

### テスト

- ファイル名は `*_test.go`
- テーブル駆動テストを基本とする（`tests := []struct{ name string; ... }{ ... }`）
- `cache_key` / `ttl` などコアロジックは 100% カバレッジ目標
- HTTP ハンドラは `httptest` を使う

### フォーマット

- `gofmt` 通過必須。`gofmt -l ./...` の出力は空であること
- import は標準 → 外部 → internal の 3 ブロック区切り

## njs / nginx

### nginx.conf / template

- インデントは 4 スペース
- セクションごとにコメント区切り（`# ---- HTTP ----` 等）
- `location` ブロックは具体的な path から順に並べる
- 動的に生成される箇所はテンプレート構文がわかるようにコメントする

### njs (.js)

- ファイルは機能単位で分割（`cache_key.js`, `ttl.js`, `cache_control.js`）
- ES6+ 可だが njs で動かない構文に注意（async/await OK、class は制限あり）
- `export default` で 1 オブジェクトを export する
- 複雑なロジックは Go 側に寄せる（DESIGN.md 参照）

## Markdown / docs

- 1 行の文字数制限なし（折り返さない）
- 見出しは `#` から `###` まで。`####` 以下は基本使わない
- コードブロックには言語指定を付ける
- リンクは `[text](URL)` 形式
