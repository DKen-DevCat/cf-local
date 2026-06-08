terraform {
  required_version = ">= 1.9.0, < 2.0.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.50.0, < 6.0.0"
    }
  }
}

provider "aws" {
  region                      = "us-east-1"
  access_key                  = "dummy"
  secret_key                  = "dummy"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    cloudfront = "http://localhost:4566"
  }
}

resource "aws_cloudfront_cache_policy" "lambda_edge_full" {
  name        = "lambda-edge-full-default"
  comment     = "cf-local Lambda@Edge phase-4e example"
  default_ttl = 60
  max_ttl     = 3600
  min_ttl     = 0

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
      query_string_behavior = "all"
    }
  }
}

resource "aws_cloudfront_distribution" "lambda_edge_full" {
  comment         = "cf-local Lambda@Edge phase-4e full example"
  enabled         = true
  is_ipv6_enabled = true
  http_version    = "http2"
  price_class     = "PriceClass_All"

  origin {
    origin_id   = "echo-origin"
    domain_name = "origin"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    target_origin_id       = "echo-origin"
    viewer_protocol_policy = "allow-all"
    cache_policy_id        = aws_cloudfront_cache_policy.lambda_edge_full.id
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    compress               = true

    lambda_function_association {
      event_type   = "viewer-request"
      lambda_arn   = "arn:aws:lambda:us-east-1:000000000000:function:auth:1"
      include_body = false
    }

    lambda_function_association {
      event_type   = "origin-request"
      lambda_arn   = "arn:aws:lambda:us-east-1:000000000000:function:origin-rewrite:1"
      include_body = false
    }

    lambda_function_association {
      event_type   = "origin-response"
      lambda_arn   = "arn:aws:lambda:us-east-1:000000000000:function:origin-response:1"
      include_body = false
    }

    lambda_function_association {
      event_type   = "viewer-response"
      lambda_arn   = "arn:aws:lambda:us-east-1:000000000000:function:viewer-response:1"
      include_body = false
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  wait_for_deployment = false
}

output "distribution_id" {
  value = aws_cloudfront_distribution.lambda_edge_full.id
}

output "distribution_domain_name" {
  value = aws_cloudfront_distribution.lambda_edge_full.domain_name
}
