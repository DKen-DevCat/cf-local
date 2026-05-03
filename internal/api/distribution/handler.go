package distribution

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
)

// maxBodyBytes caps inbound XML request bodies. AWS's actual API caps are
// well under 1 MiB and cf-local does not gain anything from accepting
// larger.
const maxBodyBytes = 1 << 20

// Handler implements the AWS REST/XML Distribution CRUD endpoints. Each
// method is a http.HandlerFunc that api.buildMux registers against a
// distinct Method+Path pattern (Go 1.22+ ServeMux pattern syntax).
type Handler struct {
	Store Store
}

// Create handles POST /2020-05-31/distribution.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	cfg, ok := decodeConfig(w, r)
	if !ok {
		return
	}
	rec, err := h.Store.Create(r.Context(), cfg)
	if err != nil {
		switch {
		case errors.Is(err, ErrAlreadyExists):
			awsxml.WriteError(w, awsxml.CodeDistributionAlreadyExists,
				fmt.Sprintf("a distribution already exists with the same caller reference: %s", *cfg.CallerReference))
		default:
			awsxml.WriteInternalError(w, err)
		}
		return
	}
	writeDistributionResponse(w, http.StatusCreated, rec)
}

// Get handles GET /2020-05-31/distribution/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := h.Store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			awsxml.WriteError(w, awsxml.CodeNoSuchDistribution,
				fmt.Sprintf("the distribution does not exist: %s", id))
			return
		}
		awsxml.WriteInternalError(w, err)
		return
	}
	writeDistributionResponse(w, http.StatusOK, rec)
}

// Update handles PUT /2020-05-31/distribution/{id}/config.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	cfg, ok := decodeConfig(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	ifMatch := r.Header.Get("If-Match")
	rec, err := h.Store.Update(r.Context(), id, cfg, ifMatch)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			awsxml.WriteError(w, awsxml.CodeNoSuchDistribution,
				fmt.Sprintf("the distribution does not exist: %s", id))
			return
		}
		awsxml.WriteInternalError(w, err)
		return
	}
	writeDistributionResponse(w, http.StatusOK, rec)
}

// GetConfig handles GET /2020-05-31/distribution/{id}/config. Returns the
// bare <DistributionConfig> envelope (no <Distribution> metadata wrapper).
// Provider uses this to fetch the latest ETag before an Update.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := h.Store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			awsxml.WriteError(w, awsxml.CodeNoSuchDistribution,
				fmt.Sprintf("the distribution does not exist: %s", id))
			return
		}
		awsxml.WriteInternalError(w, err)
		return
	}
	w.Header().Set("ETag", rec.ETag)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	// See List handler for why the encode error is ignored.
	_ = enc.Encode(awsxml.FromSDKDistributionConfig(rec.Config))
	_ = enc.Close()
}

// Delete handles DELETE /2020-05-31/distribution/{id}. AWS requires the
// distribution to be Disabled before delete; cf-local skips that check in
// phase-4a (4a-C will add it).
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ifMatch := r.Header.Get("If-Match")
	if err := h.Store.Delete(r.Context(), id, ifMatch); err != nil {
		if errors.Is(err, ErrNotFound) {
			awsxml.WriteError(w, awsxml.CodeNoSuchDistribution,
				fmt.Sprintf("the distribution does not exist: %s", id))
			return
		}
		awsxml.WriteInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// List handles GET /2020-05-31/distribution. Phase 4-A returns every record
// on a single page; Marker / MaxItems query params are accepted but
// ignored.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	records, err := h.Store.List(r.Context())
	if err != nil {
		awsxml.WriteInternalError(w, err)
		return
	}

	list := awsxml.DistributionList{MaxItems: 100, IsTruncated: false}
	for _, rec := range records {
		list.Items.DistributionSummary = append(list.Items.DistributionSummary, toDistributionSummary(rec))
	}
	// Quantity must equal len(Items) on the wire (InconsistentQuantities 400);
	// derive from the slice we just built so future filtering keeps the
	// invariant.
	list.Quantity = len(list.Items.DistributionSummary)

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

// decodeConfig reads the XML request body and returns the SDK-shaped
// config. Accepts both <DistributionConfig> (CreateDistribution /
// UpdateDistribution path) and <DistributionConfigWithTags>
// (CreateDistributionWithTags path; Terraform AWS Provider always uses
// this variant). On any failure it has already emitted the AWS error
// envelope and the caller should return.
func decodeConfig(w http.ResponseWriter, r *http.Request) (*types.DistributionConfig, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		awsxml.WriteError(w, awsxml.CodeInvalidArgument, "read body: "+err.Error())
		return nil, false
	}

	rootName, err := peekRootElement(body)
	if err != nil {
		awsxml.WriteError(w, awsxml.CodeMalformedXML, err.Error())
		return nil, false
	}

	var wrapper *awsxml.DistributionConfig
	switch rootName {
	case "DistributionConfigWithTags":
		var withTags awsxml.DistributionConfigWithTags
		if err := xml.Unmarshal(body, &withTags); err != nil {
			awsxml.WriteError(w, awsxml.CodeMalformedXML, err.Error())
			return nil, false
		}
		// Tags are intentionally dropped: cf-local does not track tags
		// (out of scope for phase-4a).
		wrapper = withTags.DistributionConfig
	case "DistributionConfig":
		var direct awsxml.DistributionConfig
		if err := xml.Unmarshal(body, &direct); err != nil {
			awsxml.WriteError(w, awsxml.CodeMalformedXML, err.Error())
			return nil, false
		}
		wrapper = &direct
	default:
		awsxml.WriteError(w, awsxml.CodeMalformedXML,
			fmt.Sprintf("expected element type <DistributionConfig> or <DistributionConfigWithTags> but have <%s>", rootName))
		return nil, false
	}

	cfg := wrapper.ToSDK()
	if cfg == nil || cfg.CallerReference == nil || *cfg.CallerReference == "" {
		awsxml.WriteError(w, awsxml.CodeInvalidArgument, "CallerReference is required")
		return nil, false
	}
	if cfg.Origins == nil || len(cfg.Origins.Items) == 0 {
		awsxml.WriteError(w, awsxml.CodeInvalidArgument, "Origins is required and must contain at least one origin")
		return nil, false
	}
	if cfg.DefaultCacheBehavior == nil || cfg.DefaultCacheBehavior.TargetOriginId == nil {
		awsxml.WriteError(w, awsxml.CodeInvalidArgument, "DefaultCacheBehavior is required")
		return nil, false
	}
	return cfg, true
}

// writeDistributionResponse emits a 201 / 200 response with ETag header
// and a <Distribution> body wrapping the stored config. ARN / DomainName
// are synthesised from the Id; Status is always "Deployed" (cf-local has
// no async deploy pipeline). Active*Signers / *KeyGroups are emitted with
// Quantity=0 to keep Provider state diffs stable.
func writeDistributionResponse(w http.ResponseWriter, status int, rec *Record) {
	w.Header().Set("ETag", rec.ETag)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	d := toDistribution(rec)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	// See List handler for why the encode error is ignored.
	_ = enc.Encode(d)
	_ = enc.Close()
}

func toDistribution(rec *Record) *awsxml.Distribution {
	return &awsxml.Distribution{
		ID:                            rec.ID,
		ARN:                           distributionARN(rec.ID),
		Status:                        "Deployed",
		LastModifiedTime:              rec.LastModifiedTime.Format(time.RFC3339),
		InProgressInvalidationBatches: 0,
		DomainName:                    distributionDomainName(rec.ID),
		ActiveTrustedSigners:          &awsxml.ActiveTrustedSigners{Enabled: false, Quantity: 0},
		ActiveTrustedKeyGroups:        &awsxml.ActiveTrustedKeyGroups{Enabled: false, Quantity: 0},
		AliasICPRecordals:             &awsxml.AliasICPRecordals{Quantity: 0},
		DistributionConfig:            awsxml.FromSDKDistributionConfig(rec.Config),
	}
}

func toDistributionSummary(rec *Record) awsxml.DistributionSummary {
	cfg := awsxml.FromSDKDistributionConfig(rec.Config)
	s := awsxml.DistributionSummary{
		ID:               rec.ID,
		ARN:              distributionARN(rec.ID),
		Status:           "Deployed",
		LastModifiedTime: rec.LastModifiedTime.Format(time.RFC3339),
		DomainName:       distributionDomainName(rec.ID),
	}
	if cfg != nil {
		s.Aliases = cfg.Aliases
		s.CacheBehaviors = cfg.CacheBehaviors
		s.Comment = cfg.Comment
		s.CustomErrorResponses = cfg.CustomErrorResponses
		s.DefaultCacheBehavior = cfg.DefaultCacheBehavior
		s.Enabled = cfg.Enabled
		s.HTTPVersion = cfg.HTTPVersion
		s.IsIPV6Enabled = cfg.IsIPV6Enabled
		s.OriginGroups = cfg.OriginGroups
		s.Origins = cfg.Origins
		s.PriceClass = cfg.PriceClass
		s.Restrictions = cfg.Restrictions
		s.Staging = cfg.Staging
		s.ViewerCertificate = cfg.ViewerCertificate
		s.WebACLID = cfg.WebACLID
	}
	return s
}

// distributionARN synthesises the ARN. cf-local uses a fixed account
// number ("000000000000") since it has no IAM concept; Provider treats
// the value as opaque so the format only needs to be syntactically valid.
func distributionARN(id string) string {
	return "arn:aws:cloudfront::000000000000:distribution/" + id
}

// distributionDomainName synthesises the user-visible CDN domain. AWS
// returns "<lower-id>.cloudfront.net"; cf-local uses ".cloudfront.local"
// so the value is obviously local-only and does not collide with real
// CloudFront DNS names if state files leak.
func distributionDomainName(id string) string {
	return strings.ToLower(id) + ".cloudfront.local"
}

// peekRootElement returns the local name of the first XML start element
// in body so the caller can dispatch on <DistributionConfig> vs
// <DistributionConfigWithTags> without first decoding the entire body.
func peekRootElement(body []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", fmt.Errorf("peek root: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local, nil
		}
	}
}
