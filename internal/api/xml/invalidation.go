package awsxml

import "encoding/xml"

// invalidation.go: XML wrapper structs for the AWS CloudFront Invalidation API
// (CreateInvalidation / GetInvalidation / ListInvalidations).
//
// AWS REST/XML wire shape (from public API Reference syntax):
//
//	<InvalidationBatch xmlns="...2020-05-31/">       <-- request body
//	  <CallerReference>...</CallerReference>
//	  <Paths>
//	    <Quantity>N</Quantity>
//	    <Items>
//	      <Path>/foo</Path>
//	      <Path>/bar/*</Path>
//	    </Items>
//	  </Paths>
//	</InvalidationBatch>
//
//	<Invalidation>                                    <-- response body
//	  <Id>I2J0I21PCZYDI6</Id>
//	  <Status>InProgress</Status>
//	  <CreateTime>2026-05-02T07:00:00.000Z</CreateTime>
//	  <InvalidationBatch>...</InvalidationBatch>
//	</Invalidation>
//
// Note: The `<Path>` element name (in `Paths/Items`) is intentionally different
// from the `<Name>` element used by CachePolicy / OriginRequestPolicy whitelists.
// We do not reuse the `Names` / `Items` types from cache_policy.go for that
// reason — the wire format is genuinely different.

// InvalidationBatch is the request body of CreateInvalidation and the inner
// element of the Invalidation response. It carries the unique CallerReference
// supplied by the client and the list of paths to invalidate.
type InvalidationBatch struct {
	XMLName         xml.Name `xml:"http://cloudfront.amazonaws.com/doc/2020-05-31/ InvalidationBatch"`
	CallerReference string   `xml:"CallerReference"`
	Paths           *Paths   `xml:"Paths"`
}

// Paths is the AWS list-of-paths shape with explicit Quantity. Quantity must
// equal len(Items.Path) on the wire; FromSDKInvalidationBatch / handlers
// recompute it from the slice length so the wrapper caller never has to keep
// the two in sync.
type Paths struct {
	Items    PathItems `xml:"Items"`
	Quantity int       `xml:"Quantity"`
}

// PathItems wraps the repeated <Path> children of `Paths/Items`. It is named
// distinctly from the cache_policy.go `Items` type because the inner element
// name differs (`<Path>` vs `<Name>`).
type PathItems struct {
	Path []string `xml:"Path"`
}

// Invalidation is the response body of GetInvalidation and CreateInvalidation.
// CreateTime is rendered as an ISO 8601 UTC string (e.g. "2026-05-02T07:00:00.000Z")
// to match the wire format AWS emits — the SDK uses *time.Time internally but
// the XML side is a plain string, mirroring how cache_policy.go handles
// LastModifiedTime.
type Invalidation struct {
	XMLName           xml.Name           `xml:"Invalidation"`
	CreateTime        string             `xml:"CreateTime"`
	ID                string             `xml:"Id"`
	InvalidationBatch *InvalidationBatch `xml:"InvalidationBatch"`
	Status            string             `xml:"Status"`
}

// InvalidationList is the response body of ListInvalidations. Unlike
// CachePolicyList (which has no IsTruncated field), the InvalidationList
// shape DOES include IsTruncated — verbatim from the AWS API Reference.
type InvalidationList struct {
	XMLName     xml.Name              `xml:"InvalidationList"`
	IsTruncated bool                  `xml:"IsTruncated"`
	Items       InvalidationListItems `xml:"Items"`
	Marker      string                `xml:"Marker"`
	MaxItems    int                   `xml:"MaxItems"`
	NextMarker  string                `xml:"NextMarker,omitempty"`
	Quantity    int                   `xml:"Quantity"`
}

// InvalidationListItems wraps the repeated <InvalidationSummary> children.
type InvalidationListItems struct {
	InvalidationSummary []InvalidationSummary `xml:"InvalidationSummary"`
}

// InvalidationSummary is one row of the ListInvalidations response. It
// intentionally OMITS the InvalidationBatch — only Id / Status / CreateTime
// per the AWS API spec (callers fetch full details via GetInvalidation).
type InvalidationSummary struct {
	CreateTime string `xml:"CreateTime"`
	ID         string `xml:"Id"`
	Status     string `xml:"Status"`
}
