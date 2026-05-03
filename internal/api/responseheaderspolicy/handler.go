package responseheaderspolicy

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/DKen-DevCat/cf-local/internal/api/validate"
	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
)

// maxBodyBytes caps inbound XML request bodies. AWS's actual API caps are
// well under 1 MiB and cf-local does not gain anything from accepting
// larger.
const maxBodyBytes = 1 << 20

// Handler implements the AWS REST/XML ResponseHeadersPolicy CRUD
// endpoints. Each method is a http.HandlerFunc that api.buildMux
// registers against a distinct Method+Path pattern.
type Handler struct {
	Store Store
}

// Create handles POST /2020-05-31/response-headers-policy.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	cfg, ok := decodeConfig(w, r)
	if !ok {
		return
	}
	rec, err := h.Store.Create(r.Context(), cfg)
	if err != nil {
		switch {
		case errors.Is(err, ErrAlreadyExists):
			awsxml.WriteError(w, awsxml.CodeResponseHeadersPolicyAlreadyExists,
				fmt.Sprintf("a response headers policy already exists with the same name: %s", *cfg.Name))
		default:
			awsxml.WriteInternalError(w, err)
		}
		return
	}
	writeResponseHeadersPolicyResponse(w, http.StatusCreated, rec)
}

// Get handles GET /2020-05-31/response-headers-policy/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := h.Store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			awsxml.WriteError(w, awsxml.CodeNoSuchResponseHeadersPolicy,
				fmt.Sprintf("the response headers policy does not exist: %s", id))
			return
		}
		awsxml.WriteInternalError(w, err)
		return
	}
	writeResponseHeadersPolicyResponse(w, http.StatusOK, rec)
}

// Update handles PUT /2020-05-31/response-headers-policy/{id}.
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
			awsxml.WriteError(w, awsxml.CodeNoSuchResponseHeadersPolicy,
				fmt.Sprintf("the response headers policy does not exist: %s", id))
		case errors.Is(err, ErrAlreadyExists):
			awsxml.WriteError(w, awsxml.CodeResponseHeadersPolicyAlreadyExists,
				fmt.Sprintf("a response headers policy already exists with the same name: %s", *cfg.Name))
		default:
			awsxml.WriteInternalError(w, err)
		}
		return
	}
	writeResponseHeadersPolicyResponse(w, http.StatusOK, rec)
}

// Delete handles DELETE /2020-05-31/response-headers-policy/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ifMatch := r.Header.Get("If-Match")
	if err := h.Store.Delete(r.Context(), id, ifMatch); err != nil {
		if errors.Is(err, ErrNotFound) {
			awsxml.WriteError(w, awsxml.CodeNoSuchResponseHeadersPolicy,
				fmt.Sprintf("the response headers policy does not exist: %s", id))
			return
		}
		awsxml.WriteInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// List handles GET /2020-05-31/response-headers-policy. Phase 4-C
// returns every record on a single page; Marker / MaxItems / Type query
// params are accepted but ignored. Type is hard-coded "custom" since
// managed response headers policies are out of scope for phase-4c.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	records, err := h.Store.List(r.Context())
	if err != nil {
		awsxml.WriteInternalError(w, err)
		return
	}

	list := awsxml.ResponseHeadersPolicyList{MaxItems: 100}
	for _, rec := range records {
		list.Items.ResponseHeadersPolicySummary = append(list.Items.ResponseHeadersPolicySummary, awsxml.ResponseHeadersPolicySummary{
			Type:                  "custom",
			ResponseHeadersPolicy: *toResponseResponseHeadersPolicy(rec),
		})
	}
	list.Quantity = len(list.Items.ResponseHeadersPolicySummary)

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	// Encode error is intentionally ignored: the response status is
	// already committed and we cannot send a new status. A partial body
	// is preferable to a panic.
	_ = enc.Encode(&list)
	_ = enc.Close()
}

// decodeConfig reads the XML request body and returns the SDK-shaped
// config. On any failure it has already emitted the AWS error envelope.
// 4c-1 design: SecurityHeadersConfig / ServerTimingHeadersConfig /
// RemoveHeadersConfig are accepted and round-tripped, but a warning is
// logged because the nginx renderer (4c-2) does not honour them.
func decodeConfig(w http.ResponseWriter, r *http.Request) (*types.ResponseHeadersPolicyConfig, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		awsxml.WriteError(w, awsxml.CodeInvalidArgument, "read body: "+err.Error())
		return nil, false
	}
	var wrapper awsxml.ResponseHeadersPolicyConfig
	if err := xml.Unmarshal(body, &wrapper); err != nil {
		awsxml.WriteError(w, awsxml.CodeMalformedXML, err.Error())
		return nil, false
	}
	cfg := wrapper.ToSDK()
	if err := validate.ResponseHeadersPolicyConfig(cfg); err != nil {
		awsxml.WriteError(w, awsxml.CodeInvalidArgument, err.Error())
		return nil, false
	}
	warnUnsupportedSubconfigs(*cfg.Name, cfg)
	return cfg, true
}

// warnUnsupportedSubconfigs emits a single slog warn line per request when
// the caller sets one of the sub-configs cf-local persists but does not yet
// render into nginx. The warning mentions the policy name + the list of
// unsupported sub-configs so the operator can correlate it with their
// Terraform / AWS CLI input.
func warnUnsupportedSubconfigs(name string, cfg *types.ResponseHeadersPolicyConfig) {
	var unsupported []string
	if cfg.SecurityHeadersConfig != nil {
		unsupported = append(unsupported, "SecurityHeadersConfig")
	}
	if cfg.ServerTimingHeadersConfig != nil {
		unsupported = append(unsupported, "ServerTimingHeadersConfig")
	}
	if cfg.RemoveHeadersConfig != nil {
		unsupported = append(unsupported, "RemoveHeadersConfig")
	}
	if len(unsupported) == 0 {
		return
	}
	slog.Warn("response_headers_policy_subconfig_ignored",
		slog.String("policy", name),
		slog.Any("unsupported", unsupported),
		slog.String("note", "phase-4c scope: only CustomHeadersConfig + CorsConfig are wired into nginx"),
	)
}

// writeResponseHeadersPolicyResponse emits a 201 / 200 response with
// ETag header and a <ResponseHeadersPolicy> body wrapping the stored
// config.
func writeResponseHeadersPolicyResponse(w http.ResponseWriter, status int, rec *Record) {
	w.Header().Set("ETag", rec.ETag)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	body := toResponseResponseHeadersPolicy(rec)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	// See List handler for why the encode error is ignored.
	_ = enc.Encode(body)
	_ = enc.Close()
}

func toResponseResponseHeadersPolicy(rec *Record) *awsxml.ResponseHeadersPolicy {
	return &awsxml.ResponseHeadersPolicy{
		ID:                          rec.ID,
		LastModifiedTime:            rec.LastModifiedTime.Format(time.RFC3339),
		ResponseHeadersPolicyConfig: awsxml.FromSDKResponseHeadersPolicyConfig(rec.Config),
	}
}
