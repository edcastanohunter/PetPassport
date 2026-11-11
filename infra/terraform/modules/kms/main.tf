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

# 1. KEK principal para PII envelope encryption (rotación anual automática)
resource "aws_kms_key" "kek" {
  description             = "PetPassport ${var.environment} Master Key (KEK) for PII Envelope Encryption"
  deletion_window_in_days = 30
  enable_key_rotation     = true

  tags = {
    Name        = "petpassport-${var.environment}-kek"
    Environment = var.environment
    Purpose     = "pii-envelope-encryption"
  }
}

resource "aws_kms_alias" "kek" {
  name          = "alias/petpassport-${var.environment}-kek"
  target_key_id = aws_kms_key.kek.key_id
}

# 2. Clave KMS separada para backup de subject_keys (DAT-006: nunca en el mismo artefacto ni con la misma clave)
resource "aws_kms_key" "subject_keys_backup" {
  description             = "PetPassport ${var.environment} KMS Key for Subject Keys Backup Encryption"
  deletion_window_in_days = 30
  enable_key_rotation     = true

  tags = {
    Name        = "petpassport-${var.environment}-subject-keys-backup"
    Environment = var.environment
    Purpose     = "subject-keys-backup-encryption"
  }
}

resource "aws_kms_alias" "subject_keys_backup" {
  name          = "alias/petpassport-${var.environment}-subject-keys-backup"
  target_key_id = aws_kms_key.subject_keys_backup.key_id
}

# 3. Clave KMS para base de datos y almacenamiento
resource "aws_kms_key" "storage" {
  description             = "PetPassport ${var.environment} KMS Key for RDS and S3 at-rest encryption"
  deletion_window_in_days = 30
  enable_key_rotation     = true

  tags = {
    Name        = "petpassport-${var.environment}-storage-kms"
    Environment = var.environment
    Purpose     = "storage-encryption"
  }
}

resource "aws_kms_alias" "storage" {
  name          = "alias/petpassport-${var.environment}-storage"
  target_key_id = aws_kms_key.storage.key_id
}

output "kek_arn" {
  value = aws_kms_key.kek.arn
}

output "kek_id" {
  value = aws_kms_key.kek.key_id
}

output "kek_alias" {
  value = aws_kms_alias.kek.name
}

output "subject_keys_backup_key_arn" {
  value = aws_kms_key.subject_keys_backup.arn
}

output "storage_key_arn" {
  value = aws_kms_key.storage.arn
}
