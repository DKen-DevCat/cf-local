package awsxml

import (
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// invalidation_convert.go: bridge between the XML I/O wrapper structs and the
// AWS SDK Go v2 types representation. Same conventions as cache_policy_convert.go:
//
//   - Quantity is recomputed from len(Items) — wrapper caller never sets it
//   - nil-safe both directions; nil in => nil out
//   - CreateTime crosses the boundary as ISO 8601 UTC string in the XML and
//     *time.Time in the SDK type
//
// The ISO 8601 format used (`2006-01-02T15:04:05.000Z`) matches what AWS
// CloudFront emits on the wire for these timestamps.

// invalidationTimeFormat is the on-the-wire format AWS uses for Invalidation
// CreateTime. Three-digit fractional seconds, UTC indicated by trailing 'Z'.
const invalidationTimeFormat = "2006-01-02T15:04:05.000Z"

// ToSDK converts the request-body wrapper to the AWS SDK Go v2 representation.
// Quantity is recomputed from len(Items.Path); the wrapper caller does not need
// to keep them in sync.
func (b *InvalidationBatch) ToSDK() *types.InvalidationBatch {
	if b == nil {
		return nil
	}
	out := &types.InvalidationBatch{
		CallerReference: aws.String(b.CallerReference),
	}
	if b.Paths != nil {
		out.Paths = b.Paths.toSDK()
	}
	return out
}

func (p *Paths) toSDK() *types.Paths {
	if p == nil {
		return nil
	}
	out := &types.Paths{
		Quantity: aws.Int32(int32(len(p.Items.Path))),
	}
	if len(p.Items.Path) > 0 {
		out.Items = append([]string(nil), p.Items.Path...)
	}
	return out
}

// FromSDKInvalidationBatch converts the SDK request type back to the XML
// wrapper, recomputing Quantity from the Items slice length.
func FromSDKInvalidationBatch(in *types.InvalidationBatch) *InvalidationBatch {
	if in == nil {
		return nil
	}
	out := &InvalidationBatch{
		CallerReference: aws.ToString(in.CallerReference),
	}
	if in.Paths != nil {
		out.Paths = fromSDKPaths(in.Paths)
	}
	return out
}

func fromSDKPaths(in *types.Paths) *Paths {
	if in == nil {
		return nil
	}
	items := append([]string(nil), in.Items...)
	return &Paths{
		Items:    PathItems{Path: items},
		Quantity: len(items),
	}
}

// FromSDKInvalidation converts the SDK Invalidation response shape to the XML
// wrapper. CreateTime is rendered with invalidationTimeFormat (UTC).
func FromSDKInvalidation(in *types.Invalidation) *Invalidation {
	if in == nil {
		return nil
	}
	out := &Invalidation{
		ID:     aws.ToString(in.Id),
		Status: aws.ToString(in.Status),
	}
	if in.CreateTime != nil {
		out.CreateTime = in.CreateTime.UTC().Format(invalidationTimeFormat)
	}
	if in.InvalidationBatch != nil {
		out.InvalidationBatch = FromSDKInvalidationBatch(in.InvalidationBatch)
	}
	return out
}

// FromSDKInvalidationList converts the SDK paginated list response shape to
// the XML wrapper. Quantity is recomputed from len(Items) — IsTruncated and
// NextMarker pass through verbatim because they are pagination metadata
// driven by the handler, not by the slice length.
func FromSDKInvalidationList(in *types.InvalidationList) *InvalidationList {
	if in == nil {
		return nil
	}
	items := make([]InvalidationSummary, 0, len(in.Items))
	for i := range in.Items {
		items = append(items, fromSDKInvalidationSummary(&in.Items[i]))
	}
	out := &InvalidationList{
		IsTruncated: aws.ToBool(in.IsTruncated),
		Items:       InvalidationListItems{InvalidationSummary: items},
		Marker:      aws.ToString(in.Marker),
		MaxItems:    int(aws.ToInt32(in.MaxItems)),
		Quantity:    len(items),
	}
	if in.NextMarker != nil {
		out.NextMarker = *in.NextMarker
	}
	return out
}

func fromSDKInvalidationSummary(in *types.InvalidationSummary) InvalidationSummary {
	out := InvalidationSummary{
		ID:     aws.ToString(in.Id),
		Status: aws.ToString(in.Status),
	}
	if in.CreateTime != nil {
		out.CreateTime = in.CreateTime.UTC().Format(invalidationTimeFormat)
	}
	return out
}

// parseInvalidationTime parses an ISO 8601 timestamp emitted by FromSDK*
// conversions back to a *time.Time. Used in tests; handlers do not need a
// reverse conversion because Invalidation responses are write-only on the
// SDK side (handlers convert SDK→XML, never XML→SDK for this field).
func parseInvalidationTime(s string) (time.Time, error) {
	return time.Parse(invalidationTimeFormat, s)
}
