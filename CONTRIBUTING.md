# Contributing to cf-local

このプロジェクトへの貢献を歓迎する。

## 始める前に

1. `README.md` `DESIGN.md` `.claude/plan.md` を読む
2. 何を変えたいかをissueで提案する（大きな変更の場合）
3. 小さな修正（typo、ドキュメント等）はissueなしでPRしてOK

## 開発フロー

```bash
git clone https://github.com/DKen-DevCat/cf-local.git
cd cf-local

git checkout -b feat/your-feature
git add .
git commit -m "feat(scope): your change"
git push origin feat/your-feature
```

## コミット規約

[Conventional Commits](https://www.conventionalcommits.org/) に従うこと。詳細は `docs/conventions.md` を参照。

## コーディング規約

`docs/conventions.md` を参照。

## テスト

PRには対応するテストを含めること。特に以下は必須:

- 新しいAPIエンドポイント → ハンドラのテスト
- cache_key / TTL ロジックの変更 → テーブル駆動テスト追加
- バグ修正 → 再現テスト + 修正

## 行動規範

- 建設的な議論を歓迎
- 個人攻撃や差別的な発言は禁止
- 多様な視点を尊重しよう
