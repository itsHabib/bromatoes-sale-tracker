package writer

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/couchbase/gocb/v2"
	"go.uber.org/zap"

	"bromato-sales/internal/sales"
)

const (
	cbTimeout = time.Second * 3
)

// Service is responsible for performing write operations on the nfts.sales
// collection. We use a separate reader service to avoid commingling read/writes
type Service struct {
	bucket     string
	cluster    *gocb.Cluster
	collection *gocb.Collection
	logger     *zap.Logger
}

func NewService(logger *zap.Logger, cluster *gocb.Cluster, bucket string) (*Service, error) {
	s := Service{
		bucket:  bucket,
		cluster: cluster,
		logger:  logger,
	}

	if err := s.validate(); err != nil {
		return nil, err
	}

	if err := s.setCollection(); err != nil {
		return nil, fmt.Errorf("unable to set collection: %w", err)
	}

	return &s, nil
}

func (s *Service) validate() error {
	var missingDeps []string

	for _, tc := range []struct {
		dep string
		chk func() bool
	}{
		{
			dep: "logger",
			chk: func() bool { return s.logger != nil },
		},
		{
			dep: "cluster",
			chk: func() bool { return s.cluster != nil },
		},
		{
			dep: "bucket",
			chk: func() bool { return s.bucket != "" },
		},
	} {
		if !tc.chk() {
			missingDeps = append(missingDeps, tc.dep)
		}
	}

	if len(missingDeps) > 0 {
		return fmt.Errorf(
			"unable to initialize service due to (%d) missing dependencies: %s",
			len(missingDeps),
			strings.Join(missingDeps, ","),
		)
	}

	return nil
}

// Create the sales record
func (s *Service) Create(record *sales.Record) error {
	if record == nil {
		const msg = "unable to create record: record is nil"
		s.logger.Error(msg)
		return errors.New(msg)
	}

	logger := s.logger.With(zap.String("salesId", record.ID))

	opts := gocb.InsertOptions{
		DurabilityLevel: gocb.DurabilityLevelNone,
		Timeout:         cbTimeout,
	}
	_, err := s.collection.Insert(record.ID, record, &opts)
	if err != nil {
		const msg = "unable to create sales record"
		logger.Error(msg, zap.Error(err))
		return fmt.Errorf(msg+": %w", err)
	}

	logger.Debug("successfully created sales record")

	return nil
}

func (s *Service) setCollection() error {
	bucket := s.cluster.Bucket(s.bucket)
	if err := bucket.WaitUntilReady(cbTimeout, nil); err != nil {
		return fmt.Errorf("unable to wait for bucket to be ready: %w", err)
	}

	s.collection = bucket.Scope(sales.CouchbaseScope).Collection(sales.CouchbaseCollection)

	return nil
}
