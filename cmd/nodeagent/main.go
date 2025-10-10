package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/access"
	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
	"github.com/CloudSpaceLab/control_one/internal/config"
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

	telemetrySvc := telemetry.New(client, log)

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
	}

	sched := scheduler.New(log)

	if cfg.Intervals.Provisioning > 0 {
		if _, err := sched.AddInterval("provisioning", cfg.Intervals.Provisioning, func() {
			if err := provEngine.ApplyTemplate(ctx, state.NodeID, state.Metadata); err != nil {
				log.Warn("apply provisioning template", zap.Error(err))
			}
			if err := provEngine.RunBaselines(ctx, state.NodeID); err != nil {
				log.Warn("provisioning baselines", zap.Error(err))
			}
		}); err != nil {
			log.Fatal("schedule provisioning", zap.Error(err))
		}
	}

	if _, err := sched.AddInterval("policy-sync", cfg.Intervals.PolicySync, func() {
		pset, err := policySyncer.FetchAndPersist(ctx, state.NodeID)
		if err != nil {
			log.Warn("policy sync failed", zap.Error(err))
			return
		}
		log.Info("policy sync complete", zap.Int("policies", len(pset.Policies)))
	}); err != nil {
		log.Fatal("schedule policy sync", zap.Error(err))
	}

	if _, err := sched.AddInterval("compliance-scan", cfg.Intervals.Scan, func() {
		policies, err := policySyncer.LoadCached()
		if err != nil {
			log.Warn("load cached policies", zap.Error(err))
			return
		}
		results, err := scannerSvc.Run(ctx, policies)
		if err != nil {
			log.Warn("run compliance scan", zap.Error(err))
		}
		ruleMap := make(map[string]string, len(policies))
		for _, rule := range policies {
			ruleMap[rule.ID] = rule.Check
		}
		if compResults, err := complianceEngine.Evaluate(ctx, state.NodeID, ruleMap); err != nil {
			log.Warn("compliance evaluation", zap.Error(err))
		} else if len(compResults) > 0 {
			log.Info("compliance evaluation complete", zap.Int("rules", len(compResults)))
		}
		telemetrySvc.SendCompliance(ctx, state.NodeID, results)
	}); err != nil {
		log.Fatal("schedule compliance scan", zap.Error(err))
	}

	metricsInterval := cfg.TelemetryPrefs.MetricsInterval
	if metricsInterval <= 0 {
		metricsInterval = cfg.Intervals.Telemetry
	}
	if _, err := sched.AddInterval("telemetry-metrics", metricsInterval, func() {
		metrics := util.CollectHostMetrics()
		telemetrySvc.SendMetrics(ctx, state.NodeID, metrics)
	}); err != nil {
		log.Fatal("schedule telemetry", zap.Error(err))
	}

	if cfg.Access.SyncInterval > 0 {
		if _, err := sched.AddInterval("access-sync", cfg.Access.SyncInterval, func() {
			if err := accessMgr.Sync(ctx); err != nil {
				log.Warn("access sync failed", zap.Error(err))
			}
		}); err != nil {
			log.Fatal("schedule access sync", zap.Error(err))
		}
	}

	if cfg.Secrets.SyncInterval > 0 {
		if _, err := sched.AddInterval("secrets-sync", cfg.Secrets.SyncInterval, func() {
			if err := secretStore.Sync(ctx); err != nil {
				log.Warn("secrets sync failed", zap.Error(err))
			}
		}); err != nil {
			log.Fatal("schedule secrets sync", zap.Error(err))
		}
	}

	if _, err := sched.AddInterval("heartbeat", cfg.Intervals.Heartbeat, func() {
		telemetrySvc.SendHeartbeat(ctx, state.NodeID, uuid.NewString())
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
