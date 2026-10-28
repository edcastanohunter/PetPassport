variable "environment" {
  description = "Nombre del entorno (staging | prod)."
  type        = string
  validation {
    condition     = contains(["staging", "prod"], var.environment)
    error_message = "environment debe ser staging o prod."
  }
}

variable "zone_id" {
  description = "ID de la zona de Cloudflare."
  type        = string
}

variable "domain" {
  description = "Dominio base del entorno; se publican app., api. y auth. bajo él (p. ej. staging.petpassport.example)."
  type        = string
}

variable "origin_ip" {
  description = "IP pública de la VM de origen (solo acepta tráfico de Cloudflare)."
  type        = string
}

variable "origin_auth_secret" {
  description = "Valor de X-Origin-Auth; el mismo que PP_ORIGIN_AUTH_SECRET en la VM. Desde el secret manager."
  type        = string
  sensitive   = true
  validation {
    condition     = length(var.origin_auth_secret) >= 32
    error_message = "origin_auth_secret debe tener al menos 32 caracteres."
  }
}

variable "team_ip_ranges" {
  description = "Rangos CIDR del equipo/VPN con acceso a la consola de Keycloak."
  type        = list(string)
  validation {
    condition     = length(var.team_ip_ranges) > 0
    error_message = "Debe haber al menos un rango del equipo."
  }
}

variable "waf_mode" {
  description = "log durante la primera semana en staging; block después (SEC-001)."
  type        = string
  default     = "log"
  validation {
    condition     = contains(["log", "block"], var.waf_mode)
    error_message = "waf_mode debe ser log o block."
  }
}

variable "owasp_paranoia_level" {
  description = "Nivel de paranoia del OWASP CRS (1–4)."
  type        = number
  default     = 1
}

variable "owasp_score_threshold" {
  description = "Umbral de anomalía OWASP (25 alto, 40 medio, 60 bajo)."
  type        = number
  default     = 40
}

variable "hsts_preload" {
  description = "Activar preload de HSTS (solo tras validar en staging)."
  type        = bool
  default     = false
}

variable "rate_limits" {
  description = <<-EOT
    Reglas de rate limiting del borde (SEC-001). `$${domain}` se sustituye por
    var.domain. Por defecto las dos de autenticación (caben en el plan Pro);
    con Business o superior se añaden las de la API (ver staging.tfvars.example).
  EOT
  type = list(object({
    name       = string
    expression = string
    requests   = number
    period     = number
  }))
  default = [
    {
      name       = "login-token"
      expression = "(http.host eq \"auth.$${domain}\" and http.request.method eq \"POST\" and (starts_with(http.request.uri.path, \"/realms/petpassport/login-actions/\") or http.request.uri.path eq \"/realms/petpassport/protocol/openid-connect/token\"))"
      requests   = 10
      period     = 60
    },
    {
      name       = "org-lookup"
      expression = "(http.host eq \"api.$${domain}\" and http.request.uri.path eq \"/api/v1/organizations/lookup\")"
      requests   = 20
      period     = 60
    },
  ]
}

# --- Correo (IAM-012): autenticación del remitente no-reply@<domain> ----------
variable "mail_spf_includes" {
  description = "Mecanismos include: del proveedor SMTP para el SPF de var.domain (p. ej. [\"amazonses.com\"])."
  type        = list(string)
  default     = []
}

variable "mail_dkim_records" {
  description = <<-EOT
    Registros DKIM que entrega el proveedor SMTP al verificar el dominio:
    name relativo a var.domain (p. ej. "s1._domainkey"), type CNAME o TXT y valor.
  EOT
  type = list(object({
    name    = string
    type    = string
    content = string
  }))
  default = []
}

variable "mail_dmarc_policy" {
  description = "Política DMARC: none (observación), quarantine o reject. Staging empieza en quarantine."
  type        = string
  default     = "quarantine"
  validation {
    condition     = contains(["none", "quarantine", "reject"], var.mail_dmarc_policy)
    error_message = "mail_dmarc_policy debe ser none, quarantine o reject."
  }
}

variable "mail_dmarc_rua" {
  description = "Buzón de informes agregados DMARC (mailto:...). Vacío = sin informes."
  type        = string
  default     = ""
}
