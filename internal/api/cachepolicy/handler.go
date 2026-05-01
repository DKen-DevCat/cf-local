package cachepolicy

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
)

// maxBodyBytes caps inbound XML request bodies. AWS's actual API caps are well
// under 1 MiB and cf-local does not gain anything from accepting larger.
const maxBodyBytes = 1 << 20

// Handler implements the AWS REST/XML CachePolicy CRUD endpoints. Each method
// is a http.HandlerFunc that api.buildMux registers against a distinct
// Method+Path pattern (Go 1.22+ ServeMux pattern syntax).
type Handler struct {
	Store Store
}

// Create handles POST /2020-05-31/cache-policy.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	cfg, ok := decodeConfig(w, r)
	if !ok {
		return
	}
	rec, err := h.Store.Create(r.Context(), cfg)
	if err != nil {
		switch {
		case errors.Is(err, ErrAlreadyExists):
			awsxml.WriteXMLError(w, http.StatusConflict, "CachePolicyAlreadyExists",
				fmt.Sprintf("a cache policy already exists with the same name: %s", *cfg.Name))
		default:
			awsxml.WriteXMLError(w, http.StatusInternalServerError, "InternalError", err.Error())
		}
		return
	}
	writeCachePolicyResponse(w, http.StatusCreated, rec)
}

// Get handles GET /2020-05-31/cache-policy/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := h.Store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			awsxml.WriteXMLError(w, http.StatusNotFound, "NoSuchCachePolicy",
				fmt.Sprintf("the cache policy does not exist: %s", id))
			return
		}
		awsxml.WriteXMLError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	writeCachePolicyResponse(w, http.StatusOK, rec)
}

// Update handles PUT /2020-05-31/cache-policy/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	cfg, ok := decodeConfig(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	ifMatch := r.Header.Get("If-Match")
	rec, err := h.Store.Update(r.Context(), id, cfg, ifMatch)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			awsxml.WriteXMLError(w, http.StatusNotFound, "NoSuchCachePolicy",
				fmt.Sprintf("the cache policy does not exist: %s", id))
		case errors.Is(err, ErrAlreadyExists):
			awsxml.WriteXMLError(w, http.StatusConflict, "CachePolicyAlreadyExists",
				fmt.Sprintf("a cache policy already exists with the same name: %s", *cfg.Name))
		default:
			awsxml.WriteXMLError(w, http.StatusInternalServerError, "InternalError", err.Error())
		}
		return
	}
	writeCachePolicyResponse(w, http.StatusOK, rec)
}

// Delete handles DELETE /2020-05-31/cache-policy/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ifMatch := r.Header.Get("If-Match")
	if err := h.Store.Delete(r.Context(), id, ifMatch); err != nil {
		if errors.Is(err, ErrNotFound) {
			awsxml.WriteXMLError(w, http.StatusNotFound, "NoSuchCachePolicy",
				fmt.Sprintf("the cache policy does not exist: %s", id))
			return
		}
		awsxml.WriteXMLError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// List handles GET /2020-05-31/cache-policy. Phase 4-A returns every record on
// a single page; Marker / MaxItems / Type query params are accepted but
// ignored. The Provider's data source aggregates across calls fine when there
// is only one page.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	records, err := h.Store.List(r.Context())
	if err != nil {
		awsxml.WriteXMLError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	list := awsxml.CachePolicyList{
		Quantity: len(records),
		MaxItems: 100,
	}
	for _, rec := range records {
		list.Items.CachePolicySummary = append(list.Items.CachePolicySummary, awsxml.CachePolicySummary{
			Type:        "custom",
			CachePolicy: *toResponseCachePolicy(rec),
		})
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(&list)
}

// decodeConfig reads the XML request body and returns the SDK-shaped config.
// On any failure it has already emitted the AWS error envelope and the caller
// should return.
func decodeConfig(w http.ResponseWriter, r *http.Request) (*types.CachePolicyConfig, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "InvalidArgument", "read body: "+err.Error())
		return nil, false
	}
	var wrapper awsxml.CachePolicyConfig
	if err := xml.Unmarshal(body, &wrapper); err != nil {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "MalformedXML", err.Error())
		return nil, false
	}
	cfg := wrapper.ToSDK()
	if cfg == nil || cfg.Name == nil || *cfg.Name == "" {
		awsxml.WriteXMLError(w, http.StatusBadRequest, "InvalidArgument", "Name is required")
		return nil, false
	}
	return cfg, true
}

// writeCachePolicyResponse emits a 201 / 200 response with ETag header and a
// <CachePolicy> body wrapping the stored config.
func writeCachePolicyResponse(w http.ResponseWriter, status int, rec *Record) {
	w.Header().Set("ETag", rec.ETag)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	cp := toResponseCachePolicy(rec)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(cp)
}

// toResponseCachePolicy projects a Record into the XML wrapper shape AWS
// returns from Get / Create / Update.
func toResponseCachePolicy(rec *Record) *awsxml.CachePolicy {
	return &awsxml.CachePolicy{
		ID:                rec.ID,
		LastModifiedTime:  rec.LastModifiedTime.Format(time.RFC3339),
		CachePolicyConfig: awsxml.FromSDKCachePolicyConfig(rec.Config),
	}
}
