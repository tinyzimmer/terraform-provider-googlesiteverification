data "googlesiteverification_domain_key" "this" {
  site_identifier     = "www.example.com."
  site_type           = "INET_DOMAIN"
  verification_method = "DNS_TXT"
}

resource "googlesiteverification_site_verification" "this" {
  token               = data.googlesiteverification_domain_key.this.token
  site_identifier     = data.googlesiteverification_domain_key.this.site_identifier
  site_type           = data.googlesiteverification_domain_key.this.site_type
  verification_method = data.googlesiteverification_domain_key.this.verification_method
  managed_zone        = "my-managed-zone"
}