package awsxml

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// TestInvalidationBatch_Unmarshal verifies that the AWS public API Reference
// Syntax sample for a CreateInvalidation request body parses cleanly into
// our tagged wrappers.
func TestInvalidationBatch_Unmarshal(t *testing.T) {
	const sample = `<?xml version="1.0" encoding="UTF-8"?>
<InvalidationBatch xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <CallerReference>my-cache-bust-2026-05-02</CallerReference>
  <Paths>
    <Quantity>3</Quantity>
    <Items>
      <Path>/index.html</Path>
      <Path>/posts/*</Path>
      <Path>/api/v1/data</Path>
    </Items>
  </Paths>
</InvalidationBatch>`

	var got InvalidationBatch
	if err := xml.Unmarshal([]byte(sample), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.XMLName.Space != XMLNSCloudFront {
		t.Errorf("XMLName.Space: got %q want %q", got.XMLName.Space, XMLNSCloudFront)
	}
	if got.XMLName.Local != "InvalidationBatch" {
		t.Errorf("XMLName.Local: got %q", got.XMLName.Local)
	}
	if got.CallerReference != "my-cache-bust-2026-05-02" {
		t.Errorf("CallerReference: got %q", got.CallerReference)
	}
	if got.Paths == nil {
		t.Fatal("Paths: nil")
	}
	if got.Paths.Quantity != 3 {
		t.Errorf("Paths.Quantity: got %d want 3", got.Paths.Quantity)
	}
	want := []string{"/index.html", "/posts/*", "/api/v1/data"}
	if !equalStrings(got.Paths.Items.Path, want) {
		t.Errorf("Paths.Items.Path: got %v want %v", got.Paths.Items.Path, want)
	}
}

// TestInvalidationBatch_Marshal confirms the wrapper produces a wire format
// readable as the AWS shape: namespace on the root, Quantity + Items/Path
// children in the canonical order.
func TestInvalidationBatch_Marshal(t *testing.T) {
	in := &InvalidationBatch{
		CallerReference: "ref-123",
		Paths: &Paths{
			Items:    PathItems{Path: []string{"/a", "/b/*"}},
			Quantity: 2,
		},
	}
	out, err := xml.MarshalIndent(in, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(out)
	for _, want := range []string{
		`xmlns="` + XMLNSCloudFront + `"`,
		`<CallerReference>ref-123</CallerReference>`,
		`<Path>/a</Path>`,
		`<Path>/b/*</Path>`,
		`<Quantity>2</Quantity>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Marshal output missing %q\nfull output:\n%s", want, got)
		}
	}
	// Roundtrip: unmarshal what we just produced and confirm equivalence.
	var rt InvalidationBatch
	if err := xml.Unmarshal(out, &rt); err != nil {
		t.Fatalf("roundtrip Unmarshal: %v", err)
	}
	if rt.CallerReference != in.CallerReference {
		t.Errorf("rt CallerReference: got %q want %q", rt.CallerReference, in.CallerReference)
	}
	if !equalStrings(rt.Paths.Items.Path, in.Paths.Items.Path) {
		t.Errorf("rt Paths.Items.Path: got %v want %v", rt.Paths.Items.Path, in.Paths.Items.Path)
	}
}

// TestInvalidation_Marshal_ResponseShape confirms the response top-level wrapper
// (no namespace per public API Reference) emits Id / Status / CreateTime / inner
// InvalidationBatch elements.
func TestInvalidation_Marshal_ResponseShape(t *testing.T) {
	in := &Invalidation{
		ID:         "I2J0I21PCZYDI6",
		Status:     "InProgress",
		CreateTime: "2026-05-02T07:00:00.000Z",
		InvalidationBatch: &InvalidationBatch{
			CallerReference: "ref-x",
			Paths: &Paths{
				Items:    PathItems{Path: []string{"/foo*"}},
				Quantity: 1,
			},
		},
	}
	out, err := xml.MarshalIndent(in, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(out)
	for _, want := range []string{
		`<Invalidation>`,
		`<Id>I2J0I21PCZYDI6</Id>`,
		`<Status>InProgress</Status>`,
		`<CreateTime>2026-05-02T07:00:00.000Z</CreateTime>`,
		`<CallerReference>ref-x</CallerReference>`,
		`<Path>/foo*</Path>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Marshal output missing %q\nfull output:\n%s", want, got)
		}
	}
}

// TestInvalidationList_Marshal_ResponseShape covers the ListInvalidations
// response shape including IsTruncated (which CachePolicyList lacks).
func TestInvalidationList_Marshal_ResponseShape(t *testing.T) {
	in := &InvalidationList{
		IsTruncated: true,
		Marker:      "",
		MaxItems:    100,
		NextMarker:  "I2J0I21PCZYDI6",
		Quantity:    2,
		Items: InvalidationListItems{
			InvalidationSummary: []InvalidationSummary{
				{ID: "I001", Status: "Completed", CreateTime: "2026-05-02T07:00:00.000Z"},
				{ID: "I002", Status: "InProgress", CreateTime: "2026-05-02T07:01:00.000Z"},
			},
		},
	}
	out, err := xml.MarshalIndent(in, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(out)
	for _, want := range []string{
		`<InvalidationList>`,
		`<IsTruncated>true</IsTruncated>`,
		`<MaxItems>100</MaxItems>`,
		`<NextMarker>I2J0I21PCZYDI6</NextMarker>`,
		`<Quantity>2</Quantity>`,
		`<InvalidationSummary>`,
		`<Id>I001</Id>`,
		`<Id>I002</Id>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Marshal output missing %q\nfull output:\n%s", want, got)
		}
	}
}

// TestInvalidationList_Marshal_OmitsNextMarkerWhenEmpty confirms the
// `omitempty` tag on NextMarker keeps the element off the wire when the list
// is the last page.
func TestInvalidationList_Marshal_OmitsNextMarkerWhenEmpty(t *testing.T) {
	in := &InvalidationList{
		IsTruncated: false,
		Marker:      "",
		MaxItems:    100,
		Quantity:    0,
	}
	out, err := xml.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(out), "<NextMarker>") {
		t.Errorf("expected NextMarker to be omitted when empty, got: %s", out)
	}
}

// --- ToSDK / FromSDK round-trip and conversion correctness ----------------

func TestInvalidationBatch_ToSDK_RecomputesQuantity(t *testing.T) {
	// Simulate a wrapper where Quantity is intentionally stale (3) but Items
	// holds 2 entries. ToSDK must trust the slice, never the supplied Quantity.
	in := &InvalidationBatch{
		CallerReference: "stale",
		Paths: &Paths{
			Items:    PathItems{Path: []string{"/a", "/b"}},
			Quantity: 3,
		},
	}
	got := in.ToSDK()
	if got == nil || got.Paths == nil {
		t.Fatalf("ToSDK returned nil: %+v", got)
	}
	if aws.ToInt32(got.Paths.Quantity) != 2 {
		t.Errorf("Quantity: got %d want 2 (recomputed from len(Items))", aws.ToInt32(got.Paths.Quantity))
	}
	if !equalStrings(got.Paths.Items, []string{"/a", "/b"}) {
		t.Errorf("Items: got %v", got.Paths.Items)
	}
	if aws.ToString(got.CallerReference) != "stale" {
		t.Errorf("CallerReference: got %q", aws.ToString(got.CallerReference))
	}
}

func TestInvalidationBatch_ToSDK_NilSafe(t *testing.T) {
	if (*InvalidationBatch)(nil).ToSDK() != nil {
		t.Error("nil receiver should produce nil output")
	}
	if (*Paths)(nil).toSDK() != nil {
		t.Error("nil Paths should produce nil output")
	}
}

func TestFromSDKInvalidationBatch_Roundtrip(t *testing.T) {
	src := &types.InvalidationBatch{
		CallerReference: aws.String("abc"),
		Paths: &types.Paths{
			Quantity: aws.Int32(2),
			Items:    []string{"/x", "/y/*"},
		},
	}
	wrap := FromSDKInvalidationBatch(src)
	if wrap == nil || wrap.Paths == nil {
		t.Fatalf("FromSDK returned nil")
	}
	if wrap.CallerReference != "abc" {
		t.Errorf("CallerReference: got %q", wrap.CallerReference)
	}
	if wrap.Paths.Quantity != 2 {
		t.Errorf("Quantity: got %d", wrap.Paths.Quantity)
	}
	if !equalStrings(wrap.Paths.Items.Path, []string{"/x", "/y/*"}) {
		t.Errorf("Path: got %v", wrap.Paths.Items.Path)
	}
	// Round-trip back to SDK and confirm the slice survives.
	back := wrap.ToSDK()
	if !equalStrings(back.Paths.Items, src.Paths.Items) {
		t.Errorf("roundtrip Items: got %v want %v", back.Paths.Items, src.Paths.Items)
	}
}

func TestFromSDKInvalidationBatch_NilSafe(t *testing.T) {
	if FromSDKInvalidationBatch(nil) != nil {
		t.Error("nil SDK input should produce nil wrapper")
	}
}

func TestFromSDKInvalidation_FormatsCreateTimeUTC(t *testing.T) {
	// JST input, want UTC on the wire (3-digit fractional, trailing Z).
	jst := time.FixedZone("JST", 9*60*60)
	createdJST := time.Date(2026, 5, 2, 16, 0, 0, 123_000_000, jst)
	src := &types.Invalidation{
		Id:         aws.String("I001"),
		Status:     aws.String("InProgress"),
		CreateTime: &createdJST,
		InvalidationBatch: &types.InvalidationBatch{
			CallerReference: aws.String("r"),
			Paths: &types.Paths{
				Quantity: aws.Int32(1),
				Items:    []string{"/foo*"},
			},
		},
	}
	wrap := FromSDKInvalidation(src)
	if wrap == nil {
		t.Fatal("FromSDKInvalidation returned nil")
	}
	if wrap.CreateTime != "2026-05-02T07:00:00.123Z" {
		t.Errorf("CreateTime: got %q want %q", wrap.CreateTime, "2026-05-02T07:00:00.123Z")
	}
	// Confirm the format parser accepts what we just produced.
	if _, err := parseInvalidationTime(wrap.CreateTime); err != nil {
		t.Errorf("parseInvalidationTime(%q): %v", wrap.CreateTime, err)
	}
	if wrap.ID != "I001" || wrap.Status != "InProgress" {
		t.Errorf("Id/Status: got %q/%q", wrap.ID, wrap.Status)
	}
	if wrap.InvalidationBatch == nil || wrap.InvalidationBatch.Paths == nil {
		t.Fatal("InvalidationBatch / Paths nil")
	}
	if !equalStrings(wrap.InvalidationBatch.Paths.Items.Path, []string{"/foo*"}) {
		t.Errorf("Paths.Items: got %v", wrap.InvalidationBatch.Paths.Items.Path)
	}
}

func TestFromSDKInvalidation_NilSafe(t *testing.T) {
	if FromSDKInvalidation(nil) != nil {
		t.Error("nil input should produce nil wrapper")
	}
}

func TestFromSDKInvalidationList_HappyPath(t *testing.T) {
	t1 := time.Date(2026, 5, 2, 7, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 2, 7, 1, 30, 0, time.UTC)
	src := &types.InvalidationList{
		IsTruncated: aws.Bool(true),
		Marker:      aws.String(""),
		MaxItems:    aws.Int32(100),
		NextMarker:  aws.String("I002"),
		Quantity:    aws.Int32(2),
		Items: []types.InvalidationSummary{
			{Id: aws.String("I001"), Status: aws.String("Completed"), CreateTime: &t1},
			{Id: aws.String("I002"), Status: aws.String("InProgress"), CreateTime: &t2},
		},
	}
	wrap := FromSDKInvalidationList(src)
	if wrap == nil {
		t.Fatal("FromSDKInvalidationList returned nil")
	}
	if !wrap.IsTruncated {
		t.Error("IsTruncated: got false want true")
	}
	if wrap.NextMarker != "I002" {
		t.Errorf("NextMarker: got %q", wrap.NextMarker)
	}
	if wrap.Quantity != 2 {
		t.Errorf("Quantity: got %d want 2 (recomputed)", wrap.Quantity)
	}
	if got := len(wrap.Items.InvalidationSummary); got != 2 {
		t.Fatalf("Items count: got %d want 2", got)
	}
	if wrap.Items.InvalidationSummary[0].ID != "I001" {
		t.Errorf("[0].ID: got %q", wrap.Items.InvalidationSummary[0].ID)
	}
	if wrap.Items.InvalidationSummary[0].CreateTime != "2026-05-02T07:00:00.000Z" {
		t.Errorf("[0].CreateTime: got %q", wrap.Items.InvalidationSummary[0].CreateTime)
	}
}

func TestFromSDKInvalidationList_NilSafe(t *testing.T) {
	if FromSDKInvalidationList(nil) != nil {
		t.Error("nil input should produce nil wrapper")
	}
}

func TestFromSDKInvalidationList_RecomputesQuantity(t *testing.T) {
	// Provide a stale Quantity (5) but only 1 item — wrapper Quantity must
	// reflect the slice length so the wire format is self-consistent.
	src := &types.InvalidationList{
		IsTruncated: aws.Bool(false),
		Marker:      aws.String(""),
		MaxItems:    aws.Int32(100),
		Quantity:    aws.Int32(5),
		Items: []types.InvalidationSummary{
			{Id: aws.String("I-only"), Status: aws.String("Completed")},
		},
	}
	wrap := FromSDKInvalidationList(src)
	if wrap.Quantity != 1 {
		t.Errorf("Quantity: got %d want 1 (recomputed from len(Items))", wrap.Quantity)
	}
}
