package awsxml

import (
	"encoding/xml"
	"strings"
	"testing"
)

// TestDistributionConfig_Unmarshal_ProviderShape parses a DistributionConfig
// shape resembling what the Terraform AWS Provider emits for a minimal
// aws_cloudfront_distribution. Faithful decode means the handler can hand
// the struct to ToSDK without losing fields.
func TestDistributionConfig_Unmarshal_ProviderShape(t *testing.T) {
	const sample = `<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <CallerReference>terraform-20260502123456789012</CallerReference>
  <Aliases>
    <Quantity>0</Quantity>
  </Aliases>
  <DefaultRootObject></DefaultRootObject>
  <Origins>
    <Quantity>1</Quantity>
    <Items>
      <Origin>
        <Id>tf-origin-1</Id>
        <DomainName>example.com</DomainName>
        <OriginPath></OriginPath>
        <CustomHeaders>
          <Quantity>0</Quantity>
        </CustomHeaders>
        <CustomOriginConfig>
          <HTTPPort>80</HTTPPort>
          <HTTPSPort>443</HTTPSPort>
          <OriginProtocolPolicy>http-only</OriginProtocolPolicy>
          <OriginSslProtocols>
            <Quantity>2</Quantity>
            <Items>
              <SslProtocol>TLSv1.1</SslProtocol>
              <SslProtocol>TLSv1.2</SslProtocol>
            </Items>
          </OriginSslProtocols>
          <OriginReadTimeout>30</OriginReadTimeout>
          <OriginKeepaliveTimeout>5</OriginKeepaliveTimeout>
        </CustomOriginConfig>
        <ConnectionAttempts>3</ConnectionAttempts>
        <ConnectionTimeout>10</ConnectionTimeout>
      </Origin>
    </Items>
  </Origins>
  <OriginGroups>
    <Quantity>0</Quantity>
  </OriginGroups>
  <DefaultCacheBehavior>
    <TargetOriginId>tf-origin-1</TargetOriginId>
    <ViewerProtocolPolicy>allow-all</ViewerProtocolPolicy>
    <CachePolicyId>658327ea-f89d-4fab-a63d-7e88639e58f6</CachePolicyId>
    <AllowedMethods>
      <Quantity>2</Quantity>
      <Items>
        <Method>GET</Method>
        <Method>HEAD</Method>
      </Items>
      <CachedMethods>
        <Quantity>2</Quantity>
        <Items>
          <Method>GET</Method>
          <Method>HEAD</Method>
        </Items>
      </CachedMethods>
    </AllowedMethods>
    <SmoothStreaming>false</SmoothStreaming>
    <Compress>true</Compress>
    <FieldLevelEncryptionId></FieldLevelEncryptionId>
    <FunctionAssociations>
      <Quantity>0</Quantity>
    </FunctionAssociations>
    <LambdaFunctionAssociations>
      <Quantity>0</Quantity>
    </LambdaFunctionAssociations>
    <TrustedKeyGroups>
      <Enabled>false</Enabled>
      <Quantity>0</Quantity>
    </TrustedKeyGroups>
    <TrustedSigners>
      <Enabled>false</Enabled>
      <Quantity>0</Quantity>
    </TrustedSigners>
  </DefaultCacheBehavior>
  <CacheBehaviors>
    <Quantity>0</Quantity>
  </CacheBehaviors>
  <CustomErrorResponses>
    <Quantity>0</Quantity>
  </CustomErrorResponses>
  <Comment>spike distribution</Comment>
  <Logging>
    <Enabled>false</Enabled>
    <IncludeCookies>false</IncludeCookies>
    <Bucket></Bucket>
    <Prefix></Prefix>
  </Logging>
  <PriceClass>PriceClass_All</PriceClass>
  <Enabled>true</Enabled>
  <ViewerCertificate>
    <CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate>
    <MinimumProtocolVersion>TLSv1</MinimumProtocolVersion>
    <CertificateSource>cloudfront</CertificateSource>
  </ViewerCertificate>
  <Restrictions>
    <GeoRestriction>
      <RestrictionType>none</RestrictionType>
      <Quantity>0</Quantity>
    </GeoRestriction>
  </Restrictions>
  <WebACLId></WebACLId>
  <HttpVersion>http2</HttpVersion>
  <IsIPV6Enabled>true</IsIPV6Enabled>
  <Staging>false</Staging>
</DistributionConfig>`

	var got DistributionConfig
	if err := xml.Unmarshal([]byte(sample), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Spike X1: namespace landed on XMLName.Space.
	if got.XMLName.Space != XMLNSCloudFront {
		t.Errorf("XMLName.Space: got %q want %q", got.XMLName.Space, XMLNSCloudFront)
	}

	if got.CallerReference != "terraform-20260502123456789012" {
		t.Errorf("CallerReference: got %q", got.CallerReference)
	}
	if !got.Enabled {
		t.Errorf("Enabled: got false")
	}
	if got.HTTPVersion != "http2" {
		t.Errorf("HttpVersion: got %q", got.HTTPVersion)
	}
	if got.IsIPV6Enabled == nil || !*got.IsIPV6Enabled {
		t.Errorf("IsIPV6Enabled: got %v want *true", got.IsIPV6Enabled)
	}
	if got.PriceClass != "PriceClass_All" {
		t.Errorf("PriceClass: got %q", got.PriceClass)
	}

	// Origins / one custom-http origin.
	if got.Origins == nil || got.Origins.Quantity != 1 || len(got.Origins.Items.Origin) != 1 {
		t.Fatalf("Origins shape: %+v", got.Origins)
	}
	o := got.Origins.Items.Origin[0]
	if o.ID != "tf-origin-1" || o.DomainName != "example.com" {
		t.Errorf("Origin id/domain: %+v", o)
	}
	if o.CustomOriginConfig == nil || o.CustomOriginConfig.HTTPPort != 80 ||
		o.CustomOriginConfig.OriginProtocolPolicy != "http-only" {
		t.Errorf("CustomOriginConfig: %+v", o.CustomOriginConfig)
	}
	if got, want := o.CustomOriginConfig.OriginSSLProtocols.Items.SSLProtocol,
		[]string{"TLSv1.1", "TLSv1.2"}; !equalStrings(got, want) {
		t.Errorf("OriginSslProtocols: got %v want %v", got, want)
	}

	// DefaultCacheBehavior + AllowedMethods + CachedMethods nesting.
	d := got.DefaultCacheBehavior
	if d == nil {
		t.Fatal("DefaultCacheBehavior: nil")
	}
	if d.TargetOriginID != "tf-origin-1" || d.ViewerProtocolPolicy != "allow-all" {
		t.Errorf("DefaultCacheBehavior basics: %+v", d)
	}
	if d.CachePolicyID != "658327ea-f89d-4fab-a63d-7e88639e58f6" {
		t.Errorf("CachePolicyId: got %q", d.CachePolicyID)
	}
	if d.AllowedMethods == nil || d.AllowedMethods.Quantity != 2 {
		t.Fatalf("AllowedMethods: %+v", d.AllowedMethods)
	}
	if got, want := d.AllowedMethods.Items.Method, []string{"GET", "HEAD"}; !equalStrings(got, want) {
		t.Errorf("AllowedMethods.Items: got %v want %v", got, want)
	}
	if d.AllowedMethods.CachedMethods == nil || d.AllowedMethods.CachedMethods.Quantity != 2 {
		t.Errorf("CachedMethods: %+v", d.AllowedMethods.CachedMethods)
	}

	// ViewerCertificate / Restrictions / empty Quantity-only blocks.
	if got.ViewerCertificate == nil || got.ViewerCertificate.CloudFrontDefaultCertificate == nil ||
		!*got.ViewerCertificate.CloudFrontDefaultCertificate {
		t.Errorf("ViewerCertificate.CloudFrontDefaultCertificate: %+v", got.ViewerCertificate)
	}
	if got.Restrictions == nil || got.Restrictions.GeoRestriction == nil ||
		got.Restrictions.GeoRestriction.RestrictionType != "none" {
		t.Errorf("GeoRestriction: %+v", got.Restrictions)
	}
	if got.CacheBehaviors == nil || got.CacheBehaviors.Quantity != 0 {
		t.Errorf("CacheBehaviors empty: %+v", got.CacheBehaviors)
	}
	if got.Aliases == nil || got.Aliases.Quantity != 0 {
		t.Errorf("Aliases empty: %+v", got.Aliases)
	}
	if got.OriginGroups == nil || got.OriginGroups.Quantity != 0 {
		t.Errorf("OriginGroups empty: %+v", got.OriginGroups)
	}
	if got.CustomErrorResponses == nil || got.CustomErrorResponses.Quantity != 0 {
		t.Errorf("CustomErrorResponses empty: %+v", got.CustomErrorResponses)
	}
}

// TestDistributionConfig_MarshalRoundTrip rebuilds a DistributionConfig
// programmatically and confirms the wire output carries the AWS-required
// structural cues (xmlns on root, proper Items+Quantity nesting, omitempty
// for absent optional fields).
func TestDistributionConfig_MarshalRoundTrip(t *testing.T) {
	compress := true
	enabled := true
	httpsAlways := false
	cfg := DistributionConfig{
		CallerReference: "tf-mini-2026",
		Comment:         "minimal",
		Enabled:         enabled,
		IsIPV6Enabled:   &httpsAlways,
		HTTPVersion:     "http2",
		PriceClass:      "PriceClass_All",
		Aliases:         &Aliases{Quantity: 0},
		Origins: &Origins{
			Quantity: 1,
			Items: OriginsItems{
				Origin: []Origin{
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
				},
			},
		},
		DefaultCacheBehavior: &DefaultCacheBehavior{
			TargetOriginID:       "o1",
			ViewerProtocolPolicy: "allow-all",
			CachePolicyID:        "658327ea-f89d-4fab-a63d-7e88639e58f6",
			Compress:             &compress,
			AllowedMethods: &AllowedMethods{
				Quantity: 2,
				Items:    AllowedMethodsItems{Method: []string{"GET", "HEAD"}},
				CachedMethods: &CachedMethods{
					Quantity: 2,
					Items:    CachedMethodsItems{Method: []string{"GET", "HEAD"}},
				},
			},
		},
		CacheBehaviors:       &CacheBehaviors{Quantity: 0},
		CustomErrorResponses: &CustomErrorResponses{Quantity: 0},
		OriginGroups:         &OriginGroups{Quantity: 0},
		Logging:              &LoggingConfig{Enabled: false},
		ViewerCertificate: &ViewerCertificate{
			CloudFrontDefaultCertificate: ptr(true),
			MinimumProtocolVersion:       "TLSv1",
			CertificateSource:            "cloudfront",
		},
		Restrictions: &Restrictions{
			GeoRestriction: &GeoRestriction{RestrictionType: "none", Quantity: 0},
		},
	}

	out, err := xml.MarshalIndent(&cfg, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	outStr := string(out)

	mustContain(t, outStr, `<DistributionConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">`)
	mustContain(t, outStr, `<CallerReference>tf-mini-2026</CallerReference>`)
	mustContain(t, outStr, `<Origin>`)
	mustContain(t, outStr, `<DomainName>host.docker.internal</DomainName>`)
	mustContain(t, outStr, `<HTTPPort>3000</HTTPPort>`)
	mustContain(t, outStr, `<CachePolicyId>658327ea-f89d-4fab-a63d-7e88639e58f6</CachePolicyId>`)
	mustContain(t, outStr, `<RestrictionType>none</RestrictionType>`)
	// Quantity-only empty blocks survive marshal as <Block><Quantity>0</Quantity></Block>.
	mustContain(t, outStr, `<CacheBehaviors>`)
	mustContain(t, outStr, `<CustomErrorResponses>`)

	// Round-trip back through Unmarshal preserves the wire-critical fields.
	var rt DistributionConfig
	if err := xml.Unmarshal(out, &rt); err != nil {
		t.Fatalf("Re-unmarshal: %v\n--- output ---\n%s", err, outStr)
	}
	if rt.CallerReference != cfg.CallerReference {
		t.Errorf("round-trip CallerReference: got %q want %q", rt.CallerReference, cfg.CallerReference)
	}
	if rt.DefaultCacheBehavior == nil || rt.DefaultCacheBehavior.CachePolicyID != cfg.DefaultCacheBehavior.CachePolicyID {
		t.Errorf("round-trip CachePolicyId: %+v", rt.DefaultCacheBehavior)
	}
	if rt.Origins == nil || rt.Origins.Items.Origin[0].CustomOriginConfig.HTTPPort != 3000 {
		t.Errorf("round-trip Origin HTTPPort: %+v", rt.Origins)
	}
}

// TestDistribution_ResponseWrapper exercises the response envelope used by
// GetDistribution / CreateDistribution / UpdateDistribution.
func TestDistribution_ResponseWrapper(t *testing.T) {
	cfgInner := &DistributionConfig{
		CallerReference: "resp-1",
		Comment:         "resp",
		Enabled:         true,
		Origins:         &Origins{Quantity: 0},
		DefaultCacheBehavior: &DefaultCacheBehavior{
			TargetOriginID:       "o1",
			ViewerProtocolPolicy: "allow-all",
		},
	}
	d := Distribution{
		ID:                            "EXXEXAMPLE12",
		ARN:                           "arn:aws:cloudfront::000000000000:distribution/EXXEXAMPLE12",
		Status:                        "Deployed",
		LastModifiedTime:              "2026-05-02T00:00:00Z",
		InProgressInvalidationBatches: 0,
		DomainName:                    "exxexample12.cloudfront.local",
		ActiveTrustedSigners:          &ActiveTrustedSigners{Enabled: false, Quantity: 0},
		ActiveTrustedKeyGroups:        &ActiveTrustedKeyGroups{Enabled: false, Quantity: 0},
		AliasICPRecordals:             &AliasICPRecordals{Quantity: 0},
		DistributionConfig:            cfgInner,
	}

	out, err := xml.MarshalIndent(&d, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	outStr := string(out)
	mustContain(t, outStr, `<Distribution>`)
	mustContain(t, outStr, `<Id>EXXEXAMPLE12</Id>`)
	mustContain(t, outStr, `<Status>Deployed</Status>`)
	// Inner DistributionConfig carries xmlns; outer Distribution stays bare.
	mustContain(t, outStr, `<DistributionConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">`)
	if strings.Contains(outStr, `<Distribution xmlns=`) {
		t.Errorf("outer <Distribution> should not carry xmlns:\n%s", outStr)
	}

	var got Distribution
	if err := xml.Unmarshal(out, &got); err != nil {
		t.Fatalf("Re-unmarshal: %v", err)
	}
	if got.ID != "EXXEXAMPLE12" || got.Status != "Deployed" {
		t.Errorf("round-trip Id/Status: %+v", got)
	}
	if got.DistributionConfig == nil || got.DistributionConfig.XMLName.Space != XMLNSCloudFront {
		t.Errorf("round-trip inner xmlns: %+v", got.DistributionConfig)
	}
}
