terraform {
  required_version = ">= 1.5.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
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

variable "kubernetes_version" {
  type        = string
  default     = "1.31"
  description = "Kubernetes version"
}

# 1. IAM Role para el Cluster EKS
resource "aws_iam_role" "cluster" {
  name = "petpassport-${var.environment}-eks-cluster-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "eks.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "cluster_policy" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
  role       = aws_iam_role.cluster.name
}

resource "aws_iam_role_policy_attachment" "cluster_vpc_resource_controller" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSVPCResourceController"
  role       = aws_iam_role.cluster.name
}

# Security Group para EKS Pods / Nodes
resource "aws_security_group" "nodes" {
  name        = "petpassport-${var.environment}-eks-nodes-sg"
  description = "Security group for PetPassport EKS worker nodes"
  vpc_id      = var.vpc_id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name        = "petpassport-${var.environment}-eks-nodes-sg"
    Environment = var.environment
  }
}

# 2. Clúster EKS gestionado multi-zona (OPS-001, ADR-024)
resource "aws_eks_cluster" "main" {
  name     = "petpassport-${var.environment}-eks"
  version  = var.kubernetes_version
  role_arn = aws_iam_role.cluster.arn

  vpc_config {
    subnet_ids              = var.private_subnet_ids
    endpoint_private_access = true
    endpoint_public_access  = true # Acceso restringido por CIDR o VPN
    security_group_ids      = [aws_security_group.nodes.id]
  }

  enabled_cluster_log_types = ["api", "audit", "authenticator", "controllerManager", "scheduler"]

  depends_on = [
    aws_iam_role_policy_attachment.cluster_policy,
    aws_iam_role_policy_attachment.cluster_vpc_resource_controller
  ]

  tags = {
    Name        = "petpassport-${var.environment}-eks"
    Environment = var.environment
  }
}

# OIDC Provider para IAM Roles for Service Accounts (IRSA - External Secrets, etc.)
data "tls_certificate" "eks" {
  url = aws_eks_cluster.main.identity[0].oidc[0].issuer
}

resource "aws_iam_openid_connect_provider" "eks" {
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = [data.tls_certificate.eks.certificates[0].sha1_fingerprint]
  url             = aws_eks_cluster.main.identity[0].oidc[0].issuer
}

# 3. IAM Role para Nodos EKS
resource "aws_iam_role" "nodes" {
  name = "petpassport-${var.environment}-eks-node-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ec2.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "node_worker" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy"
  role       = aws_iam_role.nodes.name
}

resource "aws_iam_role_policy_attachment" "node_cni" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy"
  role       = aws_iam_role.nodes.name
}

resource "aws_iam_role_policy_attachment" "node_container_registry" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"
  role       = aws_iam_role.nodes.name
}

# 4. Node Pool General: api, frontend, keycloak, ingress (OPS-001)
resource "aws_eks_node_group" "general" {
  cluster_name    = aws_eks_cluster.main.name
  node_group_name = "general"
  node_role_arn   = aws_iam_role.nodes.arn
  subnet_ids      = var.private_subnet_ids

  scaling_config {
    desired_size = var.environment == "prod" ? 3 : 2
    max_size     = var.environment == "prod" ? 10 : 4
    min_size     = var.environment == "prod" ? 3 : 2
  }

  instance_types = [var.environment == "prod" ? "m6g.xlarge" : "m6g.large"]
  capacity_type  = "ON_DEMAND"

  labels = {
    workload = "general"
  }

  tags = {
    Name        = "petpassport-${var.environment}-node-general"
    Environment = var.environment
  }

  depends_on = [
    aws_iam_role_policy_attachment.node_worker,
    aws_iam_role_policy_attachment.node_cni,
    aws_iam_role_policy_attachment.node_container_registry
  ]
}

# 5. Node Pool Jobs: asynq pesados (OCR, PDF render, backups) con taints dedicados (OPS-001)
resource "aws_eks_node_group" "jobs" {
  cluster_name    = aws_eks_cluster.main.name
  node_group_name = "jobs"
  node_role_arn   = aws_iam_role.nodes.arn
  subnet_ids      = var.private_subnet_ids

  scaling_config {
    desired_size = var.environment == "prod" ? 2 : 1
    max_size     = var.environment == "prod" ? 6 : 3
    min_size     = var.environment == "prod" ? 2 : 1
  }

  instance_types = [var.environment == "prod" ? "c6g.xlarge" : "c6g.large"]
  capacity_type  = "ON_DEMAND"

  labels = {
    workload = "jobs"
  }

  taint {
    key    = "dedicated"
    value  = "jobs"
    effect = "NO_SCHEDULE"
  }

  tags = {
    Name        = "petpassport-${var.environment}-node-jobs"
    Environment = var.environment
  }

  depends_on = [
    aws_iam_role_policy_attachment.node_worker,
    aws_iam_role_policy_attachment.node_cni,
    aws_iam_role_policy_attachment.node_container_registry
  ]
}

output "cluster_name" {
  value = aws_eks_cluster.main.name
}

output "cluster_endpoint" {
  value = aws_eks_cluster.main.endpoint
}

output "cluster_certificate_authority_data" {
  value = aws_eks_cluster.main.certificate_authority[0].data
}

output "oidc_provider_arn" {
  value = aws_iam_openid_connect_provider.eks.arn
}

output "nodes_security_group_id" {
  value = aws_security_group.nodes.id
}
