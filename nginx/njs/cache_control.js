// Phase 2-3: Cache-Control directive parser.
//
// Scope は DESIGN.md §4.2 が言及する 5 directive のみ:
//   no-store / no-cache / private / max-age=N / s-maxage=N
// それ以外 (public / must-revalidate / immutable / etc.) は無視する。
// CloudFront 互換性で必要になったら拡張する (Phase 4-C 以降)。
//
// `parse()` は副作用フリーの pure function。string | null | undefined を受けて
// 下記 shape を返す:
//   { noStore: bool, noCache: bool, private: bool, maxAge: int|null, sMaxage: int|null }
// 未指定の数値フィールドは null (= "directive 不在"); `0` は有効値として保持する
// — 下流の `ttl.compute()` (task 2-4) が "max-age 不在 (case 3)" と "max-age=0
// (case 2 → MinTTL clamp)" を区別するため。

function parse(raw) {
    const out = { noStore: false, noCache: false, private: false, maxAge: null, sMaxage: null };
    if (!raw) return out;

    const tokens = String(raw).split(',');
    for (let i = 0; i < tokens.length; i++) {
        const t = tokens[i].trim();
        if (!t) continue;

        let name;
        let value;
        const eq = t.indexOf('=');
        if (eq < 0) {
            name = t.toLowerCase();
            value = null;
        } else {
            name = t.substring(0, eq).trim().toLowerCase();
            value = t.substring(eq + 1).trim();
            // RFC 9111 §5.2 では delta-seconds に quoted-string 形を許す。
            // クライアント / 中間プロキシによっては `max-age="60"` のように送ってくる。
            if (value.length >= 2 && value.charAt(0) === '"' && value.charAt(value.length - 1) === '"') {
                value = value.substring(1, value.length - 1);
            }
        }

        if (name === 'no-store') {
            out.noStore = true;
        } else if (name === 'no-cache') {
            // qualified no-cache (`no-cache="Set-Cookie"`) は値を捨てて単なる flag として扱う。
            // フィールド単位の選択保持は Phase 2 のスコープ外。
            out.noCache = true;
        } else if (name === 'private') {
            out.private = true;
        } else if (name === 'max-age') {
            const n = parseDeltaSeconds(value);
            if (n !== null) out.maxAge = n;
        } else if (name === 's-maxage') {
            const n = parseDeltaSeconds(value);
            if (n !== null) out.sMaxage = n;
        }
        // 未知の directive は黙って無視。
    }
    return out;
}

// `parseInt('60abc', 10)` は 60 を返してしまうので、整数としての厳密一致を要求する。
// RFC 9111 §1.2.2 は `delta-seconds = 1*DIGIT` で負値の表記を許容しないため、
// `-N` (`-0` を含む) は reject して null を返す。下流の `ttl.compute()` の clamp
// で吸収させる暗黙依存を排除する目的 (review concern REV-13)。
function parseDeltaSeconds(s) {
    if (s === null || s === '') return null;
    for (let i = 0; i < s.length; i++) {
        const c = s.charCodeAt(i);
        if (c < 48 || c > 57) return null;   // 0-9 以外 (sign / 文字 / 空白) を含めば無効
    }
    const n = parseInt(s, 10);
    if (isNaN(n)) return null;
    return n;
}

export default { parse };
