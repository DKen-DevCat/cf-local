# Phase 2: TTL正確化

> このドキュメントは概要のみ。詳細仕様は **Phase 1完了後にユーザーと相談しながら作成**する。

## 概要

CloudFrontと同じTTL決定ロジックをnjsで実装する。

## ゴールイメージ

- `Cache-Control` ヘッダーを正確にパースできる
- CFのTTL決定3ケース（DESIGN.md参照）が動作する
- `X-Accel-Expires` でTTLを動的に注入できる

## 着手前にユーザーと相談する点

- `Cache-Control` パースの厳密さ（s-maxage、no-cacheの扱い）
- `X-Accel-Expires` の挙動確認
- TTL=0のときキャッシュしないようにする実装手段

## 想定するサブタスクの粒度

- Cache-Controlパーサー（njs）
- TTL決定ロジック（njs）
- nginx.confでのX-Accel-Expires制御
- テーブル駆動テスト（モックoriginで各パターン検証）
- ドキュメント整備
