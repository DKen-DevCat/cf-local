package invalidation

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
	matcher "github.com/DKen-DevCat/cf-local/internal/invalidation"
)

// awsMaxBodyBytes caps inbound XML request bodies. AWS's actual API caps for
// CreateInvalidation are well under 1 MiB (the path list is the only payload
// of substance) and cf-local does not gain anything from accepting larger.
const awsMaxBodyBytes = 1 << 20

// AWSHandler implements the AWS REST/XML Invalidation API endpoints
// (CreateInvalidation in 4b-5; GetInvalidation / ListInvalidations are added
// in 4b-7 / 4b-8). Distinct from the phase-3 Handler in this same package
// which serves the legacy simple-JSON `POST /_invalidate`.
//
// Routing wired by api.buildMux:
//
//	POST /2020-05-31/distribution/{distId}/invalidation       (Create, 4b-5)
//	GET  /2020-05-31/distribution/{distId}/invalidation/{id}  (Get,    4b-7)
//	GET  /2020-05-31/distribution/{distId}/invalidation       (List,   4b-8)
type AWSHandler struct {
	Store Store

	// EnqueueFn receives the newly minted invalidation ID and the path list
	// from the request batch after a successful Create. The 4b-6 worker
	// registers a function that adds (id, paths) to its run queue. nil is
	// allowed (used by 4b-5 unit tests that don't care about worker
	// dispatch); when nil, Create returns immediately with
	// Status=InProgress and no follow-up purge happens.
	//
	// Paths are passed as a fresh slice, decoupled from the persisted
	// Record, so the worker doesn't need to round-trip through Store to
	// fetch the batch.
	EnqueueFn func(invalidationID string, paths []string)
}

// Create handles POST /2020-05-31/distribution/{distId}/invalidation.
//
// Per AWS spec the response is 201 Created with `<Invalidation>` body and
// Status=InProgress. The actual purge work happens asynchronously in the
// worker (4b-6); Create only persists the record and enqueues.
func (h *AWSHandler) Create(w http.ResponseWriter, r *http.Request) {
	distID := r.PathValue("distId")
	if distID == "" {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "InvalidArgument",
			"distribution id required")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, awsMaxBodyBytes))
	if err != nil {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "InvalidArgument",
			"read body: "+err.Error())
		return
	}

	var wrapper awsxml.InvalidationBatch
	if err := xml.Unmarshal(body, &wrapper); err != nil {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "MalformedXML", err.Error())
		return
	}

	sdk := wrapper.ToSDK()
	if sdk == nil || sdk.CallerReference == nil || *sdk.CallerReference == "" {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "InvalidArgument",
			"CallerReference is required")
		return
	}
	if sdk.Paths == nil || len(sdk.Paths.Items) == 0 {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "InvalidArgument",
			"Paths must contain at least one path")
		return
	}

	// Validate every path against the strict CloudFront rules (4b-1 matcher).
	// Returning the first failure with index keeps the error specific enough
	// for a developer to fix the offending path without scrolling logs.
	for i, p := range sdk.Paths.Items {
		if _, err := matcher.ParsePattern(p); err != nil {
			awsxml.WriteXMLError(w, http.StatusBadRequest, "InvalidArgument",
				fmt.Sprintf("Paths[%d] %q: %s", i, p, err.Error()))
			return
		}
	}

	rec, err := h.Store.Create(r.Context(), distID, sdk)
	if err != nil {
		awsxml.WriteXMLError(w, http.StatusInternalServerError, "InternalError",
			err.Error())
		return
	}

	if h.EnqueueFn != nil {
		paths := append([]string(nil), sdk.Paths.Items...)
		h.EnqueueFn(rec.ID, paths)
	}

	writeInvalidationResponse(w, http.StatusCreated, rec)
}

// Get handles GET /2020-05-31/distribution/{distId}/invalidation/{id}.
//
// Returns 200 OK with `<Invalidation>` body when the record exists and
// belongs to the supplied distribution. AWS treats "wrong distribution +
// real invalidation ID" identically to a missing ID — both surface as 404
// NoSuchInvalidation, mirroring the Store.Get tuple semantics enforced in
// 4b-4.
func (h *AWSHandler) Get(w http.ResponseWriter, r *http.Request) {
	distID := r.PathValue("distId")
	invID := r.PathValue("id")
	if distID == "" || invID == "" {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "InvalidArgument",
			"distribution id and invalidation id are required")
		return
	}
	rec, err := h.Store.Get(r.Context(), distID, invID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			awsxml.WriteXMLError(w, http.StatusNotFound, "NoSuchInvalidation",
				fmt.Sprintf("the invalidation does not exist: %s", invID))
			return
		}
		awsxml.WriteXMLError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	writeInvalidationResponse(w, http.StatusOK, rec)
}

// listMaxItemsDefault matches the AWS default for ListInvalidations when the
// caller omits the MaxItems query parameter.
const listMaxItemsDefault = 100

// listMaxItemsCap is the upper bound cf-local enforces. AWS itself caps at 100
// for this API; values above are clamped down rather than rejected so the
// Provider's "give me everything" call shape (MaxItems=200) keeps working
// without the caller having to special-case the local endpoint.
const listMaxItemsCap = 100

// List handles GET /2020-05-31/distribution/{distId}/invalidation.
//
// Pagination follows AWS CloudFront convention:
//   - MaxItems (query, optional): page size. Empty → 100. Non-numeric or
//     <=0 → 400 InvalidArgument. Values >100 are clamped to 100.
//   - Marker (query, optional): the ID of the last item returned by the
//     previous page. Items strictly after that ID (in CreateTime DESC, ID
//     tie-break order) are returned.
//
// Unknown Marker values are treated as "no records after this point" and
// return an empty page rather than 400 — AWS itself is permissive here and a
// stricter check would only force callers to swallow the error.
//
// The response Marker echoes the input Marker verbatim; NextMarker is set
// only when IsTruncated=true and carries the last ID of the current page so
// the caller can pass it into the next request as Marker.
func (h *AWSHandler) List(w http.ResponseWriter, r *http.Request) {
	distID := r.PathValue("distId")
	if distID == "" {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "InvalidArgument",
			"distribution id required")
		return
	}

	q := r.URL.Query()
	marker := q.Get("Marker")
	maxItems, ok := parseListMaxItems(w, q.Get("MaxItems"))
	if !ok {
		return
	}

	records, err := h.Store.List(r.Context(), distID)
	if err != nil {
		awsxml.WriteXMLError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	// Slice the full sorted list down to the requested page. Store.List
	// already enforces newest-first + ID tie-break, so the only work here is
	// to find the marker offset and cap by maxItems.
	page, isTruncated, nextMarker := paginateRecords(records, marker, maxItems)

	list := awsxml.InvalidationList{
		Marker:      marker,
		MaxItems:    maxItems,
		IsTruncated: isTruncated,
		NextMarker:  nextMarker,
	}
	for _, rec := range page {
		list.Items.InvalidationSummary = append(list.Items.InvalidationSummary,
			awsxml.InvalidationSummary{
				ID:         rec.ID,
				Status:     rec.Status,
				CreateTime: rec.CreateTime.UTC().Format("2006-01-02T15:04:05.000Z"),
			})
	}
	// Quantity must equal len(Items) on the wire (InconsistentQuantities 400);
	// derive it from the slice we just built rather than from len(page) so a
	// future filter pass cannot drift the two apart.
	list.Quantity = len(list.Items.InvalidationSummary)

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	// Encode error is intentionally ignored: the response status is already
	// committed and we cannot send a new status. A partial body is preferable
	// to a panic.
	_ = enc.Encode(&list)
	_ = enc.Close()
}

// parseListMaxItems parses the MaxItems query parameter with the AWS-style
// fallbacks. On validation failure it has already emitted the AWS error
// envelope and the caller should return.
func parseListMaxItems(w http.ResponseWriter, raw string) (int, bool) {
	if raw == "" {
		return listMaxItemsDefault, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "InvalidArgument",
			fmt.Sprintf("MaxItems must be a positive integer, got %q", raw))
		return 0, false
	}
	if n <= 0 {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "InvalidArgument",
			fmt.Sprintf("MaxItems must be a positive integer, got %d", n))
		return 0, false
	}
	if n > listMaxItemsCap {
		return listMaxItemsCap, true
	}
	return n, true
}

// paginateRecords slices `records` (already sorted newest-first by Store.List)
// using AWS Marker semantics: when marker is non-empty, the page starts at
// the record immediately after the one with that ID. An unknown marker
// yields an empty page (no error). Returns the page slice, whether more
// records remain, and the NextMarker to advertise (empty unless truncated).
func paginateRecords(records []*Record, marker string, maxItems int) ([]*Record, bool, string) {
	start := 0
	if marker != "" {
		found := false
		for i, rec := range records {
			if rec.ID == marker {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, false, ""
		}
	}
	if start >= len(records) {
		return nil, false, ""
	}
	end := start + maxItems
	truncated := false
	if end < len(records) {
		truncated = true
	} else {
		end = len(records)
	}
	page := records[start:end]
	nextMarker := ""
	if truncated && len(page) > 0 {
		nextMarker = page[len(page)-1].ID
	}
	return page, truncated, nextMarker
}

// writeInvalidationResponse emits a 201 / 200 response with the
// `<Invalidation>` body and the standard ETag header.
//
// AWS's actual CreateInvalidation response does NOT include ETag (unlike the
// CachePolicy / Distribution responses) — the SDK Invalidation type has no
// ETag field. cf-local follows the same convention.
func writeInvalidationResponse(w http.ResponseWriter, status int, rec *Record) {
	resp := toResponseInvalidation(rec)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	// Encode error is intentionally ignored: the response status is already
	// committed and we cannot send a new status. A partial body is preferable
	// to a panic.
	_ = enc.Encode(resp)
	_ = enc.Close()
}

// toResponseInvalidation projects a Record into the XML wrapper shape AWS
// returns from Create / Get.
func toResponseInvalidation(rec *Record) *awsxml.Invalidation {
	return &awsxml.Invalidation{
		ID:                rec.ID,
		Status:            rec.Status,
		CreateTime:        rec.CreateTime.UTC().Format("2006-01-02T15:04:05.000Z"),
		InvalidationBatch: awsxml.FromSDKInvalidationBatch(rec.Batch),
	}
}
