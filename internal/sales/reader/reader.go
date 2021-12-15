package reader

import (
	"errors"
	"fmt"
	"strconv"
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

// Get returns a sales record by its transaction signature id
func (s *Service) Get(id string) (*sales.Record, error) {
	logger := s.logger.With(zap.String("saledId", id))

	opts := gocb.GetOptions{
		Timeout: cbTimeout,
	}
	result, err := s.collection.Get(id, &opts)
	if err != nil {
		if errors.Is(err, gocb.ErrDocumentNotFound) {
			return nil, sales.ErrNotFound
		}
		const msg = "unable to get record"
		logger.Error(msg, zap.Error(err))
		return nil, fmt.Errorf(msg+": %w", err)
	}

	var rec sales.Record
	if err := result.Content(&rec); err != nil {
		const msg = "unable to unmarshal content into sales.Record"
		logger.Error(msg, zap.Error(err))
		return nil, fmt.Errorf(msg+": %w", err)
	}

	return &rec, nil
}

func (s *Service) List(condition Condition) ([]sales.Record, error) {
	options := gocb.QueryOptions{
		ScanConsistency: gocb.QueryScanConsistencyRequestPlus,
		Timeout:         cbTimeout,
	}

	fqn := sales.FullyQualifiedCollectionName(s.bucket)
	stmt := "SELECT x.* FROM " + fqn + " x"

	// add where clauses to statement
	if len(condition.Wheres) > 0 {
		params := make(map[string]interface{}, len(condition.Wheres))

		stmt += " WHERE " + escapeField(condition.Wheres[0].Field) + " " + condition.Wheres[0].Operator
		if condition.Wheres[0].Operator != "IS NULL" && condition.Wheres[0].Operator != "IS NOT NULL" {
			stmt += " " + namedParamField(condition.Wheres[0].Field)
		}

		params[namedParamField(condition.Wheres[0].Field)] = condition.Wheres[0].Value

		wheres := condition.Wheres[1:]
		for i := range wheres {
			n := namedParamField(wheres[i].Field)
			stmt += " AND " + escapeField(wheres[i].Field) + " " + wheres[i].Operator
			if wheres[i].Operator != "IS NULL" && wheres[i].Operator != "IS NOT NULL" {
				stmt += " " + n
			}
			params[n] = wheres[i].Value
		}
		options.NamedParameters = params
	}

	if condition.OrderBy != "" {
		stmt += " ORDER BY " + escapeField(condition.OrderBy)
		if condition.SortDirection != "" {
			stmt += " " + condition.SortDirection
		}
	}

	if condition.Limit > 0 {
		stmt += " LIMIT " + strconv.Itoa(condition.Limit)
	}

	s.logger.Debug("query statement", zap.String("statement", stmt), zap.Any("params", options.NamedParameters))
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

type Condition struct {
	Wheres        []Where
	OrderBy       string
	SortDirection string
	Limit         int
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
