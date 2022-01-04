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
	cbTimeout = time.Second * 5
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

type Update struct {
	Field string
	Value interface{}
}

// UpdateFields updates the sales record specific fields
func (s *Service) UpdateFields(id string, updates ...Update) error {
	if len(updates) == 0 {
		return nil
	}

	logger := s.logger.With(zap.String("salesId", id))

	fqn := sales.FullyQualifiedCollectionName(s.bucket)
	stmt := "UPDATE " + fqn

	namedParams := make(map[string]interface{})
	if len(updates) > 0 {
		np := namedParamField(updates[0].Field)
		stmt += " SET " + escapeField(updates[0].Field) + " = " + np
		namedParams[np] = updates[0].Value
		updates = updates[1:]

		for i := range updates {
			np := namedParamField(updates[i].Field)
			stmt += "," + escapeField(updates[i].Field) + " = " + np
			namedParams[np] = updates[i].Value
		}
	}

	stmt += " WHERE id = $q_id LIMIT 1"
	namedParams[namedParamField("id")] = id

	logger.Debug(
		"query statement",
		zap.String("statement", stmt),
		zap.Any("params", namedParams),
	)
	opts := gocb.QueryOptions{
		Timeout:         cbTimeout,
		NamedParameters: namedParams,
		ScanConsistency: gocb.QueryScanConsistencyRequestPlus,
	}
	_, err := s.cluster.Query(stmt, &opts)
	if err != nil {
		const msg = "unable to create sales record"
		logger.Error(msg, zap.Error(err))
		return fmt.Errorf(msg+": %w", err)
	}

	logger.Debug("successfully updated sales record")

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

func escapeField(field string) string {
	return "`" + strings.Replace(field, ".", "`.`", -1) + "`"
}

func namedParamField(field string) string {
	return "$q_" + strings.Replace(field, ".", "_", -1)
}
