#!/usr/bin/env bash
# tests/stress/run.sh — vegeta stress harness (Phase 4-C 4c-8 / BL-NX2)
#
# 目的: cf-local の前段 nginx に対して 1000 RPS / 1 分の負荷を掛け、
#       4c-7 unix socket 化が「accept queue 飽和 / connection refused」
#       状態を改善したかを実機で観測するための harness。
#
# 必要なもの:
#   - docker compose up が走った状態 (cf-local + nginx in localhost:8080)
#     例: `docker compose up -d --build`
#   - vegeta CLI (https://github.com/tsenart/vegeta) もしくは Docker
#       brew install vegeta             # macOS
#       go install github.com/tsenart/vegeta/v12@latest
#       (なければ自動で peterevans/vegeta コンテナを使う)
#
# 使い方:
#   ./tests/stress/run.sh                          # default: 1000rps / 60s
#   ./tests/stress/run.sh 500 30                   # 500 RPS / 30 秒
#   TARGET_URL=http://localhost:8080/ ./tests/stress/run.sh
#
# 出力:
#   tests/stress/results/<UTC timestamp>/
#       attack.bin    — vegeta バイナリ生データ
#       summary.txt   — 集計テキスト (latency / errors / status codes)
#       histogram.txt — レイテンシヒストグラム
#
# 記録の仕方:
#   実走後に summary.txt の主要値 (success ratio, 99p latency, errors)
#   を tests/stress/baseline.md に追記する。before/after の比較は
#   baseline.md の指示通り。

set -euo pipefail

RPS="${1:-1000}"
DURATION_SECONDS="${2:-60}"
TARGET_URL="${TARGET_URL:-http://localhost:8080/}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RESULTS_BASE="${ROOT_DIR}/tests/stress/results"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
RESULTS_DIR="${RESULTS_BASE}/${TS}"
mkdir -p "${RESULTS_DIR}"

echo "==> cf-local stress harness"
echo "    target  : ${TARGET_URL}"
echo "    rate    : ${RPS} req/s"
echo "    duration: ${DURATION_SECONDS}s"
echo "    output  : ${RESULTS_DIR}"

# 0. ヘルスチェック: ターゲットが応答するか確認 (主に「docker compose up を
#    し忘れて空打ちする」事故を防ぐため)。
if ! curl -fsS --max-time 3 -o /dev/null "${TARGET_URL}"; then
    echo "ERROR: ${TARGET_URL} に到達できません。`docker compose up` してから再実行してください" >&2
    exit 1
fi

# 1. cache を warm up (先に 5 回 GET → 2 回目以降が HIT になる確率が高くなり、
#    1000 RPS の負荷下で proxy_pass 経路が想定通り origin を直撃しないことを
#    確認できる)。
echo "==> warm-up (5 sequential GETs)"
for i in 1 2 3 4 5; do
    curl -fsS -o /dev/null -w "    [$i] status=%{http_code} time=%{time_total}s\n" "${TARGET_URL}"
done

# 2. vegeta 本体。ローカル install / Docker fallback を自動選択。
TARGETS_FILE="${RESULTS_DIR}/targets.txt"
echo "GET ${TARGET_URL}" > "${TARGETS_FILE}"

run_vegeta_local() {
    vegeta attack \
        -targets="${TARGETS_FILE}" \
        -rate="${RPS}" \
        -duration="${DURATION_SECONDS}s" \
        -timeout=10s \
        > "${RESULTS_DIR}/attack.bin"
    vegeta report -type=text < "${RESULTS_DIR}/attack.bin" > "${RESULTS_DIR}/summary.txt"
    vegeta report -type='hist[0,1ms,5ms,10ms,25ms,50ms,100ms,250ms,500ms,1s,5s]' \
        < "${RESULTS_DIR}/attack.bin" > "${RESULTS_DIR}/histogram.txt"
}

run_vegeta_docker() {
    local img="peterevans/vegeta:latest"
    docker run --rm \
        --network host \
        -v "${RESULTS_DIR}:/out" \
        "${img}" \
        sh -c "echo 'GET ${TARGET_URL}' > /tmp/t && \
               attack -targets=/tmp/t -rate=${RPS} -duration=${DURATION_SECONDS}s -timeout=10s > /out/attack.bin && \
               report -type=text < /out/attack.bin > /out/summary.txt && \
               report -type='hist[0,1ms,5ms,10ms,25ms,50ms,100ms,250ms,500ms,1s,5s]' < /out/attack.bin > /out/histogram.txt"
}

if command -v vegeta >/dev/null 2>&1; then
    echo "==> running vegeta (local CLI)"
    run_vegeta_local
elif command -v docker >/dev/null 2>&1; then
    echo "==> vegeta CLI not found, falling back to docker (peterevans/vegeta)"
    run_vegeta_docker
else
    echo "ERROR: vegeta CLI も docker も見つかりません" >&2
    exit 1
fi

echo
echo "==> summary"
cat "${RESULTS_DIR}/summary.txt"
echo
echo "==> latency histogram"
cat "${RESULTS_DIR}/histogram.txt"
echo
echo "==> raw attack data: ${RESULTS_DIR}/attack.bin"
echo "    詳細: vegeta report -type=json < attack.bin | jq ."
