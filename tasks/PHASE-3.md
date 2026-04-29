# Phase 3: Invalidation API + 設定ファイル方式

> このドキュメントは概要のみ。詳細仕様は **Phase 2完了後にユーザーと相談しながら作成**する。

## 概要

設定ファイル(JSON)から複数のdistribution / cache policyを管理可能にし、HTTPでinvalidationを発火できるようにする。

**このフェーズ完了でM2達成（他プロジェクトに流用可能な状態）**。

## ゴールイメージ

- `cf-local/distribution.json` と `cf-local/cache-policies/*.json` から nginx.conf 自動生成
- 複数のdistribution / behavior（path pattern）を宣言的に管理
- `POST /_invalidate` で完全一致パスのキャッシュパージ
- CMS（microCMS等）のwebhookと連携可能

## 着手前にユーザーと相談する点

- 設定ファイルのスキーマ（CFのAPI形式そのままか、簡略化するか）
- nginx.conf生成スクリプトをGoで書くかシェル/Nodeで書くか
- ngx_cache_purge or alternative の選定

## 想定するサブタスクの粒度

- 設定ファイルスキーマ設計
- Goでnginx.conf生成
- ngx_cache_purge組込み（or 代替手段）
- Invalidation HTTPエンドポイント
- CMS連携の使い方ドキュメント
- examples/ 拡充
