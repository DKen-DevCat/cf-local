# Phase 4-A: Terraform対応・最小

> このドキュメントは概要のみ。詳細仕様は **Phase 3完了後にユーザーと相談しながら作成**する。

## 概要

`terraform apply` を本番Terraformコードのまま（endpointsだけ変えて）ローカルcf-localに対して実行できるようにする。

## ゴールイメージ

- Go HTTP server (Port 4566) でAWS API互換エンドポイント提供
- `aws_cloudfront_distribution` / `aws_cloudfront_cache_policy` / `aws_cloudfront_origin_request_policy` のCRUD
- Managed Cache Policiesがbuilt-in
- BoltDB で設定永続化
- 設定変更で nginx.conf 自動再生成 + reload

## 着手前にユーザーと相談する点

- AWS SDK for Go の型をそのまま使うか、ラッパーを作るか
- XMLマーシャリングでハマる点の想定
- どのTerraform versionで動作確認するか

## 想定するサブタスクの粒度

- Go HTTP Server基盤
- AWS APIエンドポイントのrouting
- BoltDBストア実装
- nginx auto-reload (debounce付き)
- Managed Cache Policies組込み
- Terraform連携テスト

## 検証の重要ポイント

実際にTerraform plan/applyを流して、想定外のAPI呼び出しがログに出ないか確認する。出たら対応API追加。
