package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/access"
	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/hooks"
	"github.com/CloudSpaceLab/control_one/internal/logger"
	"github.com/CloudSpaceLab/control_one/internal/mesh"
	"github.com/CloudSpaceLab/control_one/internal/policy"
	"github.com/CloudSpaceLab/control_one/internal/provisioning"
	"github.com/CloudSpaceLab/control_one/internal/registration"
	"github.com/CloudSpaceLab/control_one/internal/scanner"
	"github.com/CloudSpaceLab/control_one/internal/scheduler"
	"github.com/CloudSpaceLab/control_one/internal/secrets"
	"github.com/CloudSpaceLab/control_one/internal/telemetry"
	"github.com/CloudSpaceLab/control_one/internal/util"
	"github.com/CloudSpaceLab/control_one/internal/wizard"
)

func main() {
	cfgPath := flag.String("config", "", "path to node agent config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		panic(err)
	}

	if err := cfg.EnsureDirectories(); err != nil {
		panic(err)
	}

	log, err := logger.New()
	if err != nil {
		panic(err)
	}
	defer logger.Sync(log)
	logger.ReplaceGlobals(log)

	hooksService := hooks.NewService(log, cfg.Hooks)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := api.NewClient(cfg.APIURL, cfg.TLS.CertFile, cfg.TLS.KeyFile, cfg.TLS.CACertFile, cfg.BootstrapToken)
	if err != nil {
		log.Fatal("api client init failed", zap.Error(err))
	}
	registrar := registration.NewRegistrar(client, log)

	sysInfo := util.GatherSystemInfo()
	req := &registration.RegisterRequest{
		BootstrapToken: cfg.BootstrapToken,
		Hostname:       sysInfo.Hostname,
		OS:             sysInfo.OS,
		Arch:           sysInfo.Arch,
		PublicIP:       sysInfo.PublicIP,
		Fingerprint:    sysInfo.Fingerprint,
	}
	state, err := registrar.Register(ctx, req, cfg.StateFile)
	if err != nil {
		log.Fatal("registration failed", zap.Error(err))
	}
	emitHook(ctx, hooksService, log, "agent.registration.success", state.NodeID, map[string]any{
		"hostname": sysInfo.Hostname,
	})

	wiz := wizard.NewRunner(log, cfg, client, state.NodeID, hooksService)
	if err := wiz.Run(ctx); err != nil {
		emitHook(ctx, hooksService, log, "wizard.run.failed", state.NodeID, map[string]any{"error": err.Error()})
		log.Fatal("wizard failed", zap.Error(err))
	}
	emitHook(ctx, hooksService, log, "wizard.run.completed", state.NodeID, nil)

	policySyncer, err := policy.NewSyncer(client, log, policy.Options{
		PolicyDir:     cfg.PolicyDir,
		PublicKeyPath: cfg.Policy.PublicKeyFile,
		MetadataPath:  cfg.Policy.MetadataFile,
	})
	if err != nil {
		log.Fatal("policy syncer init failed", zap.Error(err))
	}
	scannerSvc := scanner.NewBuiltinScanner(log, scanner.Options{
		Timeout: cfg.Scanner.Timeout,
		Shell:   cfg.Scanner.Shell,
	})

	telemetrySvc := telemetry.New(client, log, hooksService)
	if cfg.TelemetryPrefs.CollectLogs && len(cfg.TelemetryPrefs.LogSources) > 0 {
		telemetrySvc.StartLogCollection(ctx, state.NodeID, cfg.TelemetryPrefs.LogSources)
	}

	meshMgr := mesh.New(log, client, mesh.Options{
		Enabled:        cfg.Mesh.Enabled,
		CoordinatorURL: cfg.Mesh.CoordinatorURL,
		AuthToken:      cfg.Mesh.AuthToken,
		Namespace:      cfg.Mesh.Namespace,
		PrivateCIDR:    cfg.Mesh.PrivateCIDR,
		RelayNodes:     cfg.Mesh.RelayNodes,
		StateFile:      cfg.Mesh.StateFile,
		PollInterval:   cfg.Mesh.PollInterval,
		KeyRotation:    cfg.Mesh.KeyRotation,
		NodeID:         state.NodeID,
	})
	if err := meshMgr.EnsureState(); err != nil {
		log.Fatal("mesh state init failed", zap.Error(err))
	}
	emitHook(ctx, hooksService, log, "mesh.state.ready", state.NodeID, map[string]any{"namespace": cfg.Mesh.Namespace})
	meshMgr.Start(ctx)

	provEngine := provisioning.NewEngine(log, client, provisioning.Options{
		Template:        cfg.Provisioning.Template,
		Baselines:       cfg.Provisioning.Baselines,
		AutoRemediation: cfg.Provisioning.AutoRemediation,
	})

	complianceEngine := compliance.NewEngine(log, client, compliance.Options{
		Region:         cfg.Compliance.Region,
		RuleSets:       cfg.Compliance.RuleSets,
		Certifications: cfg.Compliance.Certifications,
		AutoApply:      cfg.Compliance.AutoApplyTemplates,
	})

	accessMgr := access.NewManager(log, client, access.Options{
		Provider:     access.ProviderType(cfg.Access.Provider),
		SyncInterval: cfg.Access.SyncInterval,
		DefaultRole:  cfg.Access.DefaultRole,
		APIEndpoint:  cfg.Access.APIEndpoint,
		NodeID:       state.NodeID,
	})
	if err := accessMgr.Sync(ctx); err != nil {
		log.Warn("initial access sync failed", zap.Error(err))
		emitHook(ctx, hooksService, log, "access.sync.failed", state.NodeID, map[string]any{"error": err.Error()})
	} else {
		emitHook(ctx, hooksService, log, "access.sync.completed", state.NodeID, nil)
	}

	secretStore := secrets.NewStore(log, client, secrets.Options{
		Backend:      secrets.BackendType(cfg.Secrets.Backend),
		Endpoint:     cfg.Secrets.Endpoint,
		Groups:       cfg.Secrets.Groups,
		SyncInterval: cfg.Secrets.SyncInterval,
		NodeID:       state.NodeID,
	})

	if err := secretStore.Sync(ctx); err != nil {
		log.Warn("initial secrets sync failed", zap.Error(err))
		emitHook(ctx, hooksService, log, "secrets.sync.failed", state.NodeID, map[string]any{"error": err.Error()})
	} else {
		emitHook(ctx, hooksService, log, "secrets.sync.completed", state.NodeID, nil)
	}
	sched := scheduler.New(log)

	if cfg.Intervals.Provisioning > 0 {
		if _, err := sched.AddInterval("provisioning", cfg.Intervals.Provisioning, func() {
			if err := provEngine.ApplyTemplate(ctx, state.NodeID, state.Metadata); err != nil {
				log.Warn("apply provisioning template", zap.Error(err))
				emitHook(ctx, hooksService, log, "provisioning.template.failed", state.NodeID, map[string]any{"error": err.Error()})
			}
			if err := provEngine.RunBaselines(ctx, state.NodeID); err != nil {
				log.Warn("provisioning baselines", zap.Error(err))
				emitHook(ctx, hooksService, log, "provisioning.baselines.failed", state.NodeID, map[string]any{"error": err.Error()})
			}
			emitHook(ctx, hooksService, log, "provisioning.cycle.completed", state.NodeID, nil)
		}); err != nil {
			log.Fatal("schedule provisioning", zap.Error(err))
		}
	}

	if cfg.Intervals.PolicySync > 0 {
		if _, err := sched.AddInterval("policy-sync", cfg.Intervals.PolicySync, func() {
			pset, err := policySyncer.FetchAndPersist(ctx, state.NodeID)
			if err != nil {
				log.Warn("policy sync failed", zap.Error(err))
				emitHook(ctx, hooksService, log, "policy.sync.failed", state.NodeID, map[string]any{"error": err.Error()})
				return
			}
			log.Info("policy sync complete", zap.Int("policies", len(pset.Policies)))
			emitHook(ctx, hooksService, log, "policy.sync.completed", state.NodeID, map[string]any{"count": len(pset.Policies)})
		}); err != nil {
			log.Fatal("schedule policy sync", zap.Error(err))
		}
	}

	if cfg.Intervals.Scan > 0 {
		if _, err := sched.AddInterval("compliance-scan", cfg.Intervals.Scan, func() {
			policies, err := policySyncer.LoadCached()
			if err != nil {
				log.Warn("load cached policies", zap.Error(err))
				emitHook(ctx, hooksService, log, "compliance.scan.skipped", state.NodeID, map[string]any{"error": err.Error()})
				return
			}
			results, err := scannerSvc.Run(ctx, policies)
			if err != nil {
				log.Warn("run compliance scan", zap.Error(err))
				emitHook(ctx, hooksService, log, "compliance.scan.failed", state.NodeID, map[string]any{"error": err.Error()})
				return
			}
			ruleMap := make(map[string]string, len(policies))
			for _, rule := range policies {
				ruleMap[rule.ID] = rule.Check
			}
			if compResults, err := complianceEngine.Evaluate(ctx, state.NodeID, ruleMap); err != nil {
				log.Warn("compliance evaluation", zap.Error(err))
				emitHook(ctx, hooksService, log, "compliance.evaluate.failed", state.NodeID, map[string]any{"error": err.Error()})
				return
			} else if len(compResults) > 0 {
				log.Info("compliance evaluation complete", zap.Int("rules", len(compResults)))
				emitHook(ctx, hooksService, log, "compliance.evaluate.completed", state.NodeID, map[string]any{"rules": len(compResults)})
			}
			telemetrySvc.SendCompliance(ctx, state.NodeID, results)
			emitHook(ctx, hooksService, log, "telemetry.compliance.sent", state.NodeID, map[string]any{"checks": len(results)})
		}); err != nil {
			log.Fatal("schedule compliance scan", zap.Error(err))
		}
	}

	metricsInterval := cfg.TelemetryPrefs.MetricsInterval
	if metricsInterval <= 0 {
		metricsInterval = cfg.Intervals.Telemetry
	}
	if metricsInterval <= 0 {
		metricsInterval = time.Minute
	}
	if _, err := sched.AddInterval("telemetry-metrics", metricsInterval, func() {
		metrics := util.CollectHostMetrics()
		telemetrySvc.SendMetrics(ctx, state.NodeID, metrics)
		emitHook(ctx, hooksService, log, "telemetry.metrics.sent", state.NodeID, map[string]any{"components": len(metrics)})
	}); err != nil {
		log.Fatal("schedule telemetry", zap.Error(err))
	}

	if cfg.Access.SyncInterval > 0 {
		if _, err := sched.AddInterval("access-sync", cfg.Access.SyncInterval, func() {
			if err := accessMgr.Sync(ctx); err != nil {
				log.Warn("access sync failed", zap.Error(err))
				emitHook(ctx, hooksService, log, "access.sync.failed", state.NodeID, map[string]any{"error": err.Error()})
			} else {
				emitHook(ctx, hooksService, log, "access.sync.completed", state.NodeID, nil)
			}
		}); err != nil {
			log.Fatal("schedule access sync", zap.Error(err))
		}
	}

	if cfg.Secrets.SyncInterval > 0 {
		if _, err := sched.AddInterval("secrets-sync", cfg.Secrets.SyncInterval, func() {
			if err := secretStore.Sync(ctx); err != nil {
				log.Warn("secrets sync failed", zap.Error(err))
				emitHook(ctx, hooksService, log, "secrets.sync.failed", state.NodeID, map[string]any{"error": err.Error()})
			} else {
				emitHook(ctx, hooksService, log, "secrets.sync.completed", state.NodeID, nil)
			}
		}); err != nil {
			log.Fatal("schedule secrets sync", zap.Error(err))
		}
	}

	if _, err := sched.AddInterval("heartbeat", cfg.Intervals.Heartbeat, func() {
		telemetrySvc.SendHeartbeat(ctx, state.NodeID, uuid.NewString())
		emitHook(ctx, hooksService, log, "telemetry.heartbeat.sent", state.NodeID, nil)
	}); err != nil {
		log.Fatal("schedule heartbeat", zap.Error(err))
	}

	sched.Start()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Info("shutdown signal received")
	sched.Stop(ctx)
}

func emitHook(parent context.Context, publisher hooks.Publisher, log *zap.Logger, eventID, subject string, payload map[string]any) {
	if publisher == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	evt := &hooks.Event{
		EventID:   eventID,
		Source:    "nodeagent",
		Subject:   subject,
		Payload:   payload,
		Metadata:  map[string]string{"component": "nodeagent"},
		Timestamp: time.Now().UTC(),
	}
	if err := publisher.PublishEvent(ctx, evt); err != nil && !errors.Is(err, context.Canceled) {
		log.Debug("hook publish failed", zap.String("event_id", eventID), zap.Error(err))
	}
}
