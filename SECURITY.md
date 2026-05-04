# Security Policy

## サポート対象バージョン

| バージョン | サポート状況 |
|---|---|
| `0.1.x` | ✅ アクティブ |
| `< 0.1.0` | ❌ 未サポート (pre-release) |

最新の安定版を利用してください。

## 設計上の前提

cf-local は **ローカル開発専用** のツールです。CloudFront のキャッシュ挙動を再現することに特化しており、以下のセキュリティ機能は **意図的に実装していません** ([`DESIGN.md`](DESIGN.md) §2 参照):

- 認証 / 認可
- HTTPS / TLS
- WAF / Shield / Field-level Encryption
- 署名付き URL / Cookie
- 地理ブロック

このため cf-local を **本番環境や公開ネットワークに晒すことは想定外** です。`localhost` または信頼された開発ネットワーク (Tailscale 等) でのみ利用してください。

## 脆弱性の報告

cf-local 自体の脆弱性 (たとえば設定ファイル parser の path traversal、njs ロジックの XSS 余地、Lambda RIE 連携での SSRF など) を発見された場合は、**public な Issue を作らず**、以下の手順で報告してください:

1. リポジトリの **Security** タブを開く
2. **Report a vulnerability** をクリック
3. private vulnerability advisory フォームに以下を記載:
   - 影響範囲 (どのコンポーネント / どのバージョン)
   - 再現手順 (最小限の reproducer)
   - 想定される影響 (情報漏洩 / 任意コード実行 / DoS 等)
   - (任意) 修正案

URL: <https://github.com/DKen-DevCat/cf-local/security/advisories/new>

## 対応方針

- 報告内容を確認次第、可能な範囲で迅速に対応します
- 個人プロジェクトのため **対応 SLA は提示できません**
- 修正版がリリースされるまで、報告者には public な開示を控えていただくようお願いします
- 修正リリース時に CHANGELOG.md の `### Security` セクションで開示します (報告者のクレジットは希望に応じて)

## スコープ外の報告

以下は脆弱性ではなく **設計上の挙動** として扱います:

- HTTPS 非対応 (ローカル開発用途のため)
- 認証なしの管理 API (`:4566`、`:4569`) — 信頼ネットワーク前提
- BoltDB の暗号化なし — ローカルファイルのため
- nginx / njs / Lambda RIE 等の **upstream 依存ライブラリ** の脆弱性 — それぞれの upstream に直接報告してください

ただし、これらの上に乗っている cf-local の実装に起因する具体的な脆弱性 (例: 設定ファイル parser の入力検証不足) は報告対象です。
