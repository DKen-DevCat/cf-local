// Package tagging implements the CloudFront resource-tagging API endpoints
// as no-ops. cf-local does not track resource tags (out of scope for
// phase-4a), but the Terraform AWS Provider always calls ListTagsForResource
// after Create / before Update on every CloudFront resource. Returning 200
// with an empty <Tags><Items /></Tags> envelope satisfies the Provider's
// tagging round-trip.
//
// AWS endpoints:
//
//	GET    /2020-05-31/tagging?Resource=<ARN>           ListTagsForResource
//	POST   /2020-05-31/tagging?Operation=Tag&Resource=  TagResource
//	POST   /2020-05-31/tagging?Operation=Untag&Resource= UntagResource
package tagging

import (
	"encoding/xml"
	"net/http"

	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
)

// Handler is the stub tagging handler. Stateless — cf-local does not
// persist tags so there is no Store dependency.
type Handler struct{}

// Get handles GET /2020-05-31/tagging?Resource=<ARN>. Returns an empty
// <Tags><Items /></Tags> envelope regardless of the resource queried.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("Resource") == "" {
		awsxml.WriteError(w, awsxml.CodeInvalidArgument, "Resource query parameter is required")
		return
	}
	resp := tagsResponse{Items: &tagsItems{}}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	// See cachepolicy/handler.go for why the encode error is ignored.
	_ = enc.Encode(&resp)
	_ = enc.Close()
}

// Post handles both TagResource and UntagResource based on the Operation
// query parameter. Returns 204 No Content for any well-formed request;
// cf-local intentionally drops the tag payload.
func (h *Handler) Post(w http.ResponseWriter, r *http.Request) {
	op := r.URL.Query().Get("Operation")
	switch op {
	case "Tag", "Untag":
		w.WriteHeader(http.StatusNoContent)
	default:
		awsxml.WriteError(w, awsxml.CodeInvalidArgument,
			"Operation query parameter must be Tag or Untag")
	}
}

// tagsResponse is the wire shape of the ListTagsForResource response.
// Defined here (not awsxml) to keep awsxml focused on shared types; tags
// are only used by this stub and the Distribution decoder.
type tagsResponse struct {
	XMLName xml.Name   `xml:"http://cloudfront.amazonaws.com/doc/2020-05-31/ Tags"`
	Items   *tagsItems `xml:"Items"`
}

type tagsItems struct{}
