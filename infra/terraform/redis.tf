resource "aws_elasticache_subnet_group" "main" {
  name       = "${local.name_prefix}-redis-subnet-group"
  subnet_ids = aws_subnet.private[*].id

  tags = merge(
    local.common_tags,
    {
      Name = "${local.name_prefix}-redis-subnet-group"
    }
  )
}

resource "random_password" "redis_auth" {
  count   = var.environment == "prod" ? 1 : 0
  length  = 32
  special = true
}

resource "aws_elasticache_replication_group" "main" {
  replication_group_id       = "${local.name_prefix}-redis"
  description                 = "Redis cluster for Control One"
  node_type                   = var.redis_node_type
  port                        = 6379
  parameter_group_name        = "default.redis7"
  num_cache_clusters          = var.enable_ha && var.environment == "prod" ? var.redis_num_cache_nodes : 1
  automatic_failover_enabled  = var.enable_ha && var.environment == "prod"
  multi_az_enabled            = var.enable_ha && var.environment == "prod"
  at_rest_encryption_enabled  = true
  transit_encryption_enabled  = true
  auth_token                  = var.environment == "prod" ? random_password.redis_auth[0].result : null

  subnet_group_name  = aws_elasticache_subnet_group.main.name
  security_group_ids = [aws_security_group.redis.id]

  snapshot_retention_limit = var.environment == "prod" ? 5 : 1
  snapshot_window         = "03:00-05:00"

  tags = merge(
    local.common_tags,
    {
      Name = "${local.name_prefix}-redis"
    }
  )
}

