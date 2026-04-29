# Phase 4-B: Invalidation API互換

> このドキュメントは概要のみ。詳細仕様は **Phase 4-A完了後にユーザーと相談しながら作成**する。

## 概要

`aws cloudfront create-invalidation` がそのまま通るようにする。

## ゴールイメージ

- `CreateInvalidation` / `GetInvalidation` / `ListInvalidations` API実装
- 非同期実行（goroutine）+ ステータス管理
- ワイルドカードパス対応（`/posts/*`等）

## 着手前にユーザーと相談する点

- ワイルドカードのマッチング戦略（正規表現? glob?）
- cache_keys_zone の走査方法
- Invalidationの履歴をどこまで保持するか
