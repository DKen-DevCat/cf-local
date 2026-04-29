# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

---

## Phase phase-0: PoC — nginx前段配置とキャッシュ挙動の最小再現 — 進行中

ブランチ: `feat/phase-0-poc`（develop @ `b6e1752` 起点）

設計: [`.claude/design/phase-0-poc-2026-04-29.md`](design/phase-0-poc-2026-04-29.md)

- [ ] `docker compose up` で nginx (port 8080) が起動する
- [ ] origin (Next.js等) を別途立てた状態で、ブラウザから `http://localhost:8080` にアクセスしてページが表示される
- [ ] curlで同じURLに2回アクセスすると、2回目はキャッシュヒットする (`X-Cache-Status: HIT`)
- [ ] 設定変更時に `docker compose restart` で反映できる
