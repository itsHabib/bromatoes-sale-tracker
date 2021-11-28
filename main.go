package main

import (
	"log"

	"github.com/caarlos0/env/v6"
	"github.com/couchbase/gocb/v2"
	"go.uber.org/zap"

	"bromato-sales/internal/alphaart"
	"bromato-sales/internal/sales"
	"bromato-sales/internal/sales/reader"
	"bromato-sales/internal/sales/service"
	"bromato-sales/internal/sales/writer"
)

type Config struct {
	CouchbaseEndpoint string `env:"COUCHBASE_ENDPOINT,required"`

	CouchbaseUsername string `env:"COUCHBASE_USERNAME,required"`

	CouchbasePassword string `env:"COUCHBASE_PASSWORD,required"`

	CouchbaseBucket string `env:"COUCHBASE_BUCKET,required"`
}

func main() {
	cfg, err := getConfig()
	if err != nil {
		log.Fatalf("unable to get config: %s", err)
	}

	cluster, err := getCluster(cfg)
	if err != nil {
		log.Fatalf("unable to initialize cluster: %s", err)
	}

	logger, err := zap.NewDevelopment(
		zap.WithCaller(true),
	)
	if err != nil {
		log.Fatalf("unable to initialize logger: %s", err)
	}

	svc, err := getService(logger, cluster, cfg.CouchbaseBucket)
	if err != nil {
		log.Fatalf("unable to initialize service: %s", err)
	}

	if err := svc.SaveNewSales(sales.AlphaArt); err != nil {
		log.Fatalf("unable to save new sales: %s", err)
	}
}

func getConfig() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func getService(logger *zap.Logger, cluster *gocb.Cluster, bucket string) (*service.Service, error) {
	r, err := reader.NewService(logger, cluster, bucket)
	if err != nil {
		return nil, err
	}

	w, err := writer.NewService(logger, cluster, bucket)
	if err != nil {
		return nil, err
	}

	aa, err := alphaart.NewClient(logger)
	if err != nil {
		return nil, err
	}

	svc, err := service.NewService(logger, r, w, aa)
	if err != nil {
		return nil, err
	}

	return svc, nil
}

func getCluster(cfg *Config) (*gocb.Cluster, error) {
	return gocb.Connect(
		cfg.CouchbaseEndpoint,
		gocb.ClusterOptions{
			Username: cfg.CouchbaseUsername,
			Password: cfg.CouchbasePassword,
		},
	)
}
