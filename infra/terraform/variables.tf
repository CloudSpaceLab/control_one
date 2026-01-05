variable "aws_region" {
  description = "AWS region for resources"
  type        = string
  default     = "us-east-1"
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "Environment must be dev, staging, or prod."
  }
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = ""
}

variable "vpc_cidr" {
  description = "CIDR block for VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "availability_zones" {
  description = "List of availability zones"
  type        = list(string)
  default     = ["us-east-1a", "us-east-1b"]
}

variable "enable_ha" {
  description = "Enable high availability (multi-AZ)"
  type        = bool
  default     = true
}

variable "database_instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.t3.medium"
}

variable "database_allocated_storage" {
  description = "RDS allocated storage in GB"
  type        = number
  default     = 100
}

variable "database_master_username" {
  description = "RDS master username"
  type        = string
  default     = "controlone"
  sensitive   = true
}

variable "database_master_password" {
  description = "RDS master password"
  type        = string
  sensitive   = true
}

variable "redis_node_type" {
  description = "ElastiCache node type"
  type        = string
  default     = "cache.t3.medium"
}

variable "redis_num_cache_nodes" {
  description = "Number of cache nodes"
  type        = number
  default     = 1
}

variable "control_plane_instance_type" {
  description = "EC2 instance type for control plane"
  type        = string
  default     = "t3.large"
}

variable "control_plane_min_size" {
  description = "Minimum number of control plane instances"
  type        = number
  default     = 2
}

variable "control_plane_max_size" {
  description = "Maximum number of control plane instances"
  type        = number
  default     = 4
}

variable "control_plane_desired_capacity" {
  description = "Desired number of control plane instances"
  type        = number
  default     = 2
}

variable "s3_bucket_name" {
  description = "S3 bucket name for artifacts (must be globally unique)"
  type        = string
  default     = ""
}

variable "enable_ssl_certificate" {
  description = "Enable SSL certificate via ACM"
  type        = bool
  default     = false
}

variable "domain_name" {
  description = "Domain name for SSL certificate"
  type        = string
  default     = ""
}

variable "common_tags" {
  description = "Common tags for all resources"
  type        = map(string)
  default     = {}
}

variable "allowed_cidr_blocks" {
  description = "CIDR blocks allowed to access control plane"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "enable_nat_gateway" {
  description = "Enable NAT Gateway for private subnets"
  type        = bool
  default     = true
}

variable "enable_vpn" {
  description = "Enable VPN endpoint"
  type        = bool
  default     = false
}


