data "googlesiteverification_domain_key" "this" {
  site_identifier     = "www.example.com."
  site_type           = "INET_DOMAIN"
  verification_method = "DNS_TXT"
}