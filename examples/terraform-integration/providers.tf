// providers.tf: AWS Provider configured to talk to cf-local instead of real
// AWS. The endpoints block redirects every CloudFront API call to the
// control-plane URL exposed by cf-local. cf-local does not verify SigV4
// signatures, so the dummy access keys are tolerated.
//
// CF_LOCAL_ENDPOINT default :14566 matches the smoke-test address used in
// the cf-local commit log; switch to :4566 for the docker-compose default.

variable "cf_local_endpoint" {
  description = "Control-plane endpoint URL where cf-local listens (e.g. http://localhost:14566)."
  type        = string
  default     = "http://localhost:14566"
}

provider "aws" {
  region                      = "us-east-1"
  access_key                  = "dummy"
  secret_key                  = "dummy"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    cloudfront = var.cf_local_endpoint
  }
}
