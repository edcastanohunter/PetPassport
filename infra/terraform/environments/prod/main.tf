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
    bucket         = "petpassport-prod-tfstate"
    key            = "prod/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "petpassport-prod-tflocks"
    encrypt        = true
  }
}

provider "aws" {
  region = var.aws_region
  default_tags {
    tags = {
      Environment = "prod"
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
  default = "petpassport.app"
}

variable "cloudflare_zone_id" {
  type        = string
  description = "Cloudflare Zone ID for production"
}

# 1. Redes multi-AZ (3 Availability Zones para producción)
module "vpc" {
  source             = "../../modules/vpc"
  environment        = "prod"
  vpc_cidr           = "10.0.0.0/16"
  availability_zones = ["us-east-1a", "us-east-1b", "us-east-1c"]
  cluster_name       = "petpassport-prod-eks"
}

# 2. Claves KMS con rotación automática anual
module "kms" {
  source      = "../../modules/kms"
  environment = "prod"
}

# 3. Almacenamiento, Object Lock 10 años y réplica Cross-Region
module "storage" {
  source                   = "../../modules/storage"
  environment              = "prod"
  kms_storage_key_arn      = module.kms.storage_key_arn
  kms_subject_keys_key_arn = module.kms.subject_keys_backup_key_arn
  secondary_region         = "us-west-2"
}

# 4. EKS Clúster gestionado multi-zona
module "eks" {
  source             = "../../modules/eks"
  environment        = "prod"
  vpc_id             = module.vpc.vpc_id
  private_subnet_ids = module.vpc.private_subnet_ids
  kubernetes_version = "1.31"
}

# 5. Base de datos PostgreSQL multi-AZ con réplica síncrona y failover < 60s
module "rds" {
  source                     = "../../modules/rds"
  environment                = "prod"
  vpc_id                     = module.vpc.vpc_id
  db_subnet_group_name       = module.vpc.db_subnet_group_name
  kms_storage_key_arn        = module.kms.storage_key_arn
  allowed_security_group_ids = [module.eks.nodes_security_group_id]
  instance_class             = "db.r6g.xlarge"
  allocated_storage          = 150
}

# 6. Redis multi-AZ con réplica y failover automático
module "redis" {
  source                     = "../../modules/redis"
  environment                = "prod"
  vpc_id                     = module.vpc.vpc_id
  database_subnet_ids        = module.vpc.database_subnet_ids
  allowed_security_group_ids = [module.eks.nodes_security_group_id]
  node_type                  = "cache.m6g.xlarge"
}

# 7. OpenSearch multi-AZ con 2 nodos en distintas zonas y réplica
module "opensearch" {
  source                     = "../../modules/opensearch"
  environment                = "prod"
  vpc_id                     = module.vpc.vpc_id
  private_subnet_ids         = module.vpc.private_subnet_ids
  allowed_security_group_ids = [module.eks.nodes_security_group_id]
  kms_storage_key_arn        = module.kms.storage_key_arn
}

# 8. DNS & WAF
module "dns_waf" {
  source               = "../../modules/dns_waf"
  environment          = "prod"
  zone_id              = var.cloudflare_zone_id
  domain               = var.domain
  origin_ingress_cname = "k8s-ingress.petpassport.app"
}
