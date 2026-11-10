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

variable "private_subnet_ids" {
  type        = list(string)
  description = "Private subnet IDs across zones"
}

variable "allowed_security_group_ids" {
  type        = list(string)
  default     = []
  description = "Security groups allowed to connect to OpenSearch"
}

variable "kms_storage_key_arn" {
  type        = string
  description = "KMS storage key ARN"
}

resource "aws_security_group" "opensearch" {
  name        = "petpassport-${var.environment}-opensearch-sg"
  description = "Security group for PetPassport OpenSearch domain"
  vpc_id      = var.vpc_id

  ingress {
    from_port       = 443
    to_port         = 443
    protocol        = "tcp"
    security_groups = var.allowed_security_group_ids
    description     = "Allow HTTPS from EKS pods"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = {
    Name        = "petpassport-${var.environment}-opensearch-sg"
    Environment = var.environment
  }
}

resource "aws_opensearch_domain" "search" {
  domain_name    = "petpassport-${var.environment}-search"
  engine_version = "OpenSearch_2.17"

  cluster_config {
    instance_type          = "m6g.large.search"
    instance_count         = 2 # 2 nodos en zonas distintas con réplica por índice (OPS-002, ADR-024)
    zone_awareness_enabled = true

    zone_awareness_config {
      availability_zone_count = 2
    }
  }

  ebs_options {
    ebs_enabled = true
    volume_type = "gp3"
    volume_size = 50
  }

  encrypt_at_rest {
    enabled    = true
    kms_key_id = var.kms_storage_key_arn
  }

  node_to_node_encryption {
    enabled = true
  }

  domain_endpoint_options {
    enforce_https       = true
    tls_security_policy = "Policy-Min-TLS-1-2-2019-07"
  }

  vpc_options {
    subnet_ids         = slice(var.private_subnet_ids, 0, 2)
    security_group_ids = [aws_security_group.opensearch.id]
  }

  tags = {
    Name        = "petpassport-${var.environment}-search"
    Environment = var.environment
    HighAvail   = "Multi-AZ"
  }
}

output "endpoint" {
  value = aws_opensearch_domain.search.endpoint
}

output "security_group_id" {
  value = aws_security_group.opensearch.id
}
