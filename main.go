package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v6"
	"github.com/couchbase/gocb/v2"
	"github.com/gagliardetto/solana-go/rpc"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

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

	ctx, cancel := context.WithCancel(context.Background())
	g, gctx := errgroup.WithContext(ctx)

	// handle interrupts
	g.Go(func() error {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)

		for {
			select {
			case <-gctx.Done():
				return nil
			case <-c:
				const waitShutdown = time.Second * 2
				cancel()
				time.Sleep(waitShutdown)
				os.Exit(0)
				return nil
			}
		}
	})

	g.Go(func() error {
		return run(gctx, logger, svc)
	})

	if err := g.Wait(); err != nil {
		log.Fatalf("erorr wating for go routines to finish")
	}

}

func run(ctx context.Context, logger *zap.Logger, svc *service.Service) error {
	g, _ := errgroup.WithContext(ctx)

	// save new sales
	g.Go(func() error {
		ticker := time.NewTicker(time.Second * 30)

		for {
			select {
			case <-ticker.C:
				if err := svc.SaveNewSales("5ufx3eajnjPMvVbqT3hiEs7ur2ubwto4Hjr4UCTUbu7n"); err != nil {
					logger.Error("unable to save new sales", zap.Error(err))
				}
			}
		}
	})

	// publish new sales
	g.Go(func() error {
		ticker := time.NewTicker(time.Second * 15)
		for {
			select {
			case <-ticker.C:
				if err := svc.PublishNewSales(); err != nil {
					logger.Error("unable to publish new sales")
				}
			}
		}
	})

	if err := g.Wait(); err != nil {
		return fmt.Errorf("error waiting for go routines to finish")
	}

	return nil
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

	svc, err := service.NewService(logger, r, w, rpc.New(rpc.MainNetBeta_RPC))
	if err != nil {
		return nil, err
	}

	return svc, nil
}

func getCluster(cfg *Config) (*gocb.Cluster, error) {
	b, err := ioutil.ReadFile("trialcert.cer")
	if err != nil {
		return nil, fmt.Errorf("unable to get cluster: %w", err)
	}

	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	if ok := rootCAs.AppendCertsFromPEM(b); !ok {
		return nil, fmt.Errorf("unable to append certs to pool: %w", err)
	}

	c, err := gocb.Connect(
		"couchbases://"+cfg.CouchbaseEndpoint+"?ssl=no_verify",
		gocb.ClusterOptions{
			Username: cfg.CouchbaseUsername,
			Password: cfg.CouchbasePassword,
			SecurityConfig: gocb.SecurityConfig{
				TLSRootCAs:    rootCAs,
				TLSSkipVerify: true,
			},
		},
	)
	if err := c.WaitUntilReady(time.Second*5, nil); err != nil {
		return nil, fmt.Errorf("unable to wait until cluster ready: %w", err)
	}

	return c, nil
}
