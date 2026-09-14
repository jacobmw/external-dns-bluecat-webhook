package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	webhookapi "sigs.k8s.io/external-dns/provider/webhook/api"

	"github.com/jacobmw/external-dns-bluecat-webhook/internal/config"
	bluecatprovider "github.com/jacobmw/external-dns-bluecat-webhook/internal/provider"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	cfg, err := config.Load(args)
	if err != nil {
		return err
	}

	level, err := log.ParseLevel(cfg.LogLevel)
	if err != nil {
		return err
	}
	log.SetLevel(level)
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	p, err := bluecatprovider.New(ctx, cfg)
	if err != nil {
		return err
	}

	health := &http.Server{
		Addr:              cfg.HealthAddress,
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" && r.URL.Path != "/readyz" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}),
	}
	go func() {
		log.Infof("health server listening on %s", cfg.HealthAddress)
		if err := health.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("health server: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = health.Shutdown(shutdownCtx)
	}()

	log.Infof("webhook API listening on %s", cfg.ListenAddress)
	webhookapi.StartHTTPApi(p, nil, cfg.ReadTimeout, cfg.WriteTimeout, cfg.ListenAddress)
	return nil
}
