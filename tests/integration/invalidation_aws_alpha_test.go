// Phase 4-B 4b-10 (α): AWS REST/XML CreateInvalidation のフルパス検証。
//
// 前提: docker-compose up で nginx (:8080) + cf-local control plane (:4566)
// が稼働。testserver が host:3000 で `cc` query を Cache-Control に echoback。
//
// AWS 互換 endpoint なので path は `/2020-05-31/distribution/{Id}/invalidation`。
// distId は handler が永続化するだけで存在検証はしない (4b-5) ため、未登録の
// 合成 ID `EALPHATEST` を使えば BoltDB に他の Distribution を作る必要が無い。
// worker (4b-6) は cache directory walk + URI 一致 + os.Remove で動くので、
// distId に紐づかず invalidation 自体は機能する。

package integration

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	awsInvalidationBase = "http://localhost:4566/2020-05-31/distribution"
	awsAlphaDistID      = "EALPHATEST"
)

// awsInvAlphaPath uses PID + name to keep cases independently warm-cacheable
// across parallel test runs (matches the convention of the legacy α suite).
func awsInvAlphaPath(name string) string {
	return fmt.Sprintf("/_inv_aws_alpha/%d/%s", os.Getpid(), name)
}

// awsInvalidation is a minimal projection of the AWS <Invalidation> response
// body. We only assert on Id / Status here so the wrapper omits Batch/Paths.
type awsInvalidation struct {
	XMLName xml.Name `xml:"Invalidation"`
	ID      string   `xml:"Id"`
	Status  string   `xml:"Status"`
}

// awsInvalidationList mirrors the shape needed for the ListInvalidations
// smoke test (Items / IsTruncated / Quantity).
type awsInvalidationList struct {
	XMLName     xml.Name `xml:"InvalidationList"`
	IsTruncated bool     `xml:"IsTruncated"`
	Quantity    int      `xml:"Quantity"`
	Items       struct {
		Summary []struct {
			ID     string `xml:"Id"`
			Status string `xml:"Status"`
		} `xml:"InvalidationSummary"`
	} `xml:"Items"`
}

// createInvalidationXML POSTs CreateInvalidation against awsAlphaDistID with
// the supplied paths and returns the parsed Invalidation. Fatals on any
// network / status / decode error so the calling test fails loudly.
func createInvalidationXML(t *testing.T, callerRef string, paths ...string) awsInvalidation {
	t.Helper()
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<InvalidationBatch xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">`)
	body.WriteString(`<CallerReference>` + callerRef + `</CallerReference>`)
	body.WriteString(`<Paths><Quantity>` + fmt.Sprint(len(paths)) + `</Quantity><Items>`)
	for _, p := range paths {
		body.WriteString(`<Path>` + p + `</Path>`)
	}
	body.WriteString(`</Items></Paths></InvalidationBatch>`)

	req, err := http.NewRequest(http.MethodPost,
		awsInvalidationBase+"/"+awsAlphaDistID+"/invalidation",
		bytes.NewReader([]byte(body.String())))
	if err != nil {
		t.Fatalf("build CreateInvalidation request: %v", err)
	}
	req.Header.Set("Content-Type", "application/xml")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("CreateInvalidation request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("CreateInvalidation status: got %d want 201\nbody: %s", resp.StatusCode, raw)
	}
	var inv awsInvalidation
	if err := xml.NewDecoder(resp.Body).Decode(&inv); err != nil {
		t.Fatalf("decode CreateInvalidation response: %v", err)
	}
	if inv.ID == "" || inv.ID[0] != 'I' {
		t.Fatalf("CreateInvalidation Id: got %q want I-prefixed", inv.ID)
	}
	return inv
}

// waitForCompleted polls GetInvalidation until Status=Completed or the
// deadline expires. The async worker is fast (single os.Remove per match),
// so 5 seconds is generous for local dev.
func waitForCompleted(t *testing.T, invID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := httpClient.Get(awsInvalidationBase + "/" + awsAlphaDistID + "/invalidation/" + invID)
		if err != nil {
			t.Fatalf("GetInvalidation request: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("GetInvalidation status: got %d want 200\nbody: %s", resp.StatusCode, raw)
		}
		var inv awsInvalidation
		if err := xml.NewDecoder(resp.Body).Decode(&inv); err != nil {
			resp.Body.Close()
			t.Fatalf("decode GetInvalidation response: %v", err)
		}
		resp.Body.Close()
		if inv.Status == "Completed" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("invalidation %s did not reach Completed within 5s", invID)
}

// TestInvalidate_AWS_Alpha_HitToMiss covers the AWS REST/XML path of
// CreateInvalidation → worker → cache MISS — the phase-4b replacement for the
// phase-3 simple-JSON α suite (deleted in 4b-9).
func TestInvalidate_AWS_Alpha_HitToMiss(t *testing.T) {
	requireUp(t)
	if os.Getenv("CF_LOCAL_MOCK_ORIGIN") == "skip" {
		t.Skip("CF_LOCAL_MOCK_ORIGIN=skip set; this α suite drives the mock origin via cc= query")
	}

	// max-age=300 keeps HIT alive for the duration of the test wall clock.
	cc := "max-age%3D300" // url-encoded "max-age=300"

	t.Run("single literal path: warm → HIT → invalidate → MISS", func(t *testing.T) {
		path := awsInvAlphaPath("AAINV01")
		full := path + "?cc=" + cc

		if r := head(t, full, nil); r.cacheStatus != "MISS" {
			t.Fatalf("warm round 1: expected MISS got %s", r.cacheStatus)
		}
		if r := head(t, full, nil); r.cacheStatus != "HIT" {
			t.Fatalf("warm round 2: expected HIT got %s", r.cacheStatus)
		}

		inv := createInvalidationXML(t, "alpha-aainv01", path)
		if inv.Status != "InProgress" {
			t.Errorf("CreateInvalidation initial Status: got %q want InProgress", inv.Status)
		}
		waitForCompleted(t, inv.ID)

		if r := head(t, full, nil); r.cacheStatus != "MISS" {
			t.Fatalf("after invalidate: expected MISS got %s", r.cacheStatus)
		}
	})

	t.Run("wildcard `/...*` purges all matching slots", func(t *testing.T) {
		base := awsInvAlphaPath("AAINV02")
		a := base + "/a"
		b := base + "/b"
		fullA := a + "?cc=" + cc
		fullB := b + "?cc=" + cc

		head(t, fullA, nil) // warm a
		head(t, fullB, nil) // warm b
		if r := head(t, fullA, nil); r.cacheStatus != "HIT" {
			t.Fatalf("a warm round 2: %s", r.cacheStatus)
		}
		if r := head(t, fullB, nil); r.cacheStatus != "HIT" {
			t.Fatalf("b warm round 2: %s", r.cacheStatus)
		}

		inv := createInvalidationXML(t, "alpha-aainv02", base+"/*")
		waitForCompleted(t, inv.ID)

		if r := head(t, fullA, nil); r.cacheStatus != "MISS" {
			t.Fatalf("a after wildcard invalidate: %s", r.cacheStatus)
		}
		if r := head(t, fullB, nil); r.cacheStatus != "MISS" {
			t.Fatalf("b after wildcard invalidate: %s", r.cacheStatus)
		}
	})

	t.Run("multi paths in one CreateInvalidation", func(t *testing.T) {
		a := awsInvAlphaPath("AAINV03-a")
		b := awsInvAlphaPath("AAINV03-b")
		fullA := a + "?cc=" + cc
		fullB := b + "?cc=" + cc

		head(t, fullA, nil)
		head(t, fullB, nil)
		if r := head(t, fullA, nil); r.cacheStatus != "HIT" {
			t.Fatalf("a warm round 2: %s", r.cacheStatus)
		}
		if r := head(t, fullB, nil); r.cacheStatus != "HIT" {
			t.Fatalf("b warm round 2: %s", r.cacheStatus)
		}

		inv := createInvalidationXML(t, "alpha-aainv03", a, b)
		waitForCompleted(t, inv.ID)

		if r := head(t, fullA, nil); r.cacheStatus != "MISS" {
			t.Fatalf("a after invalidate: %s", r.cacheStatus)
		}
		if r := head(t, fullB, nil); r.cacheStatus != "MISS" {
			t.Fatalf("b after invalidate: %s", r.cacheStatus)
		}
	})

	t.Run("invalidate non-cached path → 201 + Completed (worker walks empty)", func(t *testing.T) {
		path := awsInvAlphaPath("AAINV04-never-cached")
		inv := createInvalidationXML(t, "alpha-aainv04", path)
		waitForCompleted(t, inv.ID)
		// No HEAD assertion: a never-warmed path stays MISS regardless,
		// but the worker reaching Completed on an empty match set is the
		// success condition.
	})

	t.Run("invalid path → 400 InvalidArgument", func(t *testing.T) {
		body := `<?xml version="1.0" encoding="UTF-8"?>
<InvalidationBatch xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <CallerReference>alpha-aainv05</CallerReference>
  <Paths><Quantity>1</Quantity><Items><Path>/foo~bar</Path></Items></Paths>
</InvalidationBatch>`
		req, err := http.NewRequest(http.MethodPost,
			awsInvalidationBase+"/"+awsAlphaDistID+"/invalidation",
			bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		req.Header.Set("Content-Type", "application/xml")
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", resp.StatusCode)
		}
	})

	t.Run("ListInvalidations returns the records we just created", func(t *testing.T) {
		// Paginate through everything under awsAlphaDistID and assert that
		// at least the IDs from the previous sub-tests are present.
		resp, err := httpClient.Get(awsInvalidationBase + "/" + awsAlphaDistID + "/invalidation?MaxItems=100")
		if err != nil {
			t.Fatalf("ListInvalidations: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("List status: got %d\nbody: %s", resp.StatusCode, raw)
		}
		var list awsInvalidationList
		if err := xml.NewDecoder(resp.Body).Decode(&list); err != nil {
			t.Fatalf("decode List: %v", err)
		}
		if list.Quantity < 4 {
			t.Errorf("List Quantity: got %d want >=4 (the sub-tests above each Create one)", list.Quantity)
		}
	})
}
