terraform {
  required_version = ">= 1.5.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.0"
    }
  }

  backend "s3" {
    bucket         = "petpassport-staging-tfstate"
    key            = "staging/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "petpassport-staging-tflocks"
    encrypt        = true
  }
}

provider "aws" {
  region = var.aws_region
  default_tags {
    tags = {
      Environment = "staging"
      Project     = "PetPassport"
      ManagedBy   = "Terraform"
    }
  }
}

variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "domain" {
  type    = string
  default = "staging.petpassport.app"
}

variable "cloudflare_zone_id" {
  type        = string
  description = "Cloudflare Zone ID for staging"
}

# 1. Redes multi-AZ
module "vpc" {
  source             = "../../modules/vpc"
  environment        = "staging"
  vpc_cidr           = "10.10.0.0/16"
  availability_zones = ["us-east-1a", "us-east-1b"]
  cluster_name       = "petpassport-staging-eks"
}

# 2. Claves KMS
module "kms" {
  source      = "../../modules/kms"
  environment = "staging"
}

# 3. Almacenamiento y Object Lock
module "storage" {
  source                   = "../../modules/storage"
  environment              = "staging"
  kms_storage_key_arn      = module.kms.storage_key_arn
  kms_subject_keys_key_arn = module.kms.subject_keys_backup_key_arn
}

# 4. EKS Clúster gestionado
module "eks" {
  source             = "../../modules/eks"
  environment        = "staging"
  vpc_id             = module.vpc.vpc_id
  private_subnet_ids = module.vpc.private_subnet_ids
  kubernetes_version = "1.31"
}

# 5. Base de datos PostgreSQL multi-AZ
module "rds" {
  source                     = "../../modules/rds"
  environment                = "staging"
  vpc_id                     = module.vpc.vpc_id
  db_subnet_group_name       = module.vpc.db_subnet_group_name
  kms_storage_key_arn        = module.kms.storage_key_arn
  allowed_security_group_ids = [module.eks.nodes_security_group_id]
  instance_class             = "db.r6g.large"
  allocated_storage          = 50
}

# 6. Redis multi-AZ
module "redis" {
  source                     = "../../modules/redis"
  environment                = "staging"
  vpc_id                     = module.vpc.vpc_id
  database_subnet_ids        = module.vpc.database_subnet_ids
  allowed_security_group_ids = [module.eks.nodes_security_group_id]
  node_type                  = "cache.m6g.large"
}

# 7. OpenSearch multi-AZ
module "opensearch" {
  source                     = "../../modules/opensearch"
  environment                = "staging"
  vpc_id                     = module.vpc.vpc_id
  private_subnet_ids         = module.vpc.private_subnet_ids
  allowed_security_group_ids = [module.eks.nodes_security_group_id]
  kms_storage_key_arn        = module.kms.storage_key_arn
}

# 8. DNS & WAF
module "dns_waf" {
  source               = "../../modules/dns_waf"
  environment          = "staging"
  zone_id              = var.cloudflare_zone_id
  domain               = var.domain
  origin_ingress_cname = "k8s-ingress.staging.petpassport.app"
}
