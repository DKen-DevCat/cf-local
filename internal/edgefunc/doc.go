// Package edgefunc implements the cf-local edge-proxy logic for invoking
// AWS Lambda@Edge viewer-request handlers locally via the AWS Lambda
// Runtime Interface Emulator (RIE).
//
// Phase 4-D viewer-request MVP (U-2 confirmed). The package is consumed by
// `cmd/edge-proxy` (sidecar binary) and provides:
//
//   - server.go:    HTTP /invoke handler that njs (`ngx.fetch`) calls
//   - event.go:     CloudFront viewer-request event construction (AWS docs
//                   schema)
//   - rie_client.go HTTP client for POST /2015-03-31/functions/function/
//                   invocations
//   - lookup.go:    distribution → LambdaFunctionAssociations resolution via
//                   the cf-local control-plane internal API
//
// Companion code on the cf-local side lives at
// `internal/api/edgefunc/handler.go` (the internal lookup endpoint that
// edge-proxy calls). Both paths share the JSON wire types defined here so
// the two binaries cannot drift out of step.
package edgefunc
