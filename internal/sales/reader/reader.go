package reader

import (
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

// Service is responsible for performing read operations on the nfts.sales
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

func (s *Service) List(wheres ...*Where) ([]sales.Record, error) {
	options := gocb.QueryOptions{
		ScanConsistency: gocb.QueryScanConsistencyRequestPlus,
		Timeout:         cbTimeout,
	}

	fqn := s.fullyQualifiedCollectionName()
	stmt := "SELECT * FROM " + fqn

	// add where clauses to statement
	if len(wheres) > 0 {
		params := make(map[string]interface{}, len(wheres))

		stmt += " WHERE " + escapeField(wheres[0].Field) + " " + wheres[0].Operator + namedParamField(wheres[0].Field)
		params[namedParamField(wheres[0].Field)] = wheres[0].Value

		wheres := wheres[1:]
		for i := range wheres {
			n := namedParamField(wheres[i].Field)
			stmt += " AND " + escapeField(wheres[i].Field) + " " + wheres[i].Operator + n
			params[n] = wheres[i].Value
		}
		options.NamedParameters = params
	}

	res, err := s.cluster.Query(stmt, &options)
	if err != nil {
		const msg = "unable to query collection"
		s.logger.Error(msg, zap.Error(err))
		return nil, fmt.Errorf(msg+": %w", err)
	}

	var records []sales.Record
	for res.Next() {
		var rec sales.Record
		if err := res.Row(&rec); err != nil {
			const msg = "unable to unmarshal record"
			s.logger.Error(msg, zap.Error(err))
			return nil, fmt.Errorf(msg+": %w", err)
		}
		records = append(records, rec)
	}

	if len(records) == 0 {
		return nil, sales.ErrNotFound
	}

	return records, nil
}

func (s *Service) setCollection() error {
	bucket := s.cluster.Bucket(s.bucket)
	if err := bucket.WaitUntilReady(cbTimeout, nil); err != nil {
		return fmt.Errorf("unable to wait for bucket to be ready: %w", err)
	}

	s.collection = bucket.Scope(sales.CouchbaseScope).Collection(sales.CouchbaseCollection)

	return nil
}

func (s *Service) fullyQualifiedCollectionName() string {
	return "`" + s.bucket + "`" + "." + sales.CouchbaseScope + "." + sales.CouchbaseCollection
}

type Where struct {
	Field    string
	Value    interface{}
	Operator string
}

func escapeField(field string) string {
	return "`" + strings.Replace(field, ".", "`.`", -1) + "`"
}

func namedParamField(field string) string {
	return "$" + strings.Replace(field, ".", "_", -1)
}
