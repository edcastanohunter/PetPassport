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

variable "vpc_id" {
  type        = string
  description = "VPC ID"
}

variable "db_subnet_group_name" {
  type        = string
  description = "DB Subnet Group name"
}

variable "kms_storage_key_arn" {
  type        = string
  description = "KMS Key ARN for storage encryption"
}

variable "allowed_security_group_ids" {
  type        = list(string)
  default     = []
  description = "Security groups allowed to connect to RDS"
}

variable "instance_class" {
  type        = string
  default     = "db.r6g.xlarge"
  description = "RDS instance class (r6g.xlarge for prod multi-AZ)"
}

variable "allocated_storage" {
  type        = number
  default     = 100
  description = "Allocated storage in GB"
}

resource "aws_security_group" "rds" {
  name        = "petpassport-${var.environment}-rds-sg"
  description = "Security group for PetPassport PostgreSQL RDS"
  vpc_id      = var.vpc_id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = var.allowed_security_group_ids
    description     = "Allow inbound PostgreSQL from EKS pods and pooler"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = {
    Name        = "petpassport-${var.environment}-rds-sg"
    Environment = var.environment
  }
}

resource "aws_db_parameter_group" "pg17" {
  name   = "petpassport-${var.environment}-pg17-params"
  family = "postgres17"

  parameter {
    name  = "shared_preload_libraries"
    value = "pg_stat_statements"
  }

  parameter {
    name  = "rds.logical_replication"
    value = "1"
  }

  parameter {
    name  = "track_io_timing"
    value = "1"
  }

  parameter {
    name  = "idle_in_transaction_session_timeout"
    value = "60000" # 60s timeout
  }

  tags = {
    Name        = "petpassport-${var.environment}-pg17-params"
    Environment = var.environment
  }
}

resource "aws_db_instance" "postgres" {
  identifier                  = "petpassport-${var.environment}-postgres"
  engine                      = "postgres"
  engine_version              = "17.2"
  instance_class              = var.instance_class
  allocated_storage           = var.allocated_storage
  max_allocated_storage       = 500
  storage_type                = "gp3"
  storage_encrypted           = true
  kms_key_id                  = var.kms_storage_key_arn
  multi_az                    = true # Multi-AZ para alta disponibilidad y failover automático < 60s (OPS-002, ADR-024)
  db_subnet_group_name        = var.db_subnet_group_name
  vpc_security_group_ids      = [aws_security_group.rds.id]
  parameter_group_name        = aws_db_parameter_group.pg17.name
  auto_minor_version_upgrade  = false
  allow_major_version_upgrade = false

  db_name  = "petpassport"
  username = "pp_admin"
  password = "CHANGE_ME_IN_SECRET_STORE" # Manejado vía External Secrets / Secret Manager

  backup_retention_period   = 30 # 30 días de retención PITR continuo (OPS-003, ADR-024)
  backup_window             = "03:00-04:00"
  maintenance_window        = "Sun:04:30-Sun:05:30"
  copy_tags_to_snapshot     = true
  deletion_protection       = var.environment == "prod" ? true : false
  skip_final_snapshot       = var.environment == "staging" ? true : false
  final_snapshot_identifier = "petpassport-${var.environment}-postgres-final"

  enabled_cloudwatch_logs_exports = ["postgresql", "upgrade"]

  tags = {
    Name        = "petpassport-${var.environment}-postgres"
    Environment = var.environment
    HighAvail   = "Multi-AZ"
  }
}

output "endpoint" {
  value = aws_db_instance.postgres.endpoint
}

output "address" {
  value = aws_db_instance.postgres.address
}

output "port" {
  value = aws_db_instance.postgres.port
}

output "security_group_id" {
  value = aws_security_group.rds.id
}
