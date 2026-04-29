export type Category =
  | 'Web servers'
  | 'Databases'
  | 'Message queues'
  | 'App runtimes'
  | 'Security'
  | 'System'
  | 'Observability'
  | 'Storage'
  | 'Network services'
  | 'CI/CD';

export interface RulePackPortRule {
  name: string;
  port: number;
  protocol: 'tcp' | 'udp';
  expected_state: 'open' | 'closed';
  severity: 'critical' | 'high' | 'medium' | 'low';
}

export interface RulePackLogRule {
  name: string;
  log_source: string;
  pattern: string;
  severity: 'critical' | 'high' | 'medium' | 'low';
  window_seconds: number;
  threshold: number;
}

export interface RulePack {
  id: string;
  name: string;
  category: Category;
  description: string;
  tags: string[];
  portRules: RulePackPortRule[];
  logRules: RulePackLogRule[];
}

export const RULE_PACK_CATALOG: RulePack[] = [
  // ── Web servers ──────────────────────────────────────────────────────────
  {
    id: 'nginx',
    name: 'Nginx',
    category: 'Web servers',
    description: 'Monitor Nginx HTTP/HTTPS ports and detect 5xx error bursts, upstream failures, and file-descriptor exhaustion.',
    tags: ['nginx', 'http', 'https', 'reverse-proxy'],
    portRules: [
      { name: 'Nginx HTTP', port: 80, protocol: 'tcp', expected_state: 'open', severity: 'high' },
      { name: 'Nginx HTTPS', port: 443, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'Nginx 5xx burst', log_source: 'nginx', pattern: '" (5\\d{2}) ', severity: 'high', window_seconds: 60, threshold: 10 },
      { name: 'Nginx upstream failure', log_source: 'nginx', pattern: 'upstream (timed out|connection refused|connect\\(\\) failed)', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Nginx too many open files', log_source: 'nginx', pattern: 'Too many open files', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },
  {
    id: 'apache',
    name: 'Apache HTTP Server',
    category: 'Web servers',
    description: 'Monitor Apache HTTP/HTTPS ports and detect segfaults, 5xx error spikes, and mod_security blocks.',
    tags: ['apache', 'httpd', 'http', 'https'],
    portRules: [
      { name: 'Apache HTTP', port: 80, protocol: 'tcp', expected_state: 'open', severity: 'high' },
      { name: 'Apache HTTPS', port: 443, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'Apache segfault', log_source: 'apache2', pattern: 'segfault at', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Apache 5xx burst', log_source: 'apache2', pattern: '" (5\\d{2}) ', severity: 'high', window_seconds: 60, threshold: 10 },
      { name: 'mod_security block', log_source: 'apache2', pattern: 'ModSecurity: Access denied', severity: 'high', window_seconds: 300, threshold: 5 },
    ],
  },
  {
    id: 'haproxy',
    name: 'HAProxy',
    category: 'Web servers',
    description: 'Monitor HAProxy frontend, backend, and stats ports; detect backend down events, health-check failures, and refused connections.',
    tags: ['haproxy', 'load-balancer', 'proxy'],
    portRules: [
      { name: 'HAProxy HTTP', port: 80, protocol: 'tcp', expected_state: 'open', severity: 'high' },
      { name: 'HAProxy HTTPS', port: 443, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
      { name: 'HAProxy stats', port: 8404, protocol: 'tcp', expected_state: 'open', severity: 'medium' },
    ],
    logRules: [
      { name: 'HAProxy backend down', log_source: 'haproxy', pattern: 'Server .* is DOWN', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'HAProxy health check failure', log_source: 'haproxy', pattern: 'Health check for server .* failed', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'HAProxy connection refused', log_source: 'haproxy', pattern: 'Connection refused', severity: 'high', window_seconds: 60, threshold: 5 },
    ],
  },
  {
    id: 'traefik',
    name: 'Traefik',
    category: 'Web servers',
    description: 'Monitor Traefik HTTP, HTTPS, and dashboard ports; detect backend errors and TLS handshake failures.',
    tags: ['traefik', 'reverse-proxy', 'kubernetes', 'docker'],
    portRules: [
      { name: 'Traefik HTTP', port: 80, protocol: 'tcp', expected_state: 'open', severity: 'high' },
      { name: 'Traefik HTTPS', port: 443, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
      { name: 'Traefik dashboard', port: 8080, protocol: 'tcp', expected_state: 'open', severity: 'medium' },
    ],
    logRules: [
      { name: 'Traefik backend error', log_source: 'traefik', pattern: 'level=error.*backend', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Traefik TLS handshake failure', log_source: 'traefik', pattern: 'TLS handshake error|acme.*error|certificate.*failed', severity: 'high', window_seconds: 300, threshold: 3 },
    ],
  },
  {
    id: 'varnish',
    name: 'Varnish Cache',
    category: 'Web servers',
    description: 'Monitor Varnish HTTP and management ports; detect unhealthy backends and cache errors.',
    tags: ['varnish', 'cache', 'http', 'cdn'],
    portRules: [
      { name: 'Varnish HTTP', port: 6081, protocol: 'tcp', expected_state: 'open', severity: 'high' },
      { name: 'Varnish management', port: 6082, protocol: 'tcp', expected_state: 'open', severity: 'medium' },
    ],
    logRules: [
      { name: 'Varnish backend unhealthy', log_source: 'varnish', pattern: 'vcl_backend_error|Backend .* not healthy', severity: 'critical', window_seconds: 60, threshold: 3 },
      { name: 'Varnish cache error', log_source: 'varnish', pattern: 'Error (storing object|fetching url)', severity: 'high', window_seconds: 60, threshold: 5 },
    ],
  },
  {
    id: 'envoy',
    name: 'Envoy Proxy',
    category: 'Web servers',
    description: 'Monitor Envoy admin port; detect upstream connection failures and downstream resets.',
    tags: ['envoy', 'service-mesh', 'proxy', 'istio'],
    portRules: [
      { name: 'Envoy admin', port: 9901, protocol: 'tcp', expected_state: 'open', severity: 'medium' },
    ],
    logRules: [
      { name: 'Envoy upstream connect failure', log_source: 'envoy', pattern: 'upstream connect error|UF,URX', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Envoy reset by downstream', log_source: 'envoy', pattern: 'reset_by_downstream|DC,URX', severity: 'medium', window_seconds: 60, threshold: 10 },
    ],
  },
  {
    id: 'caddy',
    name: 'Caddy',
    category: 'Web servers',
    description: 'Monitor Caddy HTTP/HTTPS ports; detect TLS handshake failures and upstream errors.',
    tags: ['caddy', 'https', 'acme', 'reverse-proxy'],
    portRules: [
      { name: 'Caddy HTTP', port: 80, protocol: 'tcp', expected_state: 'open', severity: 'high' },
      { name: 'Caddy HTTPS', port: 443, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'Caddy TLS handshake failure', log_source: 'caddy', pattern: 'tls handshake error|obtaining certificate.*error', severity: 'high', window_seconds: 300, threshold: 3 },
      { name: 'Caddy upstream error', log_source: 'caddy', pattern: '"level":"error".*"upstream"', severity: 'high', window_seconds: 60, threshold: 5 },
    ],
  },

  // ── Databases ─────────────────────────────────────────────────────────────
  {
    id: 'postgresql',
    name: 'PostgreSQL',
    category: 'Databases',
    description: 'Monitor PostgreSQL port and detect FATAL/PANIC errors, connection exhaustion, checkpoint warnings, replication lag, and deadlocks.',
    tags: ['postgres', 'postgresql', 'sql', 'database'],
    portRules: [
      { name: 'PostgreSQL', port: 5432, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'PostgreSQL FATAL/PANIC', log_source: 'postgresql', pattern: 'FATAL:|PANIC:', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'PostgreSQL too many connections', log_source: 'postgresql', pattern: 'remaining connection slots are reserved|too many connections', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'PostgreSQL checkpoint warning', log_source: 'postgresql', pattern: 'checkpoint request|checkpoint taking longer than', severity: 'medium', window_seconds: 300, threshold: 5 },
      { name: 'PostgreSQL replication lag', log_source: 'postgresql', pattern: 'replication.*lag|standby.*behind', severity: 'high', window_seconds: 300, threshold: 3 },
      { name: 'PostgreSQL deadlock', log_source: 'postgresql', pattern: 'deadlock detected', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'mysql',
    name: 'MySQL',
    category: 'Databases',
    description: 'Monitor MySQL port and detect ERROR events, connection exhaustion, table lock timeouts, and InnoDB errors.',
    tags: ['mysql', 'sql', 'database', 'innodb'],
    portRules: [
      { name: 'MySQL', port: 3306, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'MySQL ERROR', log_source: 'mysql', pattern: '\\[ERROR\\]', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'MySQL too many connections', log_source: 'mysql', pattern: 'Too many connections|max_connections', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'MySQL table lock timeout', log_source: 'mysql', pattern: 'Lock wait timeout exceeded|Deadlock found', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'MySQL InnoDB error', log_source: 'mysql', pattern: 'InnoDB: (Error|Warning|Cannot)', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'mariadb',
    name: 'MariaDB',
    category: 'Databases',
    description: 'Monitor MariaDB port and detect errors, connection limits, lock timeouts, and Galera cluster replication errors.',
    tags: ['mariadb', 'mysql', 'galera', 'sql', 'database'],
    portRules: [
      { name: 'MariaDB', port: 3306, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'MariaDB ERROR', log_source: 'mariadb', pattern: '\\[ERROR\\]', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'MariaDB too many connections', log_source: 'mariadb', pattern: 'Too many connections|max_connections', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'MariaDB lock timeout', log_source: 'mariadb', pattern: 'Lock wait timeout exceeded|Deadlock found', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Galera cluster error', log_source: 'mariadb', pattern: 'WSREP.*error|Galera.*failed|node.*non-primary', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },
  {
    id: 'mongodb',
    name: 'MongoDB',
    category: 'Databases',
    description: 'Monitor MongoDB port and detect ERROR/SEVERE log events, connection failures, and replica set elections.',
    tags: ['mongodb', 'nosql', 'database', 'replica-set'],
    portRules: [
      { name: 'MongoDB', port: 27017, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'MongoDB ERROR/SEVERE', log_source: 'mongodb', pattern: '"s":"E"|"s":"F"|\\bERROR\\b|\\bSEVERE\\b', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'MongoDB connection refused', log_source: 'mongodb', pattern: 'Connection refused|Failed to connect', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'MongoDB replica election', log_source: 'mongodb', pattern: 'Starting an election|election abort|became primary|stepped down', severity: 'high', window_seconds: 300, threshold: 3 },
    ],
  },
  {
    id: 'redis',
    name: 'Redis',
    category: 'Databases',
    description: 'Monitor Redis port and detect OOM warnings, misconfiguration, connection limits, and broken replication.',
    tags: ['redis', 'cache', 'nosql', 'database'],
    portRules: [
      { name: 'Redis', port: 6379, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'Redis OOM warning', log_source: 'redis', pattern: 'Out of memory|used_memory rss', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Redis MISCONF', log_source: 'redis', pattern: 'MISCONF Redis is configured to save RDB|CONFIG SET', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Redis connection limit', log_source: 'redis', pattern: 'max number of clients reached|too many connections', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'Redis replication broken', log_source: 'redis', pattern: 'REPLICATION.*Broken|replication.*failed|replica.*disconnect', severity: 'high', window_seconds: 300, threshold: 3 },
    ],
  },
  {
    id: 'elasticsearch',
    name: 'Elasticsearch',
    category: 'Databases',
    description: 'Monitor Elasticsearch HTTP and transport ports; detect FATAL events, RED cluster health, unassigned shards, and circuit breaker trips.',
    tags: ['elasticsearch', 'elastic', 'search', 'database'],
    portRules: [
      { name: 'Elasticsearch HTTP', port: 9200, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
      { name: 'Elasticsearch transport', port: 9300, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'Elasticsearch FATAL', log_source: 'elasticsearch', pattern: '\\[FATAL\\]|level.*fatal', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Elasticsearch cluster RED', log_source: 'elasticsearch', pattern: 'status.*RED|cluster_health.*red', severity: 'critical', window_seconds: 300, threshold: 1 },
      { name: 'Elasticsearch unassigned shards', log_source: 'elasticsearch', pattern: 'unassigned_shards.*[^0]|shard.*not assigned', severity: 'high', window_seconds: 300, threshold: 1 },
      { name: 'Elasticsearch circuit breaker', log_source: 'elasticsearch', pattern: 'CircuitBreakingException|circuit_breaking_exception', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'opensearch',
    name: 'OpenSearch',
    category: 'Databases',
    description: 'Monitor OpenSearch HTTP and transport ports; detect FATAL events, RED cluster health, unassigned shards, and circuit breaker trips.',
    tags: ['opensearch', 'search', 'database', 'aws'],
    portRules: [
      { name: 'OpenSearch HTTP', port: 9200, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
      { name: 'OpenSearch transport', port: 9300, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'OpenSearch FATAL', log_source: 'opensearch', pattern: '\\[FATAL\\]|level.*fatal', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'OpenSearch cluster RED', log_source: 'opensearch', pattern: 'status.*RED|cluster_health.*red', severity: 'critical', window_seconds: 300, threshold: 1 },
      { name: 'OpenSearch unassigned shards', log_source: 'opensearch', pattern: 'unassigned_shards.*[^0]|shard.*not assigned', severity: 'high', window_seconds: 300, threshold: 1 },
      { name: 'OpenSearch circuit breaker', log_source: 'opensearch', pattern: 'CircuitBreakingException|circuit_breaking_exception', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'cassandra',
    name: 'Apache Cassandra',
    category: 'Databases',
    description: 'Monitor Cassandra CQL port and detect ERROR events, compaction overload, and dropped messages.',
    tags: ['cassandra', 'nosql', 'database', 'distributed'],
    portRules: [
      { name: 'Cassandra CQL', port: 9042, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'Cassandra ERROR', log_source: 'cassandra', pattern: 'ERROR\\s+\\[', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'Cassandra compaction overload', log_source: 'cassandra', pattern: 'Compaction.*overload|compaction.*backpressure', severity: 'high', window_seconds: 300, threshold: 5 },
      { name: 'Cassandra dropping messages', log_source: 'cassandra', pattern: 'Dropping (MUTATION|READ|RANGE_SLICE)', severity: 'critical', window_seconds: 60, threshold: 5 },
    ],
  },
  {
    id: 'influxdb',
    name: 'InfluxDB',
    category: 'Databases',
    description: 'Monitor InfluxDB HTTP and RPC ports and detect write timeouts and shard errors.',
    tags: ['influxdb', 'timeseries', 'database', 'metrics'],
    portRules: [
      { name: 'InfluxDB HTTP', port: 8086, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
      { name: 'InfluxDB RPC', port: 8088, protocol: 'tcp', expected_state: 'open', severity: 'medium' },
    ],
    logRules: [
      { name: 'InfluxDB write timeout', log_source: 'influxdb', pattern: 'write.*timeout|write request timed out', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'InfluxDB shard error', log_source: 'influxdb', pattern: 'shard.*error|error writing to shard', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'clickhouse',
    name: 'ClickHouse',
    category: 'Databases',
    description: 'Monitor ClickHouse HTTP and native ports; detect DB exceptions and memory limit breaches.',
    tags: ['clickhouse', 'olap', 'database', 'analytics'],
    portRules: [
      { name: 'ClickHouse HTTP', port: 8123, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
      { name: 'ClickHouse native', port: 9000, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'ClickHouse DB::Exception', log_source: 'clickhouse', pattern: 'DB::Exception', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'ClickHouse memory limit exceeded', log_source: 'clickhouse', pattern: 'Memory limit.*exceeded|MEMORY_LIMIT_EXCEEDED', severity: 'critical', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'meilisearch',
    name: 'Meilisearch',
    category: 'Databases',
    description: 'Monitor Meilisearch HTTP port and detect errors and missing index conditions.',
    tags: ['meilisearch', 'search', 'database'],
    portRules: [
      { name: 'Meilisearch', port: 7700, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'Meilisearch error', log_source: 'meilisearch', pattern: '"level":"ERROR"|ERROR meilisearch', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'Meilisearch index not found', log_source: 'meilisearch', pattern: 'index_not_found|Index .* not found', severity: 'medium', window_seconds: 300, threshold: 5 },
    ],
  },

  // ── Message queues ────────────────────────────────────────────────────────
  {
    id: 'rabbitmq',
    name: 'RabbitMQ',
    category: 'Message queues',
    description: 'Monitor RabbitMQ AMQP and management ports; detect connection failures, deep queues, node down events, and memory alarms.',
    tags: ['rabbitmq', 'amqp', 'message-queue', 'broker'],
    portRules: [
      { name: 'RabbitMQ AMQP', port: 5672, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
      { name: 'RabbitMQ management', port: 15672, protocol: 'tcp', expected_state: 'open', severity: 'medium' },
    ],
    logRules: [
      { name: 'RabbitMQ connection refused', log_source: 'rabbitmq', pattern: 'connection_refused|refused TCP connection', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'RabbitMQ node down', log_source: 'rabbitmq', pattern: 'rabbit.*down|node.*down|lost contact with', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'RabbitMQ memory alarm', log_source: 'rabbitmq', pattern: 'memory alarm|vm_memory_high_watermark', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'RabbitMQ queue depth', log_source: 'rabbitmq', pattern: 'queue_messages_ready.*[0-9]{5,}', severity: 'high', window_seconds: 300, threshold: 3 },
    ],
  },
  {
    id: 'kafka',
    name: 'Apache Kafka',
    category: 'Message queues',
    description: 'Monitor Kafka broker port and detect under-replicated partitions, leader elections, and broker disconnects.',
    tags: ['kafka', 'message-queue', 'streaming', 'broker'],
    portRules: [
      { name: 'Kafka broker', port: 9092, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'Kafka under-replicated partitions', log_source: 'kafka', pattern: 'UnderReplicatedPartitions|under.replicated.partitions.*[^0]', severity: 'high', window_seconds: 300, threshold: 1 },
      { name: 'Kafka leader election', log_source: 'kafka', pattern: 'LeaderElection|Partition.*elected.*leader|New leader', severity: 'medium', window_seconds: 300, threshold: 5 },
      { name: 'Kafka broker disconnect', log_source: 'kafka', pattern: 'Broker.*disconnected|Lost connection to node', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'nats',
    name: 'NATS',
    category: 'Message queues',
    description: 'Monitor NATS client port and detect client errors, slow consumers, and connection limits.',
    tags: ['nats', 'message-queue', 'pubsub', 'streaming'],
    portRules: [
      { name: 'NATS client', port: 4222, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'NATS client error', log_source: 'nats', pattern: '\\[ERR\\].*client|client.*error', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'NATS slow consumer', log_source: 'nats', pattern: 'Slow Consumer', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'NATS max connections', log_source: 'nats', pattern: 'maximum connections exceeded|max_connections', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },
  {
    id: 'activemq',
    name: 'Apache ActiveMQ',
    category: 'Message queues',
    description: 'Monitor ActiveMQ broker port and detect WARN/ERROR log events, broker failures, and producer blocks.',
    tags: ['activemq', 'jms', 'message-queue', 'broker'],
    portRules: [
      { name: 'ActiveMQ broker', port: 61616, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'ActiveMQ WARN/ERROR', log_source: 'activemq', pattern: 'WARN |ERROR ', severity: 'medium', window_seconds: 60, threshold: 5 },
      { name: 'ActiveMQ broker failing', log_source: 'activemq', pattern: 'broker.*fail|Failed to start.*transport|Exception.*transport', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'ActiveMQ producer blocked', log_source: 'activemq', pattern: 'ProducerFlowControl|producer.*blocked', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'pulsar',
    name: 'Apache Pulsar',
    category: 'Message queues',
    description: 'Monitor Pulsar broker and HTTP ports; detect ledger errors and broker failures.',
    tags: ['pulsar', 'message-queue', 'streaming', 'broker'],
    portRules: [
      { name: 'Pulsar broker', port: 6650, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
      { name: 'Pulsar HTTP', port: 8080, protocol: 'tcp', expected_state: 'open', severity: 'medium' },
    ],
    logRules: [
      { name: 'Pulsar ledger error', log_source: 'pulsar', pattern: 'ManagedLedger.*error|ledger.*exception|BookieException', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'Pulsar broker failure', log_source: 'pulsar', pattern: 'BrokerServiceException|Failed.*broker|broker.*unavailable', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },

  // ── App runtimes ──────────────────────────────────────────────────────────
  {
    id: 'nodejs_pm2',
    name: 'Node.js / PM2',
    category: 'App runtimes',
    description: 'Detect uncaught exceptions, unexpected PM2 restarts, and out-of-memory kills in Node.js applications.',
    tags: ['nodejs', 'pm2', 'javascript', 'runtime'],
    portRules: [],
    logRules: [
      { name: 'Node.js uncaught exception', log_source: 'pm2', pattern: 'UnhandledPromiseRejection|uncaughtException|UnhandledPromiseRejectionWarning', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'PM2 SIGTERM restart', log_source: 'pm2', pattern: 'SIGTERM|process restarted|app crashed', severity: 'high', window_seconds: 300, threshold: 5 },
      { name: 'Node.js memory exceeded', log_source: 'pm2', pattern: 'JavaScript heap out of memory|Allocation failed.*JavaScript heap', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },
  {
    id: 'java_jvm',
    name: 'Java / JVM',
    category: 'App runtimes',
    description: 'Detect JVM OutOfMemoryError, stack overflows, GC overhead limits, and thread exhaustion.',
    tags: ['java', 'jvm', 'spring', 'runtime'],
    portRules: [],
    logRules: [
      { name: 'JVM OutOfMemoryError', log_source: 'java', pattern: 'java\\.lang\\.OutOfMemoryError', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'JVM StackOverflowError', log_source: 'java', pattern: 'java\\.lang\\.StackOverflowError', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'JVM GC overhead limit', log_source: 'java', pattern: 'GC overhead limit exceeded|concurrent mode failure', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'JVM thread creation failed', log_source: 'java', pattern: 'unable to create new native thread|OutOfMemoryError.*thread', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },
  {
    id: 'python_gunicorn',
    name: 'Python / Gunicorn',
    category: 'App runtimes',
    description: 'Detect Gunicorn worker timeouts and OOM kills in Python web applications.',
    tags: ['python', 'gunicorn', 'wsgi', 'runtime'],
    portRules: [],
    logRules: [
      { name: 'Gunicorn worker timeout', log_source: 'gunicorn', pattern: 'CRITICAL WORKER TIMEOUT|Worker with pid.*timed out', severity: 'critical', window_seconds: 60, threshold: 3 },
      { name: 'Gunicorn OOM kill', log_source: 'gunicorn', pattern: 'MemoryError|OOM kill|Killed by SIGKILL', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Gunicorn application error', log_source: 'gunicorn', pattern: '\\[ERROR\\] Exception in worker process', severity: 'high', window_seconds: 60, threshold: 5 },
    ],
  },
  {
    id: 'php_fpm',
    name: 'PHP-FPM',
    category: 'App runtimes',
    description: 'Monitor PHP-FPM port and detect max_children exhaustion, slow requests, and fatal errors.',
    tags: ['php', 'php-fpm', 'runtime', 'web'],
    portRules: [
      { name: 'PHP-FPM', port: 9000, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'PHP-FPM max_children', log_source: 'php-fpm', pattern: 'max_children.*reached|server reached pm\\.max_children', severity: 'critical', window_seconds: 60, threshold: 3 },
      { name: 'PHP-FPM slow request', log_source: 'php-fpm', pattern: 'pool .* slow request|executing too slow', severity: 'medium', window_seconds: 300, threshold: 5 },
      { name: 'PHP fatal error', log_source: 'php-fpm', pattern: 'PHP Fatal error:|Fatal error: Allowed memory', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'ruby_puma',
    name: 'Ruby / Puma',
    category: 'App runtimes',
    description: 'Detect Puma thread deaths, 500 responses, and timeout errors in Ruby web applications.',
    tags: ['ruby', 'puma', 'rails', 'runtime'],
    portRules: [],
    logRules: [
      { name: 'Puma thread died', log_source: 'puma', pattern: 'PumaThreadPool.*thread died|Rack app error', severity: 'critical', window_seconds: 60, threshold: 3 },
      { name: 'Puma 500 responses', log_source: 'puma', pattern: '500 Internal Server Error|HTTP 500', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Puma Timeout::Error', log_source: 'puma', pattern: 'Timeout::Error|Rack::Timeout::RequestTimeoutException', severity: 'high', window_seconds: 60, threshold: 5 },
    ],
  },
  {
    id: 'celery',
    name: 'Celery',
    category: 'App runtimes',
    description: 'Detect Celery worker losses, soft time limit violations, and task failures.',
    tags: ['celery', 'python', 'task-queue', 'worker'],
    portRules: [],
    logRules: [
      { name: 'Celery WorkerLostError', log_source: 'celery', pattern: 'WorkerLostError|Worker exited prematurely', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Celery soft time limit', log_source: 'celery', pattern: 'SoftTimeLimitExceeded|soft time limit exceeded', severity: 'high', window_seconds: 300, threshold: 5 },
      { name: 'Celery task failed', log_source: 'celery', pattern: 'Task .* raised unexpected|FAILURE|task-failed', severity: 'high', window_seconds: 60, threshold: 5 },
    ],
  },
  {
    id: 'django',
    name: 'Django',
    category: 'App runtimes',
    description: 'Detect Django 500 errors, misconfigurations, and database errors.',
    tags: ['django', 'python', 'web', 'runtime'],
    portRules: [],
    logRules: [
      { name: 'Django 500 error', log_source: 'django', pattern: 'Internal Server Error: |Error 500', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Django ImproperlyConfigured', log_source: 'django', pattern: 'ImproperlyConfigured|django\\.core\\.exceptions', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Django DatabaseError', log_source: 'django', pattern: 'DatabaseError|OperationalError.*database', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'rails',
    name: 'Ruby on Rails',
    category: 'App runtimes',
    description: 'Detect Rails routing errors, runtime exceptions, and 500 responses.',
    tags: ['rails', 'ruby', 'web', 'runtime'],
    portRules: [],
    logRules: [
      { name: 'Rails routing error', log_source: 'rails', pattern: 'ActionController::RoutingError', severity: 'medium', window_seconds: 60, threshold: 10 },
      { name: 'Rails RuntimeError', log_source: 'rails', pattern: 'RuntimeError|StandardError.*app', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Rails 500 responses', log_source: 'rails', pattern: 'Completed 500 Internal Server Error', severity: 'high', window_seconds: 60, threshold: 5 },
    ],
  },
  {
    id: 'laravel',
    name: 'Laravel',
    category: 'App runtimes',
    description: 'Detect Laravel production errors, fatal throwable exceptions, and PDO database errors.',
    tags: ['laravel', 'php', 'web', 'runtime'],
    portRules: [],
    logRules: [
      { name: 'Laravel production ERROR', log_source: 'laravel', pattern: 'production\\.ERROR|\\[ERROR\\].*laravel', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Laravel FatalThrowableError', log_source: 'laravel', pattern: 'FatalThrowableError|FatalErrorException', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Laravel PDOException', log_source: 'laravel', pattern: 'PDOException|SQLSTATE\\[', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },

  // ── Security ──────────────────────────────────────────────────────────────
  {
    id: 'sshd',
    name: 'SSH Server (sshd)',
    category: 'Security',
    description: 'Monitor SSH port and detect failed password attempts, invalid users, and connection anomalies.',
    tags: ['ssh', 'sshd', 'auth', 'security'],
    portRules: [
      { name: 'SSH', port: 22, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'SSH failed password', log_source: 'auth', pattern: 'Failed password for ', severity: 'medium', window_seconds: 60, threshold: 5 },
      { name: 'SSH invalid user', log_source: 'auth', pattern: 'Invalid user .* from', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'SSH connection closed authenticating', log_source: 'auth', pattern: 'Connection closed by authenticating user', severity: 'medium', window_seconds: 60, threshold: 10 },
      { name: 'SSH possible break-in attempt', log_source: 'auth', pattern: 'POSSIBLE BREAK-IN ATTEMPT|reverse mapping.*failed', severity: 'high', window_seconds: 300, threshold: 1 },
    ],
  },
  {
    id: 'sshd_bruteforce',
    name: 'SSH Brute-Force Detection',
    category: 'Security',
    description: 'Detect SSH brute-force attacks via high-frequency password and publickey failures.',
    tags: ['ssh', 'brute-force', 'auth', 'security'],
    portRules: [],
    logRules: [
      { name: 'SSH brute-force critical', log_source: 'auth', pattern: 'Failed (password|publickey) for', severity: 'critical', window_seconds: 60, threshold: 10 },
    ],
  },
  {
    id: 'fail2ban',
    name: 'Fail2ban',
    category: 'Security',
    description: 'Monitor Fail2ban for high ban rates and elevated detection activity indicating ongoing attacks.',
    tags: ['fail2ban', 'security', 'ips', 'firewall'],
    portRules: [],
    logRules: [
      { name: 'Fail2ban ban activity', log_source: 'fail2ban', pattern: 'Ban ', severity: 'medium', window_seconds: 300, threshold: 20 },
      { name: 'Fail2ban high detection rate', log_source: 'fail2ban', pattern: 'Found ', severity: 'high', window_seconds: 300, threshold: 50 },
    ],
  },
  {
    id: 'ufw_blocked',
    name: 'UFW Firewall Blocks',
    category: 'Security',
    description: 'Detect elevated UFW firewall block rates indicating port scanning or DDoS activity.',
    tags: ['ufw', 'firewall', 'security', 'network'],
    portRules: [],
    logRules: [
      { name: 'UFW block burst', log_source: 'syslog', pattern: 'UFW BLOCK', severity: 'high', window_seconds: 60, threshold: 50 },
    ],
  },
  {
    id: 'auditd',
    name: 'Linux Audit (auditd)',
    category: 'Security',
    description: 'Detect privilege escalation via su/sudo, SELinux/AppArmor AVC denials, and suspicious syscalls.',
    tags: ['auditd', 'audit', 'security', 'linux'],
    portRules: [],
    logRules: [
      { name: 'auditd su escalation', log_source: 'audit', pattern: 'rule=execve comm="su" auid!=0|comm="sudo".*auid!=unset', severity: 'high', window_seconds: 300, threshold: 3 },
      { name: 'auditd AVC denial', log_source: 'audit', pattern: 'AVC denied|type=AVC.*denied', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'auditd suspicious syscall', log_source: 'audit', pattern: 'SYSCALL exe="/bin/su"|SYSCALL.*ptrace|key="root_commands"', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },
  {
    id: 'vault_hashicorp',
    name: 'HashiCorp Vault',
    category: 'Security',
    description: 'Monitor Vault API port and detect sealed state, expired leases, and authentication failures.',
    tags: ['vault', 'hashicorp', 'secrets', 'security'],
    portRules: [
      { name: 'Vault API', port: 8200, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'Vault sealed', log_source: 'vault', pattern: 'vault is sealed|Vault is sealed|vault.*sealed', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Vault lease expired', log_source: 'vault', pattern: 'lease expired|token expired|renew-self.*error', severity: 'high', window_seconds: 300, threshold: 5 },
      { name: 'Vault authentication error', log_source: 'vault', pattern: 'permission denied|authentication failed|invalid token', severity: 'high', window_seconds: 60, threshold: 10 },
    ],
  },
  {
    id: 'certbot',
    name: 'Certbot / ACME',
    category: 'Security',
    description: 'Detect Certbot certificate renewal failures and ACME protocol errors.',
    tags: ['certbot', 'acme', 'tls', 'certificates'],
    portRules: [],
    logRules: [
      { name: 'Certbot renewal failure', log_source: 'certbot', pattern: 'Failed to renew|Certificate not yet due for renewal.*error|renewal failed', severity: 'critical', window_seconds: 3600, threshold: 1 },
      { name: 'Certbot ACME error', log_source: 'certbot', pattern: 'ACME.*error|acme\\.errors|Challenge did not pass', severity: 'high', window_seconds: 3600, threshold: 1 },
    ],
  },

  // ── System ────────────────────────────────────────────────────────────────
  {
    id: 'systemd',
    name: 'systemd',
    category: 'System',
    description: 'Detect failed systemd services and units that have entered error states.',
    tags: ['systemd', 'linux', 'system', 'services'],
    portRules: [],
    logRules: [
      { name: 'systemd service failed', log_source: 'syslog', pattern: 'systemd.*service failed|Failed to start|entered failed state', severity: 'high', window_seconds: 60, threshold: 1 },
      { name: 'systemd error unit reached', log_source: 'syslog', pattern: 'Reached error unit|Reached target.*failed', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },
  {
    id: 'docker',
    name: 'Docker',
    category: 'System',
    description: 'Detect Docker container OOM kills, unhealthy containers, unexpected exits, and image pull failures.',
    tags: ['docker', 'containers', 'system', 'devops'],
    portRules: [],
    logRules: [
      { name: 'Docker OOMKilled', log_source: 'docker', pattern: 'OOMKilled|memory limit exceeded.*container', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Docker health check failing', log_source: 'docker', pattern: 'Health check.*failing|health_status.*unhealthy', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'Docker container exit', log_source: 'docker', pattern: 'container.*die.*exitCode.*[^0]|exited with non-zero', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'Docker image pull failure', log_source: 'docker', pattern: 'unable to find image|pull.*failed|manifest unknown', severity: 'high', window_seconds: 60, threshold: 1 },
    ],
  },
  {
    id: 'kubernetes_kubelet',
    name: 'Kubernetes Kubelet',
    category: 'System',
    description: 'Monitor kubelet port and detect pod failures, image pull backoffs, crash loops, and OOM kills.',
    tags: ['kubernetes', 'k8s', 'kubelet', 'containers'],
    portRules: [
      { name: 'Kubelet API', port: 10250, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'Kubernetes pod failed', log_source: 'kubelet', pattern: 'Failed to.*pod|pod.*failed|Back-off restarting', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'Kubernetes ImagePullBackOff', log_source: 'kubelet', pattern: 'ImagePullBackOff|ErrImagePull|failed to pull image', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'Kubernetes CrashLoopBackOff', log_source: 'kubelet', pattern: 'CrashLoopBackOff|back-off.*restarting failed container', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Kubernetes OOMKilling', log_source: 'kubelet', pattern: 'OOMKilling|OOM score.*killed', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },
  {
    id: 'cron',
    name: 'Cron Jobs',
    category: 'System',
    description: 'Detect failed cron jobs and errors reported to syslog over an hourly window.',
    tags: ['cron', 'scheduler', 'system', 'linux'],
    portRules: [],
    logRules: [
      { name: 'Cron failures', log_source: 'syslog', pattern: 'cron.*(FAILED|error|no space left)', severity: 'medium', window_seconds: 3600, threshold: 3 },
    ],
  },
  {
    id: 'ntp_chrony',
    name: 'NTP / Chrony',
    category: 'System',
    description: 'Detect NTP/Chrony time sync issues: large offsets, time steps, and missing sources.',
    tags: ['ntp', 'chrony', 'time-sync', 'system'],
    portRules: [],
    logRules: [
      { name: 'Chrony offset above limit', log_source: 'chrony', pattern: 'Offset .* (above|exceeds).*limit|offset error', severity: 'high', window_seconds: 300, threshold: 3 },
      { name: 'Chrony time step', log_source: 'chrony', pattern: 'System clock was stepped|large time offset', severity: 'high', window_seconds: 300, threshold: 1 },
      { name: 'Chrony no sources online', log_source: 'chrony', pattern: 'No NTP sources|no usable NTP sources|cannot synchronise', severity: 'critical', window_seconds: 300, threshold: 1 },
    ],
  },
  {
    id: 'oom_killer',
    name: 'OOM Killer',
    category: 'System',
    description: 'Detect Linux OOM killer activations — each event indicates severe memory pressure.',
    tags: ['oom', 'memory', 'kernel', 'system'],
    portRules: [],
    logRules: [
      { name: 'OOM kill event', log_source: 'syslog', pattern: 'Out of memory|Killed process|oom_kill_process', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },
  {
    id: 'disk_full',
    name: 'Disk Full',
    category: 'System',
    description: 'Detect "no space left on device" and "disk full" conditions — immediate critical alert on first occurrence.',
    tags: ['disk', 'storage', 'system', 'capacity'],
    portRules: [],
    logRules: [
      { name: 'Disk full event', log_source: 'syslog', pattern: 'No space left on device|disk full|ENOSPC', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },

  // ── Observability ─────────────────────────────────────────────────────────
  {
    id: 'prometheus',
    name: 'Prometheus',
    category: 'Observability',
    description: 'Monitor Prometheus HTTP port and detect TSDB errors, rule loading failures, and remote storage issues.',
    tags: ['prometheus', 'metrics', 'monitoring', 'observability'],
    portRules: [
      { name: 'Prometheus HTTP', port: 9090, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
    ],
    logRules: [
      { name: 'Prometheus TSDB error', log_source: 'prometheus', pattern: 'level=error.*tsdb|TSDB.*error|head.*WAL', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'Prometheus rule loading error', log_source: 'prometheus', pattern: 'level=error.*loading rules|Error loading rule', severity: 'high', window_seconds: 300, threshold: 1 },
      { name: 'Prometheus remote storage failure', log_source: 'prometheus', pattern: 'remote_write.*error|remote storage.*fail|non-recoverable.*remote', severity: 'high', window_seconds: 60, threshold: 5 },
    ],
  },
  {
    id: 'grafana',
    name: 'Grafana',
    category: 'Observability',
    description: 'Monitor Grafana HTTP port and detect critical-level log events and database connectivity failures.',
    tags: ['grafana', 'dashboards', 'monitoring', 'observability'],
    portRules: [
      { name: 'Grafana HTTP', port: 3000, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'Grafana critical error', log_source: 'grafana', pattern: 'lvl=crit|level=critical', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Grafana database error', log_source: 'grafana', pattern: 'failed to connect to database|database.*error|sql.*error', severity: 'critical', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'alertmanager',
    name: 'Alertmanager',
    category: 'Observability',
    description: 'Monitor Alertmanager HTTP port and detect errors and notification dispatch failures.',
    tags: ['alertmanager', 'prometheus', 'alerts', 'observability'],
    portRules: [
      { name: 'Alertmanager HTTP', port: 9093, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'Alertmanager error', log_source: 'alertmanager', pattern: 'level=error', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Alertmanager dispatch failure', log_source: 'alertmanager', pattern: 'failed to dispatch|Notify.*failed|send.*error', severity: 'high', window_seconds: 300, threshold: 3 },
    ],
  },
  {
    id: 'node_exporter',
    name: 'Node Exporter',
    category: 'Observability',
    description: 'Monitor Node Exporter port — if closed, host metrics are no longer being scraped.',
    tags: ['node-exporter', 'prometheus', 'metrics', 'observability'],
    portRules: [
      { name: 'Node Exporter', port: 9100, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [],
  },
  {
    id: 'telegraf',
    name: 'Telegraf',
    category: 'Observability',
    description: 'Detect Telegraf metric collection warnings, write errors, and buffer overflow conditions.',
    tags: ['telegraf', 'influxdata', 'metrics', 'observability'],
    portRules: [],
    logRules: [
      { name: 'Telegraf warning', log_source: 'telegraf', pattern: 'W! \\[', severity: 'medium', window_seconds: 300, threshold: 10 },
      { name: 'Telegraf write error', log_source: 'telegraf', pattern: 'E! \\[.*outputs|write.*error|failed to write', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Telegraf batch full', log_source: 'telegraf', pattern: 'batch is full|metrics buffer is full|write.*timeout', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'jaeger',
    name: 'Jaeger',
    category: 'Observability',
    description: 'Monitor Jaeger collector and UI ports; detect span processor errors.',
    tags: ['jaeger', 'tracing', 'opentelemetry', 'observability'],
    portRules: [
      { name: 'Jaeger collector', port: 14268, protocol: 'tcp', expected_state: 'open', severity: 'high' },
      { name: 'Jaeger UI', port: 16686, protocol: 'tcp', expected_state: 'open', severity: 'medium' },
    ],
    logRules: [
      { name: 'Jaeger span processor error', log_source: 'jaeger', pattern: 'span_processor.*error|Failed to.*span|SpanProcessor.*failed', severity: 'high', window_seconds: 60, threshold: 5 },
    ],
  },

  // ── Storage ───────────────────────────────────────────────────────────────
  {
    id: 'minio',
    name: 'MinIO',
    category: 'Storage',
    description: 'Monitor MinIO API and console ports; detect ERROR events, offline disks, and rate limiting.',
    tags: ['minio', 's3', 'object-storage', 'storage'],
    portRules: [
      { name: 'MinIO API', port: 9000, protocol: 'tcp', expected_state: 'open', severity: 'critical' },
      { name: 'MinIO console', port: 9001, protocol: 'tcp', expected_state: 'open', severity: 'medium' },
    ],
    logRules: [
      { name: 'MinIO ERROR', log_source: 'minio', pattern: '"level":"ERROR"|MINIO_ERROR', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'MinIO disk offline', log_source: 'minio', pattern: 'disk.*offline|drive.*disconnected|XL disk.*offline', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'MinIO too many requests', log_source: 'minio', pattern: 'SlowDown|storage resource.*unavailable', severity: 'high', window_seconds: 60, threshold: 10 },
    ],
  },
  {
    id: 'nfs',
    name: 'NFS',
    category: 'Storage',
    description: 'Detect NFS server unresponsiveness and mount failures reported to syslog.',
    tags: ['nfs', 'network-filesystem', 'storage', 'linux'],
    portRules: [],
    logRules: [
      { name: 'NFS server not responding', log_source: 'syslog', pattern: 'nfs: server .* not responding|nfs.*not responding', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'NFS mount failed', log_source: 'syslog', pattern: 'mount.*nfs.*failed|nfsmount.*failed|rpc.*failed', severity: 'high', window_seconds: 60, threshold: 1 },
    ],
  },
  {
    id: 'ceph',
    name: 'Ceph',
    category: 'Storage',
    description: 'Detect Ceph errors, health warnings, OSD downs, and slow request backlogs.',
    tags: ['ceph', 'object-storage', 'distributed-storage', 'storage'],
    portRules: [],
    logRules: [
      { name: 'Ceph ERROR', log_source: 'ceph', pattern: '\\[ERR\\]', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'Ceph health WARN', log_source: 'ceph', pattern: 'health WARN|health_warn', severity: 'medium', window_seconds: 300, threshold: 5 },
      { name: 'Ceph OSD down', log_source: 'ceph', pattern: 'osd\\.\\d+ down|OSD.*is down', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Ceph slow requests', log_source: 'ceph', pattern: 'slow requests|slow_requests.*[0-9]{3,}', severity: 'high', window_seconds: 300, threshold: 3 },
    ],
  },
  {
    id: 'glusterfs',
    name: 'GlusterFS',
    category: 'Storage',
    description: 'Detect GlusterFS critical failures, disconnected bricks, and volumes needing healing.',
    tags: ['glusterfs', 'distributed-filesystem', 'storage'],
    portRules: [],
    logRules: [
      { name: 'GlusterFS CRITICAL', log_source: 'glusterfs', pattern: 'CRITICAL|\\[CRITICAL\\]', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'GlusterFS brick disconnected', log_source: 'glusterfs', pattern: 'brick is not connected|Brick .* is not running', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'GlusterFS heal needed', log_source: 'glusterfs', pattern: 'heal.*needed|self-heal.*required|split-brain', severity: 'high', window_seconds: 300, threshold: 3 },
    ],
  },

  // ── Network services ──────────────────────────────────────────────────────
  {
    id: 'openvpn',
    name: 'OpenVPN',
    category: 'Network services',
    description: 'Monitor OpenVPN UDP port and detect TLS errors, authentication failures, and peer timeouts.',
    tags: ['openvpn', 'vpn', 'network', 'security'],
    portRules: [
      { name: 'OpenVPN UDP', port: 1194, protocol: 'udp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'OpenVPN TLS Error', log_source: 'openvpn', pattern: 'TLS Error|TLS handshake failed|TLS_ERROR', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'OpenVPN AUTH_FAILED', log_source: 'openvpn', pattern: 'AUTH_FAILED|VERIFY ERROR|TLS Auth Error', severity: 'high', window_seconds: 300, threshold: 5 },
      { name: 'OpenVPN peer not responding', log_source: 'openvpn', pattern: 'Inactivity timeout.*restarting|peer not responding', severity: 'medium', window_seconds: 300, threshold: 5 },
    ],
  },
  {
    id: 'bind_dns',
    name: 'BIND DNS',
    category: 'Network services',
    description: 'Monitor BIND DNS ports (TCP+UDP) and detect SERVFAIL responses, refused queries, and recursion limits.',
    tags: ['bind', 'dns', 'network', 'named'],
    portRules: [
      { name: 'DNS TCP', port: 53, protocol: 'tcp', expected_state: 'open', severity: 'high' },
      { name: 'DNS UDP', port: 53, protocol: 'udp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'BIND SERVFAIL', log_source: 'named', pattern: 'SERVFAIL|query failed.*SERVFAIL', severity: 'high', window_seconds: 60, threshold: 10 },
      { name: 'BIND queries refused', log_source: 'named', pattern: 'query.*refused|REFUSED', severity: 'medium', window_seconds: 60, threshold: 20 },
      { name: 'BIND max-recursive-clients', log_source: 'named', pattern: 'max-recursive-clients exceeded|recursive clients.*exceeded', severity: 'high', window_seconds: 60, threshold: 5 },
    ],
  },
  {
    id: 'postfix',
    name: 'Postfix',
    category: 'Network services',
    description: 'Monitor Postfix SMTP port and detect rejected messages, connection failures, and bounced mail.',
    tags: ['postfix', 'smtp', 'email', 'network'],
    portRules: [
      { name: 'Postfix SMTP', port: 25, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'Postfix rejected', log_source: 'postfix', pattern: 'status=rejected|reject.*RCPT', severity: 'medium', window_seconds: 300, threshold: 20 },
      { name: 'Postfix connection refused', log_source: 'postfix', pattern: 'connect to .* Connection refused|refused.*connection', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Postfix bounce', log_source: 'postfix', pattern: 'status=bounced|delivery temporarily suspended', severity: 'medium', window_seconds: 300, threshold: 10 },
    ],
  },
  {
    id: 'dovecot',
    name: 'Dovecot',
    category: 'Network services',
    description: 'Monitor Dovecot IMAP ports and detect authentication failures and fatal/panic events.',
    tags: ['dovecot', 'imap', 'email', 'network'],
    portRules: [
      { name: 'Dovecot IMAP', port: 143, protocol: 'tcp', expected_state: 'open', severity: 'high' },
      { name: 'Dovecot IMAPS', port: 993, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'Dovecot auth failed', log_source: 'dovecot', pattern: 'auth failed|authentication failure|no auth attempts', severity: 'medium', window_seconds: 60, threshold: 10 },
      { name: 'Dovecot Fatal/Panic', log_source: 'dovecot', pattern: 'Fatal:|Panic:', severity: 'critical', window_seconds: 60, threshold: 1 },
    ],
  },

  // ── CI/CD ─────────────────────────────────────────────────────────────────
  {
    id: 'jenkins',
    name: 'Jenkins',
    category: 'CI/CD',
    description: 'Monitor Jenkins HTTP port and detect SEVERE log events, OOM conditions, and connectivity failures.',
    tags: ['jenkins', 'ci-cd', 'build', 'automation'],
    portRules: [
      { name: 'Jenkins HTTP', port: 8080, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'Jenkins SEVERE', log_source: 'jenkins', pattern: 'SEVERE|java\\.lang\\.RuntimeException', severity: 'high', window_seconds: 60, threshold: 3 },
      { name: 'Jenkins out of memory', log_source: 'jenkins', pattern: 'java\\.lang\\.OutOfMemoryError|GC overhead limit exceeded', severity: 'critical', window_seconds: 60, threshold: 1 },
      { name: 'Jenkins connection failure', log_source: 'jenkins', pattern: 'Failed to connect.*agent|Connection was broken|Remote call failed', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'gitlab_runner',
    name: 'GitLab Runner',
    category: 'CI/CD',
    description: 'Detect GitLab Runner errors, job failures, and runner connection errors.',
    tags: ['gitlab', 'gitlab-runner', 'ci-cd', 'build'],
    portRules: [],
    logRules: [
      { name: 'GitLab Runner ERROR', log_source: 'gitlab-runner', pattern: 'level=error|ERROR.*runner', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'GitLab Runner job failed', log_source: 'gitlab-runner', pattern: 'job failed|failed.*job.*reason', severity: 'high', window_seconds: 300, threshold: 5 },
      { name: 'GitLab Runner error', log_source: 'gitlab-runner', pattern: 'runner.*error|Failed to.*runner|Cannot.*runner', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
  {
    id: 'argocd',
    name: 'Argo CD',
    category: 'CI/CD',
    description: 'Monitor Argo CD API port and detect error-level events, sync failures, and out-of-sync apps.',
    tags: ['argocd', 'gitops', 'kubernetes', 'ci-cd'],
    portRules: [
      { name: 'Argo CD API', port: 8080, protocol: 'tcp', expected_state: 'open', severity: 'high' },
    ],
    logRules: [
      { name: 'Argo CD error', log_source: 'argocd', pattern: 'level=error', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Argo CD sync failed', log_source: 'argocd', pattern: 'sync.*failed|Failed to sync|SyncFailed', severity: 'high', window_seconds: 300, threshold: 3 },
      { name: 'Argo CD OutOfSync', log_source: 'argocd', pattern: 'OutOfSync|out of sync', severity: 'medium', window_seconds: 3600, threshold: 5 },
    ],
  },
  {
    id: 'github_actions_runner',
    name: 'GitHub Actions Runner',
    category: 'CI/CD',
    description: 'Detect GitHub Actions self-hosted runner errors, SIGTERM signals, and connectivity failures.',
    tags: ['github-actions', 'ci-cd', 'runner', 'automation'],
    portRules: [],
    logRules: [
      { name: 'Actions Runner error', log_source: 'github-actions', pattern: '\\[Error\\]|error.*runner|runner.*error', severity: 'high', window_seconds: 60, threshold: 5 },
      { name: 'Actions Runner SIGTERM', log_source: 'github-actions', pattern: 'Runner received SIGTERM|runner exiting', severity: 'high', window_seconds: 300, threshold: 3 },
      { name: 'Actions Runner connection failure', log_source: 'github-actions', pattern: 'Cannot connect to GitHub|Failed to connect.*github\\.com', severity: 'high', window_seconds: 60, threshold: 3 },
    ],
  },
];
