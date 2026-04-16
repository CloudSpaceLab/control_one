package logs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/CloudSpaceLab/control_one/internal/config"
)

type Preset struct {
	Name    string
	Sources []config.LogSourceConfig
}

var (
	presetRegistry     = map[string]Preset{}
	defaultPresetOrder = []string{"nginx", "apache", "mysql", "postgresql", "redis", "kafka"}
)

func RegisterPreset(name string, preset Preset) {
	if strings.TrimSpace(name) == "" {
		return
	}
	presetRegistry[strings.ToLower(name)] = preset
}

func PrepareSources(userSources []config.LogSourceConfig) []config.LogSourceConfig {
	merged := make(map[string]config.LogSourceConfig)

	for _, name := range defaultPresetOrder {
		if preset, ok := presetRegistry[name]; ok {
			for _, src := range preset.Sources {
				merged[strings.ToLower(src.Program)] = applyPathResolution(src)
			}
		}
	}

	for _, user := range userSources {
		key := strings.ToLower(strings.TrimSpace(user.Program))
		if key == "" {
			continue
		}

		base, ok := merged[key]
		if !ok {
			base = config.LogSourceConfig{Program: user.Program}
		}

		merged[key] = applyPathResolution(mergeLogSource(base, user))
	}

	result := make([]config.LogSourceConfig, 0, len(merged))
	for _, src := range merged {
		config.NormalizeLogSourceConfig(&src)
		result = append(result, src)
	}
	return result
}

func mergeLogSource(base, override config.LogSourceConfig) config.LogSourceConfig {
	result := base

	if strings.TrimSpace(override.Type) != "" {
		result.Type = override.Type
	}
	if len(override.Paths) > 0 {
		result.Paths = override.Paths
	}
	if len(override.JournalUnits) > 0 {
		result.JournalUnits = override.JournalUnits
	}
	if len(override.EventChannels) > 0 {
		result.EventChannels = override.EventChannels
	}
	if strings.TrimSpace(override.Formatter) != "" {
		result.Formatter = override.Formatter
	}
	if override.BatchSize > 0 {
		result.BatchSize = override.BatchSize
	}
	if override.BufferSize > 0 {
		result.BufferSize = override.BufferSize
	}
	if override.FlushInterval > 0 {
		result.FlushInterval = override.FlushInterval
	}
	if override.PollInterval > 0 {
		result.PollInterval = override.PollInterval
	}
	if override.Disabled {
		result.Disabled = true
	}

	if len(override.FormatRules) > 0 {
		result.FormatRules = override.FormatRules
	}

	if len(result.SeverityMap) == 0 {
		result.SeverityMap = map[string]string{}
	}
	for k, v := range override.SeverityMap {
		result.SeverityMap[k] = v
	}

	if len(result.Labels) == 0 {
		result.Labels = map[string]string{}
	}
	for k, v := range override.Labels {
		result.Labels[k] = v
	}

	return result
}

func applyPathResolution(src config.LogSourceConfig) config.LogSourceConfig {
	resolved := src
	resolved.Paths = resolvePaths(src.Program, src.Paths)
	return resolved
}

func resolvePaths(program string, explicit []string) []string {
	candidates := make([]string, 0, len(explicit)+4)
	for _, p := range explicit {
		candidates = append(candidates, expandPath(p))
	}
	candidates = append(candidates, presetPathCandidates(program)...)
	candidates = dedupeStrings(candidates)

	existing := filterExistingPaths(candidates)
	if len(existing) > 0 {
		return existing
	}
	return candidates
}

func presetPathCandidates(program string) []string {
	switch strings.ToLower(program) {
	case "nginx":
		return nginxDefaultPaths()
	case "apache":
		return apacheDefaultPaths()
	case "mysql":
		return mysqlDefaultPaths()
	case "postgresql":
		return postgresDefaultPaths()
	case "redis":
		return redisDefaultPaths()
	case "kafka":
		return kafkaDefaultPaths()
	default:
		return nil
	}
}

func nginxDefaultPaths() []string {
	if runtime.GOOS == "windows" {
		return []string{"C:/nginx/logs/access.log", "C:/nginx/logs/error.log"}
	}
	return []string{"/var/log/nginx/access.log", "/var/log/nginx/error.log"}
}

func apacheDefaultPaths() []string {
	if runtime.GOOS == "windows" {
		return []string{"C:/Program Files/Apache Group/Apache2/logs/access.log", "C:/Program Files/Apache Group/Apache2/logs/error.log"}
	}
	return []string{"/var/log/apache2/access.log", "/var/log/apache2/error.log"}
}

func mysqlDefaultPaths() []string {
	if runtime.GOOS == "windows" {
		return []string{"C:/ProgramData/MySQL/MySQL Server 8.0/Data/hostname.err"}
	}
	return []string{"/var/log/mysql/error.log", "/var/log/mysqld.log"}
}

func postgresDefaultPaths() []string {
	if runtime.GOOS == "windows" {
		return []string{"C:/Program Files/PostgreSQL/14/data/log/postgresql.log"}
	}
	return []string{"/var/log/postgresql/postgresql-14-main.log", "/var/lib/pgsql/data/log/postgresql.log"}
}

func redisDefaultPaths() []string {
	if runtime.GOOS == "windows" {
		return []string{"C:/Program Files/Redis/logs/redis.log"}
	}
	return []string{"/var/log/redis/redis-server.log"}
}

func kafkaDefaultPaths() []string {
	if runtime.GOOS == "windows" {
		return []string{"C:/kafka/logs/server.log"}
	}
	return []string{"/var/log/kafka/server.log", "/opt/kafka/logs/server.log"}
}

func expandPath(p string) string {
	if p == "" {
		return p
	}
	expanded := os.ExpandEnv(p)
	if strings.HasPrefix(expanded, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~"))
		}
	}
	return filepath.Clean(expanded)
}

func filterExistingPaths(paths []string) []string {
	existing := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		}
	}
	return existing
}

func dedupeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return out
}

func init() {
	RegisterPreset("nginx", Preset{
		Name: "nginx",
		Sources: []config.LogSourceConfig{
			{
				Program:   "nginx",
				Type:      "file",
				Paths:     nginxDefaultPaths(),
				Formatter: "nginx",
				SeverityMap: map[string]string{
					"crit":   "critical",
					"notice": "info",
					"warn":   "warn",
				},
			},
		},
	})

	RegisterPreset("apache", Preset{
		Name: "apache",
		Sources: []config.LogSourceConfig{
			{
				Program:   "apache",
				Type:      "file",
				Paths:     apacheDefaultPaths(),
				Formatter: "apache",
			},
		},
	})

	RegisterPreset("mysql", Preset{
		Name: "mysql",
		Sources: []config.LogSourceConfig{
			{
				Program:   "mysql",
				Type:      "file",
				Paths:     mysqlDefaultPaths(),
				Formatter: "mysql",
			},
		},
	})

	RegisterPreset("postgresql", Preset{
		Name: "postgresql",
		Sources: []config.LogSourceConfig{
			{
				Program:   "postgresql",
				Type:      "file",
				Paths:     postgresDefaultPaths(),
				Formatter: "generic",
				FormatRules: []config.LogFormatRuleConfig{
					{
						Regex:           "^(?P<ts>\\d{4}-\\d{2}-\\d{2}\\s+\\d{2}:\\d{2}:\\d{2}\\.\\d+\\s+\\w+)\\s+\\[(?P<pid>\\d+)\\]\\s+(?P<level>\\w+):\\s+(?P<message>.*)$",
						TimestampLayout: "2006-01-02 15:04:05.999 MST",
						SeverityField:   "level",
						SeverityMap: map[string]string{
							"LOG":     "info",
							"WARNING": "warn",
							"ERROR":   "error",
							"FATAL":   "critical",
						},
						Fields: map[string]string{
							"pid": "${pid}",
						},
					},
				},
			},
		},
	})

	RegisterPreset("redis", Preset{
		Name: "redis",
		Sources: []config.LogSourceConfig{
			{
				Program:   "redis",
				Type:      "file",
				Paths:     redisDefaultPaths(),
				Formatter: "generic",
				FormatRules: []config.LogFormatRuleConfig{
					{
						Regex:           "^(?P<ts>\\d{2}\\s\\w+\\s\\d{2}:\\d{2}:\\d{2})\\s+(?P<level>\\w+)\\s+(?P<pid>\\d+):\\s*(?P<message>.*)$",
						TimestampLayout: "02 Jan 15:04:05",
						SeverityField:   "level",
						SeverityMap: map[string]string{
							"warning": "warn",
							"notice":  "notice",
						},
						Fields: map[string]string{
							"pid": "${pid}",
						},
					},
				},
			},
		},
	})

	RegisterPreset("kafka", Preset{
		Name: "kafka",
		Sources: []config.LogSourceConfig{
			{
				Program:   "kafka",
				Type:      "file",
				Paths:     kafkaDefaultPaths(),
				Formatter: "generic",
				FormatRules: []config.LogFormatRuleConfig{
					{
						Regex:           "^(?P<ts>\\d{4}-\\d{2}-\\d{2}\\s+\\d{2}:\\d{2}:\\d{2},\\d{3})\\s+(?P<level>\\w+)\\s+\\[(?P<thread>[^]]+)\\]\\s+(?P<class>[^ ]+)\\s+-\\s+(?P<message>.*)$",
						TimestampLayout: "2006-01-02 15:04:05,000",
						SeverityField:   "level",
						SeverityMap: map[string]string{
							"WARN":  "warn",
							"ERROR": "error",
							"INFO":  "info",
							"FATAL": "critical",
						},
						Fields: map[string]string{
							"thread": "${thread}",
							"class":  "${class}",
						},
					},
				},
			},
		},
	})
}
