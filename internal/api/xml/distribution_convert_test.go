package awsxml

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// distributionRoundTripCases pair a wrapper-side DistributionConfig with the
// matching SDK-side types.DistributionConfig. Each case is exercised by:
//
//   - TestDistributionConfig_ToSDK         (wrapper → SDK)
//   - TestFromSDKDistributionConfig         (SDK → wrapper)
//   - TestDistributionConfig_RoundTripThroughSDK (wrapper → SDK → wrapper)
//
// The cases cover phase-4a's handler shapes: a phase-3-equivalent minimum,
// a Provider-shape with Aliases / ViewerCertificate / Restrictions, and a
// distribution carrying one extra CacheBehavior.
var distributionRoundTripCases = []struct {
	name    string
	wrapper *DistributionConfig
	sdk     *types.DistributionConfig
}{
	{
		name: "phase3-minimum-http-only",
		wrapper: &DistributionConfig{
			CallerReference: "phase3-min",
			Comment:         "minimum",
			Enabled:         true,
			OriginGroups:    &OriginGroups{Quantity: 0},
			Origins: &Origins{
				Quantity: 1,
				Items: OriginsItems{Origin: []Origin{
					{
						ID:         "o1",
						DomainName: "host.docker.internal",
						OriginPath: "",
						CustomOriginConfig: &CustomOriginConfig{
							HTTPPort:             3000,
							HTTPSPort:            443,
							OriginProtocolPolicy: "http-only",
							OriginSSLProtocols: OriginSSLProtocols{
								Quantity: 1,
								Items:    OriginSSLProtocolsItems{SSLProtocol: []string{"TLSv1.2"}},
							},
						},
					},
				}},
			},
			DefaultCacheBehavior: &DefaultCacheBehavior{
				TargetOriginID:       "o1",
				ViewerProtocolPolicy: "allow-all",
				CachePolicyID:        "658327ea-f89d-4fab-a63d-7e88639e58f6",
				Compress:             ptr(true),
				AllowedMethods: &AllowedMethods{
					Quantity: 2,
					Items:    AllowedMethodsItems{Method: []string{"GET", "HEAD"}},
					CachedMethods: &CachedMethods{
						Quantity: 2,
						Items:    CachedMethodsItems{Method: []string{"GET", "HEAD"}},
					},
				},
			},
		},
		sdk: &types.DistributionConfig{
			CallerReference:   aws.String("phase3-min"),
			Comment:           aws.String("minimum"),
			Enabled:           aws.Bool(true),
			DefaultRootObject: aws.String(""),
			WebACLId:          aws.String(""),
			OriginGroups:      &types.OriginGroups{Quantity: aws.Int32(0)},
			Origins: &types.Origins{
				Quantity: aws.Int32(1),
				Items: []types.Origin{
					{
						Id:         aws.String("o1"),
						DomainName: aws.String("host.docker.internal"),
						OriginPath: aws.String(""),
						CustomOriginConfig: &types.CustomOriginConfig{
							HTTPPort:             aws.Int32(3000),
							HTTPSPort:            aws.Int32(443),
							OriginProtocolPolicy: types.OriginProtocolPolicyHttpOnly,
							OriginSslProtocols: &types.OriginSslProtocols{
								Quantity: aws.Int32(1),
								Items:    []types.SslProtocol{types.SslProtocolTLSv12},
							},
						},
					},
				},
			},
			DefaultCacheBehavior: &types.DefaultCacheBehavior{
				TargetOriginId:       aws.String("o1"),
				ViewerProtocolPolicy: types.ViewerProtocolPolicyAllowAll,
				CachePolicyId:        aws.String("658327ea-f89d-4fab-a63d-7e88639e58f6"),
				Compress:             aws.Bool(true),
				AllowedMethods: &types.AllowedMethods{
					Quantity: aws.Int32(2),
					Items:    []types.Method{types.MethodGet, types.MethodHead},
					CachedMethods: &types.CachedMethods{
						Quantity: aws.Int32(2),
						Items:    []types.Method{types.MethodGet, types.MethodHead},
					},
				},
			},
		},
	},
	{
		name: "provider-shape-https-default-cert",
		wrapper: &DistributionConfig{
			CallerReference:   "tf-provider-1",
			Comment:           "tf-provider",
			Enabled:           true,
			HTTPVersion:       "http2",
			IsIPV6Enabled:     ptr(true),
			PriceClass:        "PriceClass_All",
			DefaultRootObject: "",
			Aliases:           &Aliases{Quantity: 0},
			OriginGroups:      &OriginGroups{Quantity: 0},
			Origins: &Origins{
				Quantity: 1,
				Items: OriginsItems{Origin: []Origin{
					{
						ID:         "o1",
						DomainName: "example.com",
						OriginPath: "",
						CustomOriginConfig: &CustomOriginConfig{
							HTTPPort:             80,
							HTTPSPort:            443,
							OriginProtocolPolicy: "http-only",
							OriginSSLProtocols: OriginSSLProtocols{
								Quantity: 1,
								Items:    OriginSSLProtocolsItems{SSLProtocol: []string{"TLSv1.2"}},
							},
						},
					},
				}},
			},
			DefaultCacheBehavior: &DefaultCacheBehavior{
				TargetOriginID:       "o1",
				ViewerProtocolPolicy: "redirect-to-https",
				CachePolicyID:        "658327ea-f89d-4fab-a63d-7e88639e58f6",
			},
			Logging: &LoggingConfig{Enabled: false, IncludeCookies: false, Bucket: "", Prefix: ""},
			ViewerCertificate: &ViewerCertificate{
				CloudFrontDefaultCertificate: ptr(true),
				MinimumProtocolVersion:       "TLSv1",
				CertificateSource:            "cloudfront",
			},
			Restrictions: &Restrictions{
				GeoRestriction: &GeoRestriction{RestrictionType: "none", Quantity: 0},
			},
		},
		sdk: &types.DistributionConfig{
			CallerReference:   aws.String("tf-provider-1"),
			Comment:           aws.String("tf-provider"),
			Enabled:           aws.Bool(true),
			HttpVersion:       types.HttpVersionHttp2,
			IsIPV6Enabled:     aws.Bool(true),
			PriceClass:        types.PriceClassPriceClassAll,
			DefaultRootObject: aws.String(""),
			WebACLId:          aws.String(""),
			Aliases:           &types.Aliases{Quantity: aws.Int32(0)},
			OriginGroups:      &types.OriginGroups{Quantity: aws.Int32(0)},
			Origins: &types.Origins{
				Quantity: aws.Int32(1),
				Items: []types.Origin{
					{
						Id:         aws.String("o1"),
						DomainName: aws.String("example.com"),
						OriginPath: aws.String(""),
						CustomOriginConfig: &types.CustomOriginConfig{
							HTTPPort:             aws.Int32(80),
							HTTPSPort:            aws.Int32(443),
							OriginProtocolPolicy: types.OriginProtocolPolicyHttpOnly,
							OriginSslProtocols: &types.OriginSslProtocols{
								Quantity: aws.Int32(1),
								Items:    []types.SslProtocol{types.SslProtocolTLSv12},
							},
						},
					},
				},
			},
			DefaultCacheBehavior: &types.DefaultCacheBehavior{
				TargetOriginId:       aws.String("o1"),
				ViewerProtocolPolicy: types.ViewerProtocolPolicyRedirectToHttps,
				CachePolicyId:        aws.String("658327ea-f89d-4fab-a63d-7e88639e58f6"),
			},
			Logging: &types.LoggingConfig{
				Enabled:        aws.Bool(false),
				IncludeCookies: aws.Bool(false),
				Bucket:         aws.String(""),
				Prefix:         aws.String(""),
			},
			ViewerCertificate: &types.ViewerCertificate{
				CloudFrontDefaultCertificate: aws.Bool(true),
				MinimumProtocolVersion:       types.MinimumProtocolVersionTLSv1,
				CertificateSource:            types.CertificateSourceCloudfront,
			},
			Restrictions: &types.Restrictions{
				GeoRestriction: &types.GeoRestriction{
					RestrictionType: types.GeoRestrictionTypeNone,
					Quantity:        aws.Int32(0),
				},
			},
		},
	},
	{
		name: "with-extra-cache-behavior",
		wrapper: &DistributionConfig{
			CallerReference: "behaviors-1",
			Comment:         "with-extra-behavior",
			Enabled:         true,
			OriginGroups:    &OriginGroups{Quantity: 0},
			Origins: &Origins{
				Quantity: 1,
				Items: OriginsItems{Origin: []Origin{
					{
						ID:         "o1",
						DomainName: "host.docker.internal",
						OriginPath: "",
						CustomOriginConfig: &CustomOriginConfig{
							HTTPPort:             3000,
							HTTPSPort:            443,
							OriginProtocolPolicy: "http-only",
							OriginSSLProtocols: OriginSSLProtocols{
								Quantity: 1,
								Items:    OriginSSLProtocolsItems{SSLProtocol: []string{"TLSv1.2"}},
							},
						},
					},
				}},
			},
			DefaultCacheBehavior: &DefaultCacheBehavior{
				TargetOriginID:       "o1",
				ViewerProtocolPolicy: "allow-all",
				CachePolicyID:        "658327ea-f89d-4fab-a63d-7e88639e58f6",
			},
			CacheBehaviors: &CacheBehaviors{
				Quantity: 1,
				Items: &CacheBehaviorsItems{CacheBehavior: []CacheBehavior{
					{
						PathPattern:          "/api/*",
						TargetOriginID:       "o1",
						ViewerProtocolPolicy: "allow-all",
						CachePolicyID:        "4135ea2d-6df8-44a3-9df3-4b5a84be39ad",
					},
				}},
			},
		},
		sdk: &types.DistributionConfig{
			CallerReference:   aws.String("behaviors-1"),
			Comment:           aws.String("with-extra-behavior"),
			Enabled:           aws.Bool(true),
			DefaultRootObject: aws.String(""),
			WebACLId:          aws.String(""),
			OriginGroups:      &types.OriginGroups{Quantity: aws.Int32(0)},
			Origins: &types.Origins{
				Quantity: aws.Int32(1),
				Items: []types.Origin{
					{
						Id:         aws.String("o1"),
						DomainName: aws.String("host.docker.internal"),
						OriginPath: aws.String(""),
						CustomOriginConfig: &types.CustomOriginConfig{
							HTTPPort:             aws.Int32(3000),
							HTTPSPort:            aws.Int32(443),
							OriginProtocolPolicy: types.OriginProtocolPolicyHttpOnly,
							OriginSslProtocols: &types.OriginSslProtocols{
								Quantity: aws.Int32(1),
								Items:    []types.SslProtocol{types.SslProtocolTLSv12},
							},
						},
					},
				},
			},
			DefaultCacheBehavior: &types.DefaultCacheBehavior{
				TargetOriginId:       aws.String("o1"),
				ViewerProtocolPolicy: types.ViewerProtocolPolicyAllowAll,
				CachePolicyId:        aws.String("658327ea-f89d-4fab-a63d-7e88639e58f6"),
			},
			CacheBehaviors: &types.CacheBehaviors{
				Quantity: aws.Int32(1),
				Items: []types.CacheBehavior{
					{
						PathPattern:          aws.String("/api/*"),
						TargetOriginId:       aws.String("o1"),
						ViewerProtocolPolicy: types.ViewerProtocolPolicyAllowAll,
						CachePolicyId:        aws.String("4135ea2d-6df8-44a3-9df3-4b5a84be39ad"),
					},
				},
			},
		},
	},
}

func TestDistributionConfig_ToSDK(t *testing.T) {
	for _, tt := range distributionRoundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.wrapper.ToSDK()
			if !reflect.DeepEqual(got, tt.sdk) {
				t.Errorf("ToSDK mismatch:\n got = %#v\nwant = %#v", got, tt.sdk)
			}
		})
	}
}

func TestFromSDKDistributionConfig(t *testing.T) {
	for _, tt := range distributionRoundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := FromSDKDistributionConfig(tt.sdk)
			if !reflect.DeepEqual(got, tt.wrapper) {
				t.Errorf("FromSDK mismatch:\n got = %#v\nwant = %#v", got, tt.wrapper)
			}
		})
	}
}

func TestDistributionConfig_RoundTripThroughSDK(t *testing.T) {
	for _, tt := range distributionRoundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := FromSDKDistributionConfig(tt.wrapper.ToSDK())
			if !reflect.DeepEqual(got, tt.wrapper) {
				t.Errorf("round-trip mismatch:\n got = %#v\nwant = %#v", got, tt.wrapper)
			}
		})
	}
}

// TestDistributionConfig_QuantityRecompute verifies that a wrapper with stale
// Quantity fields is not propagated — ToSDK recomputes Quantity from Items.
func TestDistributionConfig_QuantityRecompute(t *testing.T) {
	w := &DistributionConfig{
		CallerReference: "stale-quantity",
		Comment:         "stale",
		Enabled:         true,
		Origins: &Origins{
			Quantity: 99, // stale; ToSDK must recompute from len(Items.Origin)
			Items: OriginsItems{Origin: []Origin{
				{
					ID:         "o1",
					DomainName: "host.docker.internal",
					OriginPath: "",
					CustomOriginConfig: &CustomOriginConfig{
						HTTPPort:             80,
						HTTPSPort:            443,
						OriginProtocolPolicy: "http-only",
						OriginSSLProtocols: OriginSSLProtocols{
							Quantity: 99, // stale
							Items:    OriginSSLProtocolsItems{SSLProtocol: []string{"TLSv1.2"}},
						},
					},
				},
			}},
		},
		DefaultCacheBehavior: &DefaultCacheBehavior{
			TargetOriginID:       "o1",
			ViewerProtocolPolicy: "allow-all",
		},
	}
	got := w.ToSDK()
	if q := aws.ToInt32(got.Origins.Quantity); q != 1 {
		t.Errorf("Origins.Quantity not recomputed: got %d want 1", q)
	}
	if q := aws.ToInt32(got.Origins.Items[0].CustomOriginConfig.OriginSslProtocols.Quantity); q != 1 {
		t.Errorf("OriginSslProtocols.Quantity not recomputed: got %d want 1", q)
	}
}

func TestDistributionConfig_NilSafe(t *testing.T) {
	if got := (*DistributionConfig)(nil).ToSDK(); got != nil {
		t.Errorf("nil wrapper.ToSDK should return nil, got %#v", got)
	}
	if got := FromSDKDistributionConfig(nil); got != nil {
		t.Errorf("FromSDKDistributionConfig(nil) should return nil, got %#v", got)
	}
}
