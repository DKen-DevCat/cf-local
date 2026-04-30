// Package nginx renders nginx.conf and policies.json for the Data Plane (Phase 3).
//
// 入力は internal/config.LoadResult (AWS SDK Go v2 cloudfront/types を
// in-memory 表現とした正規化済 config)。出力は 2 ファイル分のバイト列で、
// それぞれ named volume `/etc/nginx/cf-local/{cf-local.conf,policies.json}`
// に atomic rename で書き出される (writer.go, A.4.7)。
//
// 役割分担:
//
//   - renderer.go : LoadResult → Output (cf-local.conf + policies.json) を
//     生成する。template + helper の組合せで、外部ライブラリ依存なし
//     (Go 標準の text/template + encoding/json のみ)。
//   - writer.go   : Output を tmpfile + rename(2) で atomic に書き出す。
//     A.4.7 で追加。
//
// policies.json の出力フォーマットは現行 njs cache_key.js 互換 (flat array)
// を採用 (decision: 2026-04-30, design doc §論点 1)。AWS SDK の
// {Quantity, Items} 形式は renderer 内部で flatten + omitempty して出力する。
//
// nginx.conf 生成は Phase 0〜2 の 2-hop パターン (DESIGN.md §4.2) を policy
// 数だけ展開する。サニタイズ規則:
//
//   - policy id / origin id の `[^a-zA-Z0-9_]` を `_` に置換
//   - PathPattern 末尾 `/*` を strip して location prefix にする
//   - 同一 policy が複数 behavior から参照された場合、inner location は
//     1 つだけ (sanitized policy id ベースで dedup)
//
// Phase 3 では PathPattern は prefix wildcard (`…/*`) のみ受理。それ以外
// (`*.jpg` / middle wildcard / 完全 exact) は loader 側で reject (A.4.5)。
//
// パッケージ構成 (A.4 以降で追加):
//
//   - doc.go      : このファイル
//   - renderer.go : Render(*config.LoadResult) (*Output, error)
//   - writer.go   : WriteAtomic(outDir, name string, contents []byte) error
//   - testdata/   : golden file テスト fixture (A.4.0 で配置済)
package nginx
