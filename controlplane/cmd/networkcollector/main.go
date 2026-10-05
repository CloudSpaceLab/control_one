package main

import (
	"context"
	"flag"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkcollector"
	"go.uber.org/zap"
	"os/signal"
	"syscall"
)

func main() {
	path := flag.String("config", "controlplane/config/networkcollector.example.yaml", "site collector configuration")
	flag.Parse()
	log, _ := zap.NewProduction()
	defer log.Sync()
	cfg, err := networkcollector.Load(*path)
	if err != nil {
		log.Fatal("load network collector configuration", zap.Error(err))
	}
	collector, err := networkcollector.New(cfg)
	if err != nil {
		log.Fatal("configure network collector", zap.Error(err))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	collector.Run(ctx, func(err error) { log.Warn("network collector cycle failed", zap.Error(err)) })
}
