// Phase 2-3 (β): cache_control.parse のテーブル駆動テスト。
//
// `/_cache_control_test` (cache_control.test.js の endpoint) を叩いて
// JSON 結果を構造比較する。X-Test-CC ヘッダで Cache-Control 文字列を渡し、
// header 自体を送らない場合 (parse(undefined)) もテストする。
//
// scope は design doc "2-2 アーキテクチャ確定" 節の 5 directive のみ:
//   no-store / no-cache / private / max-age=N / s-maxage=N
// 未指定数値フィールドは null。`0` は有効値として保持する点が要 (case 1 と
// case 2(max-age=0) の判別)。

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

type parsedCC struct {
	NoStore bool `json:"noStore"`
	NoCache bool `json:"noCache"`
	Private bool `json:"private"`
	MaxAge  *int `json:"maxAge"`
	SMaxage *int `json:"sMaxage"`
}

func intPtr(n int) *int { return &n }

// parseCC hits /_cache_control_test. When sendHeader is false, X-Test-CC is
// not added to the request — exercising parse(undefined) explicitly.
func parseCC(t *testing.T, value string, sendHeader bool) parsedCC {
	t.Helper()
	headers := map[string]string{}
	if sendHeader {
		headers["X-Test-CC"] = value
	}
	resp := doBeta(t, "/_cache_control_test", reqOpts{headers: headers})
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	var out parsedCC
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return out
}

func TestCacheControl_Parse(t *testing.T) {
	requireUp(t)

	cases := []struct {
		name       string
		value      string
		sendHeader bool
		want       parsedCC
	}{
		// defaults
		{"P01 missing header → defaults", "", false, parsedCC{}},
		{"P02 empty string → defaults", "", true, parsedCC{}},

		// single directives (flags)
		{"P03 no-store", "no-store", true, parsedCC{NoStore: true}},
		{"P04 no-cache", "no-cache", true, parsedCC{NoCache: true}},
		{"P05 private", "private", true, parsedCC{Private: true}},

		// numeric directives
		{"P06 max-age=60", "max-age=60", true, parsedCC{MaxAge: intPtr(60)}},
		{"P07 s-maxage=30", "s-maxage=30", true, parsedCC{SMaxage: intPtr(30)}},
		{"P08 max-age + s-maxage coexist", "max-age=60, s-maxage=30", true, parsedCC{MaxAge: intPtr(60), SMaxage: intPtr(30)}},

		// directive-name case-insensitive (RFC 9111)
		{"P09 directive name CI", "MAX-AGE=60", true, parsedCC{MaxAge: intPtr(60)}},

		// max-age=0 must be retained as 0, not collapsed to null —
		// downstream `ttl.compute` distinguishes "no max-age" from "max-age=0".
		{"P10 max-age=0 keeps zero", "max-age=0", true, parsedCC{MaxAge: intPtr(0)}},

		// combos
		{"P11 all flags + max-age", "no-store, no-cache, private, max-age=60", true,
			parsedCC{NoStore: true, NoCache: true, Private: true, MaxAge: intPtr(60)}},

		// unknown directives must not affect output
		{"P12 unknown directives ignored", "public, must-revalidate, max-age=60", true,
			parsedCC{MaxAge: intPtr(60)}},

		// whitespace tolerance (spaces around `,` and `=`)
		{"P13 whitespace tolerance", " max-age = 60 , no-cache ", true,
			parsedCC{NoCache: true, MaxAge: intPtr(60)}},

		// invalid value → directive dropped, not coerced
		{"P14 max-age=abc → field stays null", "max-age=abc", true, parsedCC{}},

		// quoted value (RFC 9111 allows quoted-string for delta-seconds)
		{"P15 max-age=\"60\" quoted", `max-age="60"`, true, parsedCC{MaxAge: intPtr(60)}},

		// qualified no-cache (no-cache="Set-Cookie") — we treat it as a flag,
		// ignoring the field-name list (out of scope for Phase 2).
		{"P16 qualified no-cache treated as flag", `no-cache="Set-Cookie"`, true,
			parsedCC{NoCache: true}},

		// REV-13: RFC 9111 §1.2.2 — `delta-seconds = 1*DIGIT` には負値の
		// 表記は存在しない。parser 側で reject するのが筋 (現状は parser
		// が通して compute() 側の clamp に吸収させていて RFC 違反が silent
		// に通る)。
		{"P17 max-age=-5 rejected per RFC 9111", "max-age=-5", true, parsedCC{}},
		{"P18 s-maxage=-1 rejected per RFC 9111", "s-maxage=-1", true, parsedCC{}},
		{"P19 max-age=-0 retained as 0 (sign discarded)", "max-age=-0", true, parsedCC{}},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := parseCC(t, c.value, c.sendHeader)
			if !ccEqual(got, c.want) {
				t.Fatalf("got %s\nwant %s", showCC(got), showCC(c.want))
			}
		})
	}
}

func ccEqual(a, b parsedCC) bool {
	if a.NoStore != b.NoStore || a.NoCache != b.NoCache || a.Private != b.Private {
		return false
	}
	return ptrEq(a.MaxAge, b.MaxAge) && ptrEq(a.SMaxage, b.SMaxage)
}

func ptrEq(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func showCC(c parsedCC) string {
	return fmt.Sprintf("noStore=%v noCache=%v private=%v maxAge=%s sMaxage=%s",
		c.NoStore, c.NoCache, c.Private, ptrStr(c.MaxAge), ptrStr(c.SMaxage))
}

func ptrStr(p *int) string {
	if p == nil {
		return "null"
	}
	return fmt.Sprintf("%d", *p)
}
