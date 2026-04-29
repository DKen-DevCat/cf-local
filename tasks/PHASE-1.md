# Phase 1: Cache Key動的計算

> このドキュメントは概要のみ。詳細仕様は **Phase 0完了後にユーザーと相談しながら作成**する。

## 概要

cache policyの概念を導入し、njsで動的にcache keyを計算できるようにする。

## ゴールイメージ

- `policies.json` でcache policyを宣言できる
- 同じURLでも、whitelistされたheaders/cookies/query stringsの値が違えば別キャッシュエントリになる
- Accept-Encoding の正規化（gzip/br/identity）が動く

## 着手前にユーザーと相談する点

- Phase 0で発見したnjsの実際の制約
- cache_keyの計算ロジックを njs / Go のどちらに置くか
- テストの書き方の方針（curlベースか、Goでのテストハーネスか）

## 想定するサブタスクの粒度

- cache policy (JSON) のスキーマ設計
- njsでのcache key計算実装
- nginx.confでのpolicy_id受け渡し
- Vary対応の確認
- テストケース整備（whitelistパターンごと）
- ドキュメント整備
