package edgefunc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/DKen-DevCat/cf-local/internal/api/distribution"
	"github.com/DKen-DevCat/cf-local/internal/edgefunc"
)

func TestHandler_Get_ReturnsLambdaFunctionAssociations(t *testing.T) {
	store := distribution.NewMemoryStore()
	cfg := &types.DistributionConfig{
		CallerReference: aws.String("ref-1"),
		Origins: &types.Origins{
			Quantity: aws.Int32(1),
			Items: []types.Origin{
				{Id: aws.String("o1"), DomainName: aws.String("origin.local")},
			},
		},
		DefaultCacheBehavior: &types.DefaultCacheBehavior{
			TargetOriginId: aws.String("o1"),
			LambdaFunctionAssociations: &types.LambdaFunctionAssociations{
				Quantity: aws.Int32(1),
				Items: []types.LambdaFunctionAssociation{
					{
						EventType:         types.EventTypeViewerRequest,
						LambdaFunctionARN: aws.String("arn:aws:lambda:us-east-1:000000000000:function:auth:1"),
						IncludeBody:       aws.Bool(false),
					},
				},
			},
		},
	}
	rec, err := store.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	h := &Handler{
		Store: store,
		FunctionEndpoints: map[string]string{
			"auth": "http://lambda-auth:8080",
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_internal/edge-functions/{id}", h.Get)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/_internal/edge-functions/" + rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out edgefunc.LookupResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.DistributionID != rec.ID {
		t.Errorf("distribution_id mismatch: got %q want %q", out.DistributionID, rec.ID)
	}
	if len(out.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(out.Functions))
	}
	got := out.Functions[0]
	if got.EventType != edgefunc.EventViewerRequest {
		t.Errorf("event_type=%s", got.EventType)
	}
	if got.RIEEndpoint != "http://lambda-auth:8080" {
		t.Errorf("rie_endpoint=%q", got.RIEEndpoint)
	}
	if got.FunctionARN != "arn:aws:lambda:us-east-1:000000000000:function:auth:1" {
		t.Errorf("function_arn=%q", got.FunctionARN)
	}
}

func TestHandler_Get_NotFound(t *testing.T) {
	store := distribution.NewMemoryStore()
	h := &Handler{Store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_internal/edge-functions/{id}", h.Get)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/_internal/edge-functions/missing")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandler_Get_NoBindings_ReturnsEmptyArray(t *testing.T) {
	store := distribution.NewMemoryStore()
	cfg := &types.DistributionConfig{
		CallerReference: aws.String("ref-empty"),
		Origins: &types.Origins{
			Quantity: aws.Int32(1),
			Items:    []types.Origin{{Id: aws.String("o1"), DomainName: aws.String("o.local")}},
		},
		DefaultCacheBehavior: &types.DefaultCacheBehavior{TargetOriginId: aws.String("o1")},
	}
	rec, err := store.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	h := &Handler{Store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_internal/edge-functions/{id}", h.Get)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/_internal/edge-functions/" + rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	var out edgefunc.LookupResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Functions == nil {
		t.Error("expected non-nil empty array, got nil")
	}
	if len(out.Functions) != 0 {
		t.Errorf("expected 0 functions, got %d", len(out.Functions))
	}
}

func TestHandler_Get_MissingMapping_ReturnsEmptyEndpoint(t *testing.T) {
	store := distribution.NewMemoryStore()
	cfg := &types.DistributionConfig{
		CallerReference: aws.String("ref-no-map"),
		Origins: &types.Origins{
			Quantity: aws.Int32(1),
			Items:    []types.Origin{{Id: aws.String("o1"), DomainName: aws.String("o.local")}},
		},
		DefaultCacheBehavior: &types.DefaultCacheBehavior{
			TargetOriginId: aws.String("o1"),
			LambdaFunctionAssociations: &types.LambdaFunctionAssociations{
				Quantity: aws.Int32(1),
				Items: []types.LambdaFunctionAssociation{
					{
						EventType:         types.EventTypeViewerRequest,
						LambdaFunctionARN: aws.String("arn:aws:lambda:us-east-1:0:function:unmapped:1"),
					},
				},
			},
		},
	}
	rec, err := store.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	h := &Handler{Store: store, FunctionEndpoints: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_internal/edge-functions/{id}", h.Get)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/_internal/edge-functions/" + rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var out edgefunc.LookupResult
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(out.Functions))
	}
	if out.Functions[0].RIEEndpoint != "" {
		t.Errorf("expected empty RIE endpoint when unmapped, got %q", out.Functions[0].RIEEndpoint)
	}
}

func TestParseFunctionEndpoints(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{"empty", "", map[string]string{}},
		{"whitespace only", "   \n  \t", map[string]string{}},
		{"single line", "auth=lambda-auth:8080", map[string]string{"auth": "http://lambda-auth:8080"}},
		{"multi line", "auth=lambda-auth:8080\nrewrite=lambda-rewrite:8080", map[string]string{
			"auth":    "http://lambda-auth:8080",
			"rewrite": "http://lambda-rewrite:8080",
		}},
		{"comma separated", "auth=lambda-auth:8080,rewrite=lambda-rewrite:8080", map[string]string{
			"auth":    "http://lambda-auth:8080",
			"rewrite": "http://lambda-rewrite:8080",
		}},
		{"semicolon separated", "auth=lambda-auth:8080; rewrite=lambda-rewrite:8080", map[string]string{
			"auth":    "http://lambda-auth:8080",
			"rewrite": "http://lambda-rewrite:8080",
		}},
		{"explicit scheme preserved", "auth=https://lambda.example.com", map[string]string{
			"auth": "https://lambda.example.com",
		}},
		{"comments and blanks skipped", "# header\n\nauth=lambda-auth:8080\n# trailing", map[string]string{
			"auth": "http://lambda-auth:8080",
		}},
		{"malformed lines ignored", "garbage\nauth=lambda-auth:8080\n=novalue\nname=", map[string]string{
			"auth": "http://lambda-auth:8080",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFunctionEndpoints(tt.raw)
			if len(got) != len(tt.want) {
				t.Errorf("len mismatch: got %d want %d (%v)", len(got), len(tt.want), got)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("entry %q: got %q want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestResolveRIEEndpoint(t *testing.T) {
	endpoints := map[string]string{"auth": "http://x"}
	tests := []struct {
		name string
		arn  string
		want string
	}{
		{"valid ARN", "arn:aws:lambda:us-east-1:0:function:auth:1", "http://x"},
		{"no version", "arn:aws:lambda:us-east-1:0:function:auth", "http://x"},
		{"unmapped name", "arn:aws:lambda:us-east-1:0:function:other", ""},
		{"missing function segment", "arn:aws:lambda:us-east-1:0:layer:auth:1", ""},
		{"empty arn", "", ""},
		{"too few segments", "arn:aws:lambda", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveRIEEndpoint(tt.arn, endpoints); got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}
