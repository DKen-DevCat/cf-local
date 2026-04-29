# Phase 4-D: Lambda@Edge連携

> このドキュメントは概要のみ。詳細仕様は **Phase 4-C完了後にユーザーと相談しながら作成**する。

## 概要

Lambda@Edge / CloudFront Functions のローカル実行を実現する。AWS公式の Lambda Runtime Interface Emulator (RIE) と連携する。

## ゴールイメージ

- edge-proxy (Go) サイドカー実装
- viewer-request / origin-request / origin-response / viewer-response の4フック対応
- CloudFrontイベント形式の構築（ヘッダー正規化含む）
- `docker-compose.lambda.yml` テンプレート提供
- 単体使用 / Lambda連携使用 の両方を切り替え可能

## 着手前にユーザーと相談する点

- イベント形式のテストデータをどこまで揃えるか
- 4フック全部か、優先順位（viewer-requestから?）
- Lambda関数とdistributionの紐付け方法（環境変数 vs 設定ファイル）

## 重要な設計判断（DESIGN.md参照）

- イベント形式構築は Go 側で行う（njsではない）
- Lambda関数の管理はdocker-composeで行う（Lambda APIは実装しない）
- njsからedge-proxyへの転送は `ngx.fetch` で行う
