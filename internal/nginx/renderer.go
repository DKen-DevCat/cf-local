package nginx

import (
	"errors"

	"github.com/DKen-DevCat/cf-local/internal/config"
)

// Output は Render() の生成物。Conf は cf-local.conf 相当のバイト列、
// Policies は policies.json 相当のバイト列。両方とも末尾改行を含む。
//
// writer.go (A.4.7) はこの 2 つを named volume `/etc/nginx/cf-local/` 配下に
// atomic rename で書き出す。書き出しファイル名はそれぞれ:
//
//   - <out-dir>/cf-local.conf
//   - <out-dir>/policies.json
type Output struct {
	Conf     []byte
	Policies []byte
}

// Render は LoadResult を nginx.conf + policies.json のバイト列に変換する。
//
// 入力契約:
//   - res != nil
//   - res.Distribution が nil または Enabled=false の場合は「無効化された
//     distribution」として扱い、cf-local.conf にコメントのみ・policies.json
//     には空マップ (`{"policies":{}}`) を出力する
//   - PathPattern の受理規則は loader (config.Load) 側で検証済み前提
//     (renderer は再検証しない)
//
// 戻り値の error は I/O ではなく構造的な異常 (CachePolicyId が
// res.CachePolicies に存在しない等) を表す。loader が cross-ref 検証を
// 通している場合、ここで error が返ることはないが defense-in-depth として残す。
//
// A.4.1 時点では unimplemented (skeleton)。A.4.2 で policies.json、A.4.3
// で cf-local.conf の生成を順次実装する。
func Render(res *config.LoadResult) (*Output, error) {
	if res == nil {
		return nil, errors.New("nginx.Render: LoadResult is nil")
	}
	return nil, errors.New("nginx.Render: unimplemented (A.4.1 skeleton)")
}
