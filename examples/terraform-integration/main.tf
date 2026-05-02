// main.tf: minimal Terraform integration to exercise cf-local's AWS API
// surface end-to-end. Phase 4-A 4a-16 verifies that `terraform apply` and
// `terraform destroy` complete without unexpected API calls.
//
// Adds incrementally: cache policy first (smallest), then origin request
// policy, then distribution. Distribution references the managed
// `Managed-CachingOptimized` policy ID seeded by cf-local on startup.

resource "aws_cloudfront_cache_policy" "smoke" {
  name        = "tf-e2e-smoke-cp"
  comment     = "phase-4a 4a-16 smoke"
  default_ttl = 3600
  max_ttl     = 86400
  min_ttl     = 1

  parameters_in_cache_key_and_forwarded_to_origin {
    enable_accept_encoding_brotli = true
    enable_accept_encoding_gzip   = true

    cookies_config {
      cookie_behavior = "none"
    }
    headers_config {
      header_behavior = "none"
    }
    query_strings_config {
      query_string_behavior = "none"
    }
  }
}

resource "aws_cloudfront_origin_request_policy" "smoke" {
  name    = "tf-e2e-smoke-orp"
  comment = "phase-4a 4a-16 smoke"

  cookies_config {
    cookie_behavior = "none"
  }
  headers_config {
    header_behavior = "whitelist"
    headers {
      items = ["X-Forwarded-For", "User-Agent"]
    }
  }
  query_strings_config {
    query_string_behavior = "all"
  }
}

resource "aws_cloudfront_distribution" "smoke" {
  comment             = "phase-4a 4a-16 smoke"
  enabled             = true
  is_ipv6_enabled     = true
  http_version        = "http2"
  price_class         = "PriceClass_All"
  default_root_object = ""

  origin {
    origin_id   = "tf-e2e-origin-1"
    domain_name = "host.docker.internal"

    custom_origin_config {
      http_port              = 3000
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    target_origin_id         = "tf-e2e-origin-1"
    viewer_protocol_policy   = "allow-all"
    cache_policy_id          = aws_cloudfront_cache_policy.smoke.id
    origin_request_policy_id = aws_cloudfront_origin_request_policy.smoke.id
    allowed_methods          = ["GET", "HEAD"]
    cached_methods           = ["GET", "HEAD"]
    compress                 = true
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  // Wait_for_deployment=false skips the post-create poll until Status="Deployed";
  // cf-local always returns Status="Deployed" so this is a no-op safety toggle.
  wait_for_deployment = false
}

output "cache_policy_id" {
  value = aws_cloudfront_cache_policy.smoke.id
}

output "origin_request_policy_id" {
  value = aws_cloudfront_origin_request_policy.smoke.id
}

output "distribution_id" {
  value = aws_cloudfront_distribution.smoke.id
}

output "distribution_domain_name" {
  value = aws_cloudfront_distribution.smoke.domain_name
}
