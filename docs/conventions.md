# Coding Conventions

## Go

### 構造

- パッケージは `internal/` 配下に置く（外部からimportされたくないので）
- パッケージ名は単数形（`api`, `store`, `nginx`）
- ファイル名は機能単位（`distribution.go`, `cache_policy.go`）

### 命名

- 公開関数・型は PascalCase
- 非公開は camelCase
- 略語は大文字統一（`URL`, `HTTP`, `XML`、`Url`ではない）

### エラーハンドリング

- `errors.New` ではなく `fmt.Errorf("...: %w", err)` でwrap
- エラーメッセージは小文字で始める
- panicは初期化失敗時のみ、リクエストハンドラ内では使わない

### テスト

- ファイル名は `*_test.go`
- テーブル駆動テストを基本とする
- `cache_key` `ttl` などコアロジックは100%カバレッジ目標
- HTTPハンドラは `httptest` を使う

### 依存

- 必要最小限のライブラリしか使わない
- 標準ライブラリで十分なら追加しない
- 採用許可済み: `bbolt`, `aws-sdk-go-v2/service/cloudfront`
- 新規ライブラリ追加時は判断理由をPR説明に書く

## nginx + njs

### nginx.conf

- インデントは4スペース
- セクションごとにコメントで区切り
- 動的に生成される箇所はテンプレート構文がわかるようにコメント
- locationブロックは具体的なpathから順に並べる

### njs

- ファイル分割: 機能単位で分ける（`cache_key.js`, `ttl.js`, `cache_control.js`）
- ES6+ を使うが、njsで動かない構文に注意（async/awaitは使えるがclassは制限あり）
- `export default` で1つのオブジェクトをexportする
- 複雑なロジックは Go 側に移す（DESIGN.md参照）

## ドキュメント (Markdown)

- 1行あたりの文字数制限なし（折り返さない）
- 見出しは `#` から始め、`##`、`###` と進める。`####` 以下は基本使わない
- コードブロックには言語指定
- リンクは `[文字列](URL)` 形式
- 表は単純な構造で書く

## コミットメッセージ

[Conventional Commits](https://www.conventionalcommits.org/) に従う。

type:

- `feat`: 新機能
- `fix`: バグ修正
- `docs`: ドキュメントのみ
- `refactor`: リファクタ
- `test`: テスト追加・修正
- `chore`: ビルド設定等

例:

```
feat(api): add CreateCachePolicy endpoint

Implements POST /2020-05-31/cache-policy with XML request/response.
Stores policy in BoltDB and triggers nginx config regeneration.

Refs: phase-4a
```

## PR

- 1PR = 1論理単位
- description にどのPhaseのどのサブタスクかを明記
- `.claude/plan.md` の該当フェーズの completion checkbox に対応するならその旨を書く

## ファイル末尾

- 改行で終わる（POSIX標準準拠）
- BOMなし
- LF改行（CRLFは使わない）
