// Package config loads cf-local user-facing JSON config files (Phase 3).
//
// 役割:
//
//   - ./cf-local/cache-policies/*.json を読み込み、AWS SDK Go v2 の
//     cloudfront/types.CachePolicyConfig に正規化する
//   - ./cf-local/distributions/*.json を読み込み、AWS SDK Go v2 の
//     cloudfront/types.DistributionConfig に正規化する
//   - flat array 形式 (Items 相当だけユーザーが書く) を AWS SDK の
//     {Quantity, Items} 形式に変換する (Q1 決定: docs/config-schema.md §設計方針)
//
// なぜ独自スキーマ型 + 変換にしたか:
//
//   - AWS SDK 型に直接 UnmarshalJSON すると、ParametersInCacheKeyAndForwardedToOrigin
//     配下の List 型 (Headers / Cookies / QueryStrings) すべてに手書き
//     Unmarshaler が必要になり、SDK のフィールド変更に追従しづらい
//   - JSON ↔ in-memory の対応関係を明示することでテストが書きやすい
//   - Phase 4-A で AWS API ハンドラ (XML → AWS SDK 型直接) を追加する際、
//     loader と API ハンドラで同じ in-memory 表現 (AWS SDK 型) を共有できる
//
// パッケージ構成 (A.3b 以降で追加):
//
//   - schema.go  : ユーザーが書く JSON に対応する独自スキーマ型
//   - convert.go : Schema → AWS SDK types.{CachePolicyConfig,DistributionConfig}
//   - loader.go  : ディレクトリ走査 + JSON デコード + 変換 + バリデーション
//
// Phase 3 A.3a 時点では doc.go のみ。
package config
