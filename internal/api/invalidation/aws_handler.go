package invalidation

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"

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
