// Phase 2-1: X-Accel-Expires injection が proxy_cache 判定に反映されるかの spike。
// 結果サマリ (design doc の "2-1 spike 結果" 節 参照):
//   - 1-hop js_header_filter からの injection は cache 判定後に走る → 効かない
//   - 2-hop (outer cache + inner injection) パターンなら inner のフィルタが
//     outer 視点で「upstream のヘッダ」になるため injection が効く
//
// 再実行手順 (1-1 spike と同じパターン):
//   1. nginx/nginx.conf の http {} 内に下記を追加:
//        js_import ttl_probe from spike/ttl_probe.js;
//        upstream self { server 127.0.0.1:8080; }
//   2. server {} 内に下記 2 ペアを追加:
//        # 1-hop (control: 効かないことを確認)
//        location = /_ttl_probe {
//            set $cf_policy_id "default";
//            js_header_filter ttl_probe.setTTL;
//            proxy_cache cf_cache;
//            proxy_cache_key $cf_cache_key;
//            proxy_cache_valid 200 1m;
//            proxy_ignore_headers Cache-Control Set-Cookie Vary;
//            add_header X-Cache-Status $upstream_cache_status always;
//            add_header X-Cache-Key    $cf_cache_key          always;
//            proxy_pass http://origin/favicon.ico;
//        }
//        # 2-hop (Phase 2 採用候補)
//        location = /_2hop_outer {
//            set $cf_policy_id "default";
//            proxy_cache cf_cache;
//            proxy_cache_key $cf_cache_key;
//            proxy_cache_valid 200 1m;
//            proxy_ignore_headers Cache-Control Set-Cookie Vary;
//            add_header X-Cache-Status $upstream_cache_status always;
//            add_header X-Cache-Key    $cf_cache_key          always;
//            proxy_pass http://self/_2hop_inner;
//        }
//        location = /_2hop_inner {
//            js_header_filter ttl_probe.setTTL;
//            proxy_pass http://origin/favicon.ico;
//        }
//   3. docker compose restart
//   4. curl で X-Test-TTL ヘッダを変えて挙動観察 (例: 5 / 0 / 不在)

// js_header_filter エントリ。X-Test-TTL リクエストヘッダで指示された値を
// X-Accel-Expires レスポンスヘッダに乗せる。値が無ければ何もせず、
// 上流の Cache-Control / proxy_cache_valid のフォールバックに任せる。
function setTTL(r) {
    const t = r.headersIn['X-Test-TTL'];
    if (t === undefined || t === '') return;
    r.headersOut['X-Accel-Expires'] = String(t);
    r.log('ttl_probe: set X-Accel-Expires=' + t);
}

export default { setTTL };
