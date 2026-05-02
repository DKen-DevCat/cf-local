package cachepolicy

import (
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// managedLastModified is the LastModifiedTime baked into every managed
// policy record. Managed policies are immutable so a fixed past timestamp is
// fine — the 2020-05-31 API version date is used as a memorable sentinel.
var managedLastModified = time.Date(2020, 5, 31, 0, 0, 0, 0, time.UTC)

// ManagedPolicies returns the AWS-published built-in cache policies that
// CloudFront ships out of the box. cf-local seeds these into the store at
// startup so Distributions can reference them by Id without prior
// CreateCachePolicy calls.
//
// IDs and settings come from the public AWS docs, fetched 2026-05-02:
// https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-cache-policies.html
//
// 4a-9 covers the five canonical built-ins (CachingOptimized,
// CachingDisabled, CachingOptimizedForUncompressedObjects,
// Elemental-MediaPackage, Amplify). The Amplify-* sub-policies and
// UseOriginCacheControlHeaders* are out of scope for phase-4a.
func ManagedPolicies() []*Record {
	return []*Record{
		managedCachingOptimized(),
		managedCachingDisabled(),
		managedCachingOptimizedForUncompressedObjects(),
		managedElementalMediaPackage(),
		managedAmplify(),
	}
}

func managedCachingOptimized() *Record {
	return managedRecord(
		"658327ea-f89d-4fab-a63d-7e88639e58f6",
		&types.CachePolicyConfig{
			Comment:    aws.String("Policy with caching enabled. Supports Gzip and Brotli compression."),
			DefaultTTL: aws.Int64(86400),
			MaxTTL:     aws.Int64(31536000),
			MinTTL:     aws.Int64(1),
			Name:       aws.String("Managed-CachingOptimized"),
			ParametersInCacheKeyAndForwardedToOrigin: &types.ParametersInCacheKeyAndForwardedToOrigin{
				EnableAcceptEncodingBrotli: aws.Bool(true),
				EnableAcceptEncodingGzip:   aws.Bool(true),
				CookiesConfig:              &types.CachePolicyCookiesConfig{CookieBehavior: types.CachePolicyCookieBehaviorNone},
				HeadersConfig:              &types.CachePolicyHeadersConfig{HeaderBehavior: types.CachePolicyHeaderBehaviorNone},
				QueryStringsConfig:         &types.CachePolicyQueryStringsConfig{QueryStringBehavior: types.CachePolicyQueryStringBehaviorNone},
			},
		},
	)
}

func managedCachingDisabled() *Record {
	return managedRecord(
		"4135ea2d-6df8-44a3-9df3-4b5a84be39ad",
		&types.CachePolicyConfig{
			Comment:    aws.String("Policy with caching disabled."),
			DefaultTTL: aws.Int64(0),
			MaxTTL:     aws.Int64(0),
			MinTTL:     aws.Int64(0),
			Name:       aws.String("Managed-CachingDisabled"),
			ParametersInCacheKeyAndForwardedToOrigin: &types.ParametersInCacheKeyAndForwardedToOrigin{
				EnableAcceptEncodingBrotli: aws.Bool(false),
				EnableAcceptEncodingGzip:   aws.Bool(false),
				CookiesConfig:              &types.CachePolicyCookiesConfig{CookieBehavior: types.CachePolicyCookieBehaviorNone},
				HeadersConfig:              &types.CachePolicyHeadersConfig{HeaderBehavior: types.CachePolicyHeaderBehaviorNone},
				QueryStringsConfig:         &types.CachePolicyQueryStringsConfig{QueryStringBehavior: types.CachePolicyQueryStringBehaviorNone},
			},
		},
	)
}

func managedCachingOptimizedForUncompressedObjects() *Record {
	return managedRecord(
		"b2884449-e4de-46a7-ac36-70bc7f1ddd6d",
		&types.CachePolicyConfig{
			Comment:    aws.String("Policy with caching enabled. Does not support Gzip or Brotli compression."),
			DefaultTTL: aws.Int64(86400),
			MaxTTL:     aws.Int64(31536000),
			MinTTL:     aws.Int64(1),
			Name:       aws.String("Managed-CachingOptimizedForUncompressedObjects"),
			ParametersInCacheKeyAndForwardedToOrigin: &types.ParametersInCacheKeyAndForwardedToOrigin{
				EnableAcceptEncodingBrotli: aws.Bool(false),
				EnableAcceptEncodingGzip:   aws.Bool(false),
				CookiesConfig:              &types.CachePolicyCookiesConfig{CookieBehavior: types.CachePolicyCookieBehaviorNone},
				HeadersConfig:              &types.CachePolicyHeadersConfig{HeaderBehavior: types.CachePolicyHeaderBehaviorNone},
				QueryStringsConfig:         &types.CachePolicyQueryStringsConfig{QueryStringBehavior: types.CachePolicyQueryStringBehaviorNone},
			},
		},
	)
}

func managedElementalMediaPackage() *Record {
	return managedRecord(
		"08627262-05a9-4f76-9ded-b50ca2e3a84f",
		&types.CachePolicyConfig{
			Comment:    aws.String("Policy for AWS Elemental MediaPackage origins."),
			DefaultTTL: aws.Int64(86400),
			MaxTTL:     aws.Int64(31536000),
			MinTTL:     aws.Int64(0),
			Name:       aws.String("Managed-Elemental-MediaPackage"),
			ParametersInCacheKeyAndForwardedToOrigin: &types.ParametersInCacheKeyAndForwardedToOrigin{
				EnableAcceptEncodingBrotli: aws.Bool(false),
				EnableAcceptEncodingGzip:   aws.Bool(true),
				CookiesConfig:              &types.CachePolicyCookiesConfig{CookieBehavior: types.CachePolicyCookieBehaviorNone},
				HeadersConfig: &types.CachePolicyHeadersConfig{
					HeaderBehavior: types.CachePolicyHeaderBehaviorWhitelist,
					Headers: &types.Headers{
						Quantity: aws.Int32(1),
						Items:    []string{"Origin"},
					},
				},
				QueryStringsConfig: &types.CachePolicyQueryStringsConfig{
					QueryStringBehavior: types.CachePolicyQueryStringBehaviorWhitelist,
					QueryStrings: &types.QueryStringNames{
						Quantity: aws.Int32(4),
						Items:    []string{"aws.manifestfilter", "start", "end", "m"},
					},
				},
			},
		},
	)
}

func managedAmplify() *Record {
	return managedRecord(
		"2e54312d-136d-493c-8eb9-b001f22f67d2",
		&types.CachePolicyConfig{
			Comment:    aws.String("Policy for AWS Amplify web app origins."),
			DefaultTTL: aws.Int64(2),
			MaxTTL:     aws.Int64(600),
			MinTTL:     aws.Int64(2),
			Name:       aws.String("Managed-Amplify"),
			ParametersInCacheKeyAndForwardedToOrigin: &types.ParametersInCacheKeyAndForwardedToOrigin{
				EnableAcceptEncodingBrotli: aws.Bool(true),
				EnableAcceptEncodingGzip:   aws.Bool(true),
				CookiesConfig:              &types.CachePolicyCookiesConfig{CookieBehavior: types.CachePolicyCookieBehaviorAll},
				HeadersConfig: &types.CachePolicyHeadersConfig{
					HeaderBehavior: types.CachePolicyHeaderBehaviorWhitelist,
					Headers: &types.Headers{
						Quantity: aws.Int32(3),
						Items:    []string{"Authorization", "CloudFront-Viewer-Country", "Host"},
					},
				},
				QueryStringsConfig: &types.CachePolicyQueryStringsConfig{
					QueryStringBehavior: types.CachePolicyQueryStringBehaviorAll,
				},
			},
		},
	)
}

// managedRecord builds a Record with Type=managed and the fixed
// LastModifiedTime sentinel. The ETag is derived from the config so it
// stays stable across cf-local restarts.
func managedRecord(id string, cfg *types.CachePolicyConfig) *Record {
	return &Record{
		ID:               id,
		ETag:             cachePolicyETag(cfg),
		Config:           cfg,
		LastModifiedTime: managedLastModified,
		Type:             TypeManaged,
	}
}
