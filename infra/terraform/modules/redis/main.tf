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

variable "database_subnet_ids" {
  type        = list(string)
  description = "Subnet IDs for Redis subnet group"
}

variable "allowed_security_group_ids" {
  type        = list(string)
  default     = []
  description = "Security groups allowed to connect to Redis"
}

variable "node_type" {
  type        = string
  default     = "cache.m6g.large"
  description = "ElastiCache node type"
}

resource "aws_security_group" "redis" {
  name        = "petpassport-${var.environment}-redis-sg"
  description = "Security group for PetPassport Redis cluster"
  vpc_id      = var.vpc_id

  ingress {
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = var.allowed_security_group_ids
    description     = "Allow Redis from EKS pods"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = {
    Name        = "petpassport-${var.environment}-redis-sg"
    Environment = var.environment
  }
}

resource "aws_elasticache_subnet_group" "redis" {
  name       = "petpassport-${var.environment}-redis-subnet-group"
  subnet_ids = var.database_subnet_ids

  tags = {
    Name        = "petpassport-${var.environment}-redis-subnet-group"
    Environment = var.environment
  }
}

resource "aws_elasticache_replication_group" "redis" {
  replication_group_id       = "pp-${var.environment}-redis"
  description                = "PetPassport ${var.environment} Redis Replication Group Multi-AZ"
  node_type                  = var.node_type
  port                       = 6379
  parameter_group_name       = "default.redis7"
  subnet_group_name          = aws_elasticache_subnet_group.redis.name
  security_group_ids         = [aws_security_group.redis.id]
  automatic_failover_enabled = true # Multi-AZ automatic failover (OPS-002, ADR-024)
  multi_az_enabled           = true
  num_cache_clusters         = 2 # Primary + 1 replica in another AZ
  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  auto_minor_version_upgrade = false

  tags = {
    Name        = "petpassport-${var.environment}-redis"
    Environment = var.environment
    HighAvail   = "Multi-AZ"
  }
}

output "primary_endpoint_address" {
  value = aws_elasticache_replication_group.redis.primary_endpoint_address
}

output "reader_endpoint_address" {
  value = aws_elasticache_replication_group.redis.reader_endpoint_address
}

output "port" {
  value = aws_elasticache_replication_group.redis.port
}

output "security_group_id" {
  value = aws_security_group.redis.id
}
