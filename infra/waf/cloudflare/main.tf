# WAF gestionado en el borde (PP-F1-SEC-001, ADR-006): Cloudflare.
#
#   terraform -chdir=infra/waf/cloudflare init
#   terraform -chdir=infra/waf/cloudflare plan -var-file=staging.tfvars
#
# CLOUDFLARE_API_TOKEN por entorno (permisos: Zone.Settings, Zone.DNS,
# Zone.WAF, Zone.Transform Rules de la zona). El estado contiene el secreto de
# origen: backend remoto cifrado (RUNBOOK §6), nunca en el repo.
terraform {
  required_version = ">= 1.6"
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.52"
    }
  }
}

provider "cloudflare" {}

locals {
  hosts      = ["app", "api", "auth"]
  fqdns      = [for h in local.hosts : "${h}.${var.domain}"]
  host_set   = join(" ", [for f in local.fqdns : "\"${f}\""])
  team_ips   = join(" ", var.team_ip_ranges)
  auth_host  = "auth.${var.domain}"
  api_host   = "api.${var.domain}"
  log_only   = var.waf_mode == "log"
  managed_id = "efb7b8c949ac4650a09736fc376e9aee" # Cloudflare Managed Ruleset
  owasp_id   = "4814384a9e5d4991b9815dcfc25d2f1f" # Cloudflare OWASP Core Ruleset
  # Regla 949110 "Inbound Anomaly Score Exceeded": la que decide en OWASP.
  owasp_anomaly_rule = "6179ae15870a4bb7b2d480d4843b323c"
}

# --- DNS: app./api./auth. apuntan al WAF (proxied); el origen queda oculto ---
resource "cloudflare_record" "hosts" {
  for_each = toset(local.hosts)
  zone_id  = var.zone_id
  name     = "${each.key}.${var.domain}"
  type     = length(regexall(":", var.origin_ip)) > 0 ? "AAAA" : "A"
  content  = var.origin_ip
  proxied  = true
  ttl      = 1
  comment  = "PetPassport ${var.environment} (gestionado por Terraform)"
}

# --- TLS y cabeceras en el borde ---------------------------------------------
resource "cloudflare_zone_settings_override" "zone" {
  zone_id = var.zone_id
  settings {
    ssl                      = "strict" # Full (strict): certificado de origen validado
    min_tls_version          = "1.2"
    tls_1_3                  = "on"
    always_use_https         = "on"
    automatic_https_rewrites = "on"
    opportunistic_encryption = "on"
    browser_check            = "on"
    security_level           = "medium"
    security_header {
      enabled            = true
      max_age            = 63072000
      include_subdomains = true
      preload            = var.hsts_preload # se activa tras validar en staging
      nosniff            = true
    }
  }
}

# --- Reglas gestionadas: Cloudflare Managed + OWASP CRS (log → block) -------
resource "cloudflare_ruleset" "managed" {
  zone_id = var.zone_id
  name    = "petpassport-managed"
  kind    = "zone"
  phase   = "http_request_firewall_managed"

  rules {
    description = "Cloudflare Managed Ruleset (${var.waf_mode})"
    expression  = "(http.host in {${local.host_set}})"
    action      = "execute"
    enabled     = true
    action_parameters {
      id = local.managed_id
      dynamic "overrides" {
        for_each = local.log_only ? [1] : []
        content {
          action = "log"
        }
      }
    }
  }

  rules {
    description = "OWASP Core Ruleset PL${var.owasp_paranoia_level} (${var.waf_mode})"
    expression  = "(http.host in {${local.host_set}})"
    action      = "execute"
    enabled     = true
    action_parameters {
      id = local.owasp_id
      overrides {
        dynamic "categories" {
          for_each = [for pl in range(var.owasp_paranoia_level + 1, 5) : "paranoia-level-${pl}"]
          content {
            category = categories.value
            enabled  = false
          }
        }
        rules {
          id              = local.owasp_anomaly_rule
          action          = var.waf_mode
          score_threshold = var.owasp_score_threshold
        }
      }
    }
  }
}

# --- Reglas propias ------------------------------------------------------------
resource "cloudflare_ruleset" "custom" {
  zone_id = var.zone_id
  name    = "petpassport-custom"
  kind    = "zone"
  phase   = "http_request_firewall_custom"

  # Consola de Keycloak y realm master: solo IP/VPN del equipo (SEC-001).
  rules {
    description = "Keycloak admin/master solo desde el equipo"
    expression  = "(http.host eq \"${local.auth_host}\" and (starts_with(http.request.uri.path, \"/admin\") or starts_with(http.request.uri.path, \"/realms/master\")) and not ip.src in {${local.team_ips}})"
    action      = "block"
    enabled     = true
  }

  # En auth. solo se publican el realm de la app y los recursos estáticos.
  rules {
    description = "auth.: rutas no publicadas"
    expression  = "(http.host eq \"${local.auth_host}\" and not starts_with(http.request.uri.path, \"/realms/petpassport/\") and not starts_with(http.request.uri.path, \"/resources/\") and not starts_with(http.request.uri.path, \"/admin\") and not starts_with(http.request.uri.path, \"/realms/master\"))"
    action      = "block"
    enabled     = true
  }

  # En api. solo /api/* (SEC-002).
  rules {
    description = "api.: solo /api/*"
    expression  = "(http.host eq \"${local.api_host}\" and not starts_with(http.request.uri.path, \"/api/\"))"
    action      = "block"
    enabled     = true
  }
}

# --- Rate limiting en el borde (SEC-001) -------------------------------------
# El número de reglas depende del plan (Pro: 2). Las que no quepan se aplican
# igualmente en el origen (Traefik, infra/traefik/staging/routes.yml).
resource "cloudflare_ruleset" "ratelimit" {
  zone_id = var.zone_id
  name    = "petpassport-ratelimit"
  kind    = "zone"
  phase   = "http_ratelimit"

  dynamic "rules" {
    for_each = { for r in var.rate_limits : r.name => r }
    content {
      description = "${rules.value.name}: ${rules.value.requests}/${rules.value.period}s por IP"
      expression  = replace(rules.value.expression, "$${domain}", var.domain)
      action      = "block"
      enabled     = true
      ratelimit {
        characteristics     = ["ip.src", "cf.colo.id"]
        period              = rules.value.period
        requests_per_period = rules.value.requests
        mitigation_timeout  = rules.value.period
      }
      action_parameters {
        response {
          status_code  = 429
          content_type = "application/problem+json"
          content      = "{\"type\":\"about:blank\",\"title\":\"Too Many Requests\",\"status\":429,\"code\":\"RATE_LIMITED\"}"
        }
      }
    }
  }
}

# --- Cabecera secreta de origen (X-Origin-Auth) -------------------------------
# Traefik solo enruta peticiones que la traen; conexiones directas a la IP del
# origen reciben 404 (SEC-001). Se rota con el secreto de Traefik (RUNBOOK §5).
resource "cloudflare_ruleset" "origin_auth" {
  zone_id = var.zone_id
  name    = "petpassport-origin-auth"
  kind    = "zone"
  phase   = "http_request_late_transform"

  rules {
    description = "X-Origin-Auth hacia el origen"
    expression  = "(http.host in {${local.host_set}})"
    action      = "rewrite"
    enabled     = true
    action_parameters {
      headers {
        name      = "X-Origin-Auth"
        operation = "set"
        value     = var.origin_auth_secret
      }
    }
  }
}

# --- Autenticación del correo saliente (IAM-012) -----------------------------
# Solo se crean cuando el proveedor SMTP está configurado (mail_spf_includes).
# SPF: solo el proveedor envía por el dominio; -all rechaza el resto. DKIM: los
# registros que entrega el proveedor. DMARC: alineación estricta de SPF y DKIM.
resource "cloudflare_record" "mail_spf" {
  count   = length(var.mail_spf_includes) > 0 ? 1 : 0
  zone_id = var.zone_id
  name    = var.domain
  type    = "TXT"
  content = "\"v=spf1 ${join(" ", [for i in var.mail_spf_includes : "include:${i}"])} -all\""
  ttl     = 3600
  comment = "PetPassport ${var.environment} SPF (gestionado por Terraform)"
}

resource "cloudflare_record" "mail_dkim" {
  for_each = { for r in var.mail_dkim_records : r.name => r }
  zone_id  = var.zone_id
  name     = "${each.value.name}.${var.domain}"
  type     = each.value.type
  content  = each.value.content
  proxied  = false
  ttl      = 3600
  comment  = "PetPassport ${var.environment} DKIM (gestionado por Terraform)"
}

resource "cloudflare_record" "mail_dmarc" {
  count   = length(var.mail_spf_includes) > 0 ? 1 : 0
  zone_id = var.zone_id
  name    = "_dmarc.${var.domain}"
  type    = "TXT"
  content = "\"v=DMARC1; p=${var.mail_dmarc_policy}; adkim=s; aspf=s${var.mail_dmarc_rua != "" ? "; rua=${var.mail_dmarc_rua}" : ""}\""
  ttl     = 3600
  comment = "PetPassport ${var.environment} DMARC (gestionado por Terraform)"
}
