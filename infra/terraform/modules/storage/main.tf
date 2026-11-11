terraform {
  required_version = ">= 1.5.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

variable "environment" {
  type        = string
  description = "Environment name (staging, prod)"
}

variable "kms_storage_key_arn" {
  type        = string
  description = "KMS Key ARN for storage encryption"
}

variable "kms_subject_keys_key_arn" {
  type        = string
  description = "KMS Key ARN dedicated for subject keys backups"
}

variable "secondary_region" {
  type        = string
  default     = "us-west-2"
  description = "Secondary AWS region for Cross-Region Replication (CRR)"
}

# 1. Bucket principal: documentos, recetas, fotos (versionado, cifrado)
resource "aws_s3_bucket" "primary_data" {
  bucket        = "petpassport-${var.environment}-data"
  force_destroy = var.environment == "staging" ? true : false

  tags = {
    Name        = "petpassport-${var.environment}-data"
    Environment = var.environment
  }
}

resource "aws_s3_bucket_versioning" "primary_data" {
  bucket = aws_s3_bucket.primary_data.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "primary_data" {
  bucket = aws_s3_bucket.primary_data.id

  rule {
    apply_server_side_encryption_by_default {
      kms_master_key_id = var.kms_storage_key_arn
      sse_algorithm     = "aws:kms"
    }
  }
}

# 2. Bucket con Object Lock para facturas y evidencias fiscales (retención 10 años, RN-34 / CMP-009)
resource "aws_s3_bucket" "compliance_archive" {
  bucket              = "petpassport-${var.environment}-compliance-archive"
  object_lock_enabled = true
  force_destroy       = false

  tags = {
    Name        = "petpassport-${var.environment}-compliance-archive"
    Environment = var.environment
    Compliance  = "DIAN-10yr-immutable"
  }
}

resource "aws_s3_bucket_versioning" "compliance_archive" {
  bucket = aws_s3_bucket.compliance_archive.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_object_lock_configuration" "compliance_archive" {
  bucket = aws_s3_bucket.compliance_archive.id

  rule {
    default_retention {
      mode  = "COMPLIANCE"
      years = 10
    }
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "compliance_archive" {
  bucket = aws_s3_bucket.compliance_archive.id

  rule {
    apply_server_side_encryption_by_default {
      kms_master_key_id = var.kms_storage_key_arn
      sse_algorithm     = "aws:kms"
    }
  }
}

# 3. Bucket exclusivo para backups separados de subject_keys (DAT-006: NUNCA en el mismo bucket que los datos)
resource "aws_s3_bucket" "subject_keys_backup" {
  bucket        = "petpassport-${var.environment}-subject-keys-backup"
  force_destroy = false

  tags = {
    Name        = "petpassport-${var.environment}-subject-keys-backup"
    Environment = var.environment
    Sensitivity = "Highest"
  }
}

resource "aws_s3_bucket_versioning" "subject_keys_backup" {
  bucket = aws_s3_bucket.subject_keys_backup.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "subject_keys_backup" {
  bucket = aws_s3_bucket.subject_keys_backup.id

  rule {
    apply_server_side_encryption_by_default {
      kms_master_key_id = var.kms_subject_keys_key_arn
      sse_algorithm     = "aws:kms"
    }
  }
}

# 4. Bucket de backups de Event Store con copia Cross-Region
resource "aws_s3_bucket" "events_backup" {
  bucket        = "petpassport-${var.environment}-events-backup"
  force_destroy = var.environment == "staging" ? true : false

  tags = {
    Name        = "petpassport-${var.environment}-events-backup"
    Environment = var.environment
    Purpose     = "dr-backups"
  }
}

resource "aws_s3_bucket_versioning" "events_backup" {
  bucket = aws_s3_bucket.events_backup.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "events_backup" {
  bucket = aws_s3_bucket.events_backup.id

  rule {
    id     = "retention-30d"
    status = "Enabled"

    expiration {
      days = 30
    }

    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }
}

# Bloqueo total de acceso público en todos los buckets
resource "aws_s3_bucket_public_access_block" "pab_primary" {
  bucket                  = aws_s3_bucket.primary_data.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_public_access_block" "pab_compliance" {
  bucket                  = aws_s3_bucket.compliance_archive.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_public_access_block" "pab_subject_keys" {
  bucket                  = aws_s3_bucket.subject_keys_backup.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_public_access_block" "pab_events" {
  bucket                  = aws_s3_bucket.events_backup.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

output "primary_data_bucket" {
  value = aws_s3_bucket.primary_data.id
}

output "compliance_archive_bucket" {
  value = aws_s3_bucket.compliance_archive.id
}

output "subject_keys_backup_bucket" {
  value = aws_s3_bucket.subject_keys_backup.id
}

output "events_backup_bucket" {
  value = aws_s3_bucket.events_backup.id
}
