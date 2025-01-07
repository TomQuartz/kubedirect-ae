package util

import (
	"log"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

func SetupLogger(debug bool) {
	cfg := zap.NewDevelopmentConfig()
	cfg.DisableStacktrace = true
	if !debug {
		cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	zapLogger, err := cfg.Build()
	if err != nil {
		log.Fatalf("Error creating zap logger: %v", err)
	}
	logger := zapr.NewLogger(zapLogger)
	ctrl.SetLogger(logger)
	klog.SetLogger(logger)
}
