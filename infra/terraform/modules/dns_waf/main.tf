terraform {
  required_version = ">= 1.5.0"
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.0"
    }
  }
}

variable "environment" {
  type        = string
  description = "Environment name (staging, prod)"
}

variable "zone_id" {
  type        = string
  description = "Cloudflare Zone ID"
}

variable "domain" {
  type        = string
  description = "Domain name (e.g. petpassport.app or staging.petpassport.app)"
}

variable "origin_ingress_cname" {
  type        = string
  description = "Origin NLB / ALB hostname from EKS Ingress Controller"
}

# DNS Records
resource "cloudflare_record" "app" {
  zone_id = var.zone_id
  name    = "app"
  value   = var.origin_ingress_cname
  type    = "CNAME"
  proxied = true
  ttl     = 1 # Auto when proxied
}

resource "cloudflare_record" "api" {
  zone_id = var.zone_id
  name    = "api"
  value   = var.origin_ingress_cname
  type    = "CNAME"
  proxied = true
  ttl     = 1
}

resource "cloudflare_record" "auth" {
  zone_id = var.zone_id
  name    = "auth"
  value   = var.origin_ingress_cname
  type    = "CNAME"
  proxied = true
  ttl     = 1
}

resource "cloudflare_record" "partners" {
  zone_id = var.zone_id
  name    = "partners"
  value   = var.origin_ingress_cname
  type    = "CNAME"
  proxied = true
  ttl     = 1
}

# WAF and Security Settings
resource "cloudflare_zone_settings_override" "security" {
  zone_id = var.zone_id

  settings {
    ssl                      = "strict"
    always_use_https         = "on"
    min_tls_version          = "1.2"
    security_level           = "medium"
    brotli                   = "on"
    websockets               = "on"
    http3                    = "on"
    tls_1_3                  = "on"
  }
}

output "app_url" {
  value = "https://app.${var.domain}"
}

output "api_url" {
  value = "https://api.${var.domain}"
}
