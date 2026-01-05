# Control One Terraform Infrastructure

This directory contains Terraform modules for deploying Control One control plane infrastructure on AWS.

## Overview

The Terraform configuration provisions:
- **VPC** with public, private, and database subnets across multiple availability zones
- **RDS PostgreSQL** database with encryption, backups, and optional multi-AZ
- **ElastiCache Redis** cluster for job queue and caching
- **Application Load Balancer** with optional SSL/TLS termination
- **S3 bucket** for artifacts and logs
- **Security groups** with least-privilege access rules
- **NAT Gateways** for private subnet internet access

## Prerequisites

1. **AWS Account** with appropriate permissions
2. **Terraform** >= 1.5.0
3. **AWS CLI** configured with credentials
4. **S3 bucket** for Terraform state (optional but recommended)

## Quick Start

1. **Copy example variables:**
   ```bash
   cp terraform.tfvars.example terraform.tfvars
   ```

2. **Edit `terraform.tfvars`** with your values:
   - AWS region
   - Environment (dev/staging/prod)
   - Database credentials
   - S3 bucket name (must be globally unique)
   - Domain name (if using SSL)

3. **Configure Terraform backend** (optional):
   Edit `main.tf` backend configuration or use environment variables:
   ```bash
   export AWS_REGION=us-east-1
   export TF_VAR_aws_region=us-east-1
   ```

4. **Initialize Terraform:**
   ```bash
   terraform init
   ```

5. **Plan deployment:**
   ```bash
   terraform plan
   ```

6. **Apply configuration:**
   ```bash
   terraform apply
   ```

## Configuration

### Variables

Key variables to configure:

- `environment`: Environment name (dev, staging, prod)
- `vpc_cidr`: CIDR block for VPC (default: 10.0.0.0/16)
- `database_instance_class`: RDS instance type (default: db.t3.medium)
- `database_master_password`: Database password (sensitive)
- `redis_node_type`: ElastiCache node type (default: cache.t3.medium)
- `s3_bucket_name`: S3 bucket name (must be globally unique)
- `enable_ha`: Enable high availability (default: true)
- `enable_ssl_certificate`: Enable SSL certificate via ACM
- `domain_name`: Domain name for SSL certificate

### Environments

The configuration supports three environments:

- **dev**: Single-AZ, no deletion protection, shorter backup retention
- **staging**: Multi-AZ optional, moderate backup retention
- **prod**: Multi-AZ required, deletion protection, extended backups

## Outputs

After deployment, Terraform outputs:

- `vpc_id`: VPC ID
- `database_host`: RDS hostname
- `redis_endpoint`: ElastiCache endpoint
- `load_balancer_dns`: Load balancer DNS name
- `s3_bucket_name`: S3 bucket name
- Security group IDs

## State Management

For production, use remote state backend:

```hcl
backend "s3" {
  bucket = "control-one-terraform-state"
  key    = "control-plane/terraform.tfstate"
  region = "us-east-1"
  encrypt = true
  dynamodb_table = "terraform-state-lock"
}
```

## Security Considerations

1. **Database passwords**: Use AWS Secrets Manager or parameter store
2. **SSL certificates**: Use ACM for automatic renewal
3. **Security groups**: Review and restrict CIDR blocks
4. **S3 bucket**: Enable versioning and encryption
5. **Backups**: Configure appropriate retention periods

## Cost Optimization

- Use `enable_nat_gateway = false` for dev environments
- Use smaller instance types for non-production
- Enable S3 lifecycle policies for cost savings
- Use single-AZ for dev/staging

## Troubleshooting

### Common Issues

1. **S3 bucket name conflict**: Choose a globally unique name
2. **VPC CIDR conflicts**: Ensure CIDR doesn't overlap with existing networks
3. **SSL certificate validation**: Complete DNS validation for ACM certificates
4. **Database connection**: Check security group rules

## Next Steps

After infrastructure is provisioned:

1. Deploy Control One using Helm charts (see `../helm/`)
2. Configure DNS to point to load balancer
3. Set up monitoring and alerting
4. Configure backup and disaster recovery procedures

## Support

For issues or questions, refer to:
- [Deployment Guide](../../docs/deployment.md)
- [Architecture Documentation](../../docs/architecture.md)
- [Operational Runbooks](../../docs/runbooks.md)


