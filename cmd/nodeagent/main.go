package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/access"
	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/dbquery"
	"github.com/CloudSpaceLab/control_one/internal/events"
	"github.com/CloudSpaceLab/control_one/internal/eventstream"
	"github.com/CloudSpaceLab/control_one/internal/fileaccess"
	"github.com/CloudSpaceLab/control_one/internal/hooks"
	"github.com/CloudSpaceLab/control_one/internal/logger"
	"github.com/CloudSpaceLab/control_one/internal/mesh"
	"github.com/CloudSpaceLab/control_one/internal/netflow"
	"github.com/CloudSpaceLab/control_one/internal/policy"
	"github.com/CloudSpaceLab/control_one/internal/procmon"
	"github.com/CloudSpaceLab/control_one/internal/provisioning"
	"github.com/CloudSpaceLab/control_one/internal/registration"
	"github.com/CloudSpaceLab/control_one/internal/scanner"
	"github.com/CloudSpaceLab/control_one/internal/scheduler"
	"github.com/CloudSpaceLab/control_one/internal/secrets"
	"github.com/CloudSpaceLab/control_one/internal/securityfacts"
	"github.com/CloudSpaceLab/control_one/internal/sessionrecording"
	"github.com/CloudSpaceLab/control_one/internal/telemetry"
	"github.com/CloudSpaceLab/control_one/internal/util"
	"github.com/CloudSpaceLab/control_one/internal/wizard"
)

func main() {
	// Subcommand dispatch. We keep flag-style invocation for backwards compatibility
	// (`--join`, `--install-service`, etc.) but also accept simple subcommands like
	// `controlone-agent uninstall` and `controlone-agent verify-binary ...`.
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "uninstall":
			if err := runUninstall(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "uninstall failed: %v\n", err)
				os.Exit(1)
			}
			return
		case "verify-binary":
			if err := runVerifyBinary(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "verify-binary failed: %v\n", err)
				os.Exit(1)
			}
			return
		case "rotate-cert":
			if err := runRotateCert(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "rotate-cert failed: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	cfgPath := flag.String("config", "", "path to node agent config file")
	joinURL := flag.String("join", "", "Control plane URL to enroll with")
	joinToken := flag.String("token", "", "Enrollment token")
	nodeName := flag.String("name", "", "Override node hostname")
	configDirFlag := flag.String("config-dir", "/etc/control-one", "Config directory")
	dataDirFlag := flag.String("data-dir", "/var/lib/control-one/nodeagent", "Data directory")
	installServiceFlag := flag.Bool("install-service", false, "Install systemd service")
	startAfterJoin := flag.Bool("start", false, "Start agent after join")
	flag.Parse()

	if *joinURL != "" {
		if *joinToken == "" {
			fmt.Fprintln(os.Stderr, "error: --token is required with --join")
			os.Exit(1)
		}
		if err := runJoin(*joinURL, *joinToken, *nodeName, *configDirFlag, *dataDirFlag, *installServiceFlag, *startAfterJoin); err != nil {
			fmt.Fprintf(os.Stderr, "join failed: %v\n", err)
			os.Exit(1)
		}
		if !*startAfterJoin {
			return
		}
		*cfgPath = filepath.Join(*configDirFlag, "nodeagent.yaml")
	}

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
	var scannerSvc scanner.Runner
	if cfg.Scanner.Enabled && cfg.Scanner.UseRealScan {
		fallbackScanner := scanner.NewBuiltinScanner(log, scanner.Options{
			Timeout:       cfg.Scanner.Timeout,
			Shell:         cfg.Scanner.Shell,
			MaxConcurrent: cfg.Scanner.MaxConcurrent,
		})
		multiScanner := scanner.NewMultiScanner(log, fallbackScanner)
		for _, preferred := range cfg.Scanner.Preferred {
			if adapter, ok := multiScanner.GetAdapter(preferred); ok && adapter.IsAvailable() {
				log.Info("using scanner adapter", zap.String("adapter", preferred))
			}
		}
		scannerSvc = multiScanner
	} else {
		scannerSvc = scanner.NewBuiltinScanner(log, scanner.Options{
			Timeout:       cfg.Scanner.Timeout,
			Shell:         cfg.Scanner.Shell,
			MaxConcurrent: cfg.Scanner.MaxConcurrent,
		})
	}

	telemetrySvc := telemetry.New(client, log, hooksService)
	if cfg.TelemetryPrefs.CollectLogs && len(cfg.TelemetryPrefs.LogSources) > 0 {
		telemetrySvc.StartLogCollection(ctx, state.NodeID, cfg.TelemetryPrefs.LogSources)
	}
	if len(cfg.TelemetryPrefs.Triggers) > 0 {
		telemetrySvc.LoadTriggers(cfg.TelemetryPrefs.Triggers)
	}

	// ---- Phase 2/2.5/2.6/3 collectors ------------------------------------
	// Buffered eventstream is fed by procmon, netflow, fileaccess, and
	// dbquery; the batcher gzips + posts to /api/v1/events/ingest. Smart
	// filters keep cardinality bounded.
	eventStream := eventstream.NewStream(8192)
	correlator := eventstream.NewCorrelator(2 * time.Second)
	batcher := eventstream.NewBatcher(client, eventStream, log, eventstream.BatcherOptions{
		Stamp: correlator.Stamp,
	})
	go batcher.Run(ctx)

	procCollector := procmon.New(eventStream, log, procmon.Options{})
	go procCollector.Run(ctx)

	netflowMgr := netflow.NewManager(eventStream, log, netflow.Options{
		FilterCfg: netflow.FilterConfig{
			CaptureExternal:         true,
			CaptureInternalSummary:  true,
			CaptureListeningChanges: true,
			AlwaysCaptureThreat:     true,
		},
	})
	go netflowMgr.Run(ctx)

	fileMgr := fileaccess.NewManager(eventStream, log, fileaccess.Options{})
	go fileMgr.Run(ctx)

	dbMgr := dbquery.NewManager(eventStream, log, dbquery.Options{})
	go dbMgr.Run(ctx)

	// Bastion SSH tunnel — when enabled the agent listens for
	// mTLS-authenticated bastion connections and forwards bytes to local
	// sshd. Each session emits bastion.session.{open,close} events so the
	// forensic timeline cross-links to the connection rows by
	// bastion_session_id.
	if cfg.SSHTunnel.Enabled {
		emitter := bastionEmitter(eventStream, state.NodeID, "" /* tenantID populated by ingest */)
		if err := startSSHTunnel(ctx, log, sshTunnelConfig{
			ListenAddr:     cfg.SSHTunnel.ListenAddr,
			ClientCAFile:   cfg.SSHTunnel.ClientCAFile,
			ServerCertFile: cfg.SSHTunnel.ServerCertFile,
			ServerKeyFile:  cfg.SSHTunnel.ServerKeyFile,
			UpstreamAddr:   cfg.SSHTunnel.UpstreamAddr,
			EmitSession:    emitter,
		}); err != nil {
			log.Warn("ssh tunnel disabled", zap.Error(err))
		}
	}

	// Telemetry log-spike publish path — wires the spike detector through
	// the eventstream so log.spike events land in Doris/UI, not just the
	// local hooks subsystem.
	telemetrySvc.WithEventStream(eventStream)

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
		Provider:        cfg.Provisioning.Provider,
		Baselines:       cfg.Provisioning.Baselines,
		AutoRemediation: cfg.Provisioning.AutoRemediation,
	})

	complianceEngine := compliance.NewEngine(log, client, compliance.Options{
		Region:         cfg.Compliance.Region,
		RuleSets:       cfg.Compliance.RuleSets,
		Certifications: cfg.Compliance.Certifications,
		AutoApply:      cfg.Compliance.AutoApplyTemplates,
	})

	var sessionRecordingSvc *sessionrecording.Service
	if cfg.SessionRecording.Enabled {
		sessionRecordingSvc = sessionrecording.NewService(log, client, state.NodeID, sessionrecording.Config{
			Enabled:          cfg.SessionRecording.Enabled,
			Backend:          cfg.SessionRecording.Backend,
			StoragePath:      cfg.SessionRecording.StoragePath,
			RetentionDays:    cfg.SessionRecording.RetentionDays,
			MaxSizeMB:        cfg.SessionRecording.MaxSizeMB,
			Compress:         cfg.SessionRecording.Compress,
			UploadInterval:   cfg.SessionRecording.UploadInterval,
			SessionTypes:     cfg.SessionRecording.SessionTypes,
			RecordSSH:        cfg.SessionRecording.RecordSSH,
			RecordTerminal:   cfg.SessionRecording.RecordTerminal,
			RecordCommands:   cfg.SessionRecording.RecordCommands,
			TlogPath:         cfg.SessionRecording.TlogPath,
			AuditxPath:       cfg.SessionRecording.AuditxPath,
			OpenReplayAPIKey: cfg.SessionRecording.OpenReplayAPIKey,
			OpenReplayURL:    cfg.SessionRecording.OpenReplayURL,
		})
		log.Info("session recording service initialized", zap.Bool("enabled", true))
		emitHook(ctx, hooksService, log, "session_recording.initialized", state.NodeID, map[string]any{
			"backend": cfg.SessionRecording.Backend,
		})
		_ = sessionRecordingSvc
	}

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
			// Merge scheduler metadata with config and registration state
			metadata := map[string]string{
				"scheduler":    "true",
				"node_name":    cfg.NodeName,
				"generated_by": "control_one_scheduler",
			}
			for k, v := range cfg.Provisioning.Metadata {
				metadata[k] = v
			}
			if state.Metadata != nil {
				for k, v := range state.Metadata {
					metadata[k] = v
				}
			}

			if err := provEngine.ApplyTemplate(ctx, state.NodeID, metadata); err != nil {
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

	// Realtime subscriber: when the control plane emits policy.updated for this
	// tenant/node, refetch immediately instead of waiting for the polling cron.
	if state.TenantID != "" {
		sub, err := events.New(client, log, events.Options{
			TenantID: state.TenantID,
			NodeID:   state.NodeID,
			Topics:   []string{"policy.updated", "rule.triggered"},
			Handler: func(hctx context.Context, ev events.Event) {
				switch ev.Topic {
				case "policy.updated":
					if pset, err := policySyncer.FetchAndPersist(hctx, state.NodeID); err != nil {
						log.Warn("realtime policy refresh failed", zap.Error(err))
					} else {
						log.Info("realtime policy refresh", zap.Int("policies", len(pset.Policies)))
					}
				}
			},
		})
		if err != nil {
			log.Warn("event subscriber init", zap.Error(err))
		} else {
			go sub.Run(ctx)
		}
	}

	// runComplianceScan collects security facts, merges scanner results, and
	// evaluates policies against the control plane. Called on first startup and
	// on every cfg.Intervals.Scan tick thereafter.
	runComplianceScan := func() {
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
		ruleMap := make(map[string]string, len(policies)+32)
		for _, rule := range policies {
			ruleMap[rule.ID] = rule.Check
		}

		// Collect host security facts and inject them as evaluation facts.
		// The server-side JSON-DSL evaluator reads these from the policies map.
		secFacts := securityfacts.Collect(ctx)
		for k, v := range secFacts {
			ruleMap[k] = v
		}

		useRealScan := cfg.Scanner.UseRealScan && cfg.Scanner.Enabled
		if useRealScan {
			ruleMap["use_real_scan"] = "true"
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
	}

	// Run an immediate compliance scan on first startup so the node transitions
	// out of enrollment_pending quickly (doesn't wait for the first interval tick).
	go func() {
		// Brief delay so the scheduler and policy syncer are fully ready.
		time.Sleep(10 * time.Second)
		log.Info("running initial compliance scan")
		runComplianceScan()
	}()

	if cfg.Intervals.Scan > 0 {
		if _, err := sched.AddInterval("compliance-scan", cfg.Intervals.Scan, runComplianceScan); err != nil {
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

	// Control-plane heartbeat — POSTs to /api/v1/nodes/:id/heartbeat so the
	// server can bump last_seen_at and, combined with the first compliance
	// scan, transition the node out of enrollment_pending. Sprint 2 Pillar
	// 1.7/1.8. Distinct from the telemetry heartbeat above: that one writes
	// to the telemetry table; this one drives the node state machine.
	startControlPlaneHeartbeat(ctx, client, log, state.NodeID, cfg.Intervals.Heartbeat,
		makeFilterApplier(log.Named("policy"), netflowMgr, fileMgr, dbMgr),
		NewDefaultSelfUpdater())

	// DLP scanner - periodically scan for PII based on rules from control plane
	if cfg.DLP.Enabled && cfg.Intervals.DLP > 0 {
		dlpScanner := NewDLPScanner(client, log.Named("dlp"), state.NodeID, state.TenantID, cfg.DLP.ScanPaths)
		if _, err := sched.AddInterval("dlp-scan", cfg.Intervals.DLP, func() {
			if err := dlpScanner.Run(ctx); err != nil {
				log.Warn("dlp scan failed", zap.Error(err))
				emitHook(ctx, hooksService, log, "dlp.scan.failed", state.NodeID, map[string]any{"error": err.Error()})
			} else {
				emitHook(ctx, hooksService, log, "dlp.scan.completed", state.NodeID, nil)
			}
		}); err != nil {
			log.Fatal("schedule dlp scan", zap.Error(err))
		}
		// Run initial scan after 30 seconds
		go func() {
			time.Sleep(30 * time.Second)
			if err := dlpScanner.Run(ctx); err != nil {
				log.Warn("initial dlp scan failed", zap.Error(err))
			}
		}()
	}

	sched.Start()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Info("shutdown signal received")
	sched.Stop(ctx)
}

// uninstallServiceHook is wired by build-tagged service_*.go files (owned by
// Worktree A) to tear down the platform-specific service manager registration.
// It is a package-level function variable so Worktree A can override it in
// init() without Worktree B needing to modify service_*.go.
//
// The default implementation emits a notice so the CLI still succeeds on
// platforms where the service teardown hasn't shipped yet.
var uninstallServiceHook = func() error {
	fmt.Println("  Service: no OS-level uninstaller registered; skipping service teardown")
	return nil
}

// runUninstall implements the `controlone-agent uninstall` subcommand used by
// the uninstall one-liner. It:
//
//  1. Calls the platform-specific uninstallServiceHook (set by Worktree A).
//  2. Optionally POSTs /api/v1/nodes/:id/retire using the cached client cert
//     so the control plane marks the node retired + eventually revokes the cert.
//  3. Removes the local config + data directories.
func runUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	configDir := fs.String("config-dir", defaultConfigDir(), "Config directory to remove")
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory to remove")
	apiURL := fs.String("api-url", "", "Control plane URL (overrides config if set)")
	nodeID := fs.String("node-id", "", "Node ID to retire server-side (overrides state file)")
	skipRetire := fs.Bool("skip-retire", false, "Skip POSTing the retire request")
	keepConfig := fs.Bool("keep-config", false, "Do not remove the config directory")

	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Println("  Running Control One agent uninstall...")

	if err := uninstallServiceHook(); err != nil {
		fmt.Fprintf(os.Stderr, "  [warn] service teardown: %v\n", err)
	}

	if !*skipRetire {
		if err := retireNodeIfPossible(*configDir, *dataDir, *apiURL, *nodeID); err != nil {
			fmt.Fprintf(os.Stderr, "  [warn] retire request: %v\n", err)
		}
	}

	// Best-effort removal of well-known local directories.
	targets := []string{*dataDir}
	if !*keepConfig {
		targets = append(targets, *configDir)
	}

	for _, target := range targets {
		if strings.TrimSpace(target) == "" {
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			fmt.Fprintf(os.Stderr, "  [warn] remove %s: %v\n", target, err)
		} else {
			fmt.Printf("  Removed %s\n", target)
		}
	}

	fmt.Println("  Control One agent uninstall complete.")
	return nil
}

// retireNodeIfPossible loads the node_id + certs from local state and POSTs
// /api/v1/nodes/:id/retire. Any error is reported but does not fail the CLI
// — local cleanup always proceeds.
func retireNodeIfPossible(configDir, dataDir, apiURLOverride, nodeIDOverride string) error {
	nodeID := strings.TrimSpace(nodeIDOverride)
	apiURL := strings.TrimSpace(apiURLOverride)

	// Try to load node_id from state.json if not overridden.
	if nodeID == "" {
		statePath := filepath.Join(dataDir, "state.json")
		stateBytes, err := os.ReadFile(statePath) // #nosec G304 — admin-supplied dir
		if err == nil {
			var state struct {
				NodeID string `json:"node_id"`
			}
			if jErr := json.Unmarshal(stateBytes, &state); jErr == nil {
				nodeID = strings.TrimSpace(state.NodeID)
			}
		}
	}

	// Try to load api_url from nodeagent.yaml if not overridden.
	if apiURL == "" {
		configPath := filepath.Join(configDir, "nodeagent.yaml")
		if data, err := os.ReadFile(configPath); err == nil { // #nosec G304 — admin-supplied dir
			for _, line := range strings.Split(string(data), "\n") {
				trimmed := strings.TrimSpace(line)
				if !strings.HasPrefix(trimmed, "api_url:") {
					continue
				}
				value := strings.TrimSpace(strings.TrimPrefix(trimmed, "api_url:"))
				value = strings.Trim(value, `"'`)
				apiURL = value
				break
			}
		}
	}

	if nodeID == "" || apiURL == "" {
		return errors.New("missing node_id or api_url; skipping server-side retire")
	}

	certDir := filepath.Join(dataDir, "certs")
	client, err := buildMTLSClient(
		filepath.Join(certDir, "client.crt"),
		filepath.Join(certDir, "client.key"),
		filepath.Join(certDir, "ca.crt"),
	)
	if err != nil {
		return fmt.Errorf("build mTLS client: %w", err)
	}

	endpoint := strings.TrimRight(apiURL, "/") + "/api/v1/nodes/" + nodeID + "/retire"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(nil))
	if err != nil {
		return fmt.Errorf("build retire request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post retire: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("retire endpoint returned HTTP %d", resp.StatusCode)
	}
	fmt.Printf("  Control plane notified (node %s retired).\n", nodeID)
	return nil
}

func buildMTLSClient(certFile, keyFile, caCertFile string) (*http.Client, error) {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}

	pool := x509.NewCertPool()
	if data, err := os.ReadFile(caCertFile); err == nil { // #nosec G304 — admin-supplied path
		pool.AppendCertsFromPEM(data)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{pair},
			RootCAs:      pool,
		},
	}
	return &http.Client{Transport: tr, Timeout: 10 * time.Second}, nil
}

func defaultConfigDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), "ControlOne")
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "ControlOne")
		}
	}
	return "/etc/control-one"
}

func defaultDataDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), "ControlOne", "nodeagent")
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "ControlOne", "nodeagent")
		}
	}
	return "/var/lib/control-one/nodeagent"
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
