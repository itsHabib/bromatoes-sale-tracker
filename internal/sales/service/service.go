package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"bromato-sales/internal/alphaart"
	"bromato-sales/internal/sales"
	"bromato-sales/internal/sales/reader"
	"bromato-sales/internal/sales/writer"
)

const (
	badBromotoesAlphaArtCollectionID = "bad-bromatoes"
)

type Service struct {
	logger   *zap.Logger
	aaClient *alphaart.Client
	reader   *reader.Service
	writer   *writer.Service
}

func NewService(logger *zap.Logger, r *reader.Service, w *writer.Service, aaClient *alphaart.Client) (*Service, error) {
	s := Service{
		logger:   logger,
		aaClient: aaClient,
		reader:   r,
		writer:   w,
	}

	if err := s.validate(); err != nil {
		return nil, err
	}

	s.logger.Debug("successfully initialized sales service")

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
			dep: "reader",
			chk: func() bool { return s.reader != nil },
		},
		{
			dep: "writer",
			chk: func() bool { return s.writer != nil },
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
func (s *Service) Create(rec sales.Record) (*sales.Record, error) {
	logger := s.logger.With(zap.String("salesId", rec.ID))

	now := time.Now().UTC()
	rec.CreatedAt = &now

	if err := s.writer.Create(&rec); err != nil {
		const msg = "unable to create sales record"
		s.logger.Error(msg, zap.Error(err))
		return nil, fmt.Errorf(msg+": %w", err)
	}

	logger.Debug("successfully created sales record")

	return &rec, nil
}

func (s *Service) SaveNewSales(marketplace sales.Marketplace) error {
	logger := s.logger.With(zap.String("marketplace", marketplace.String()))

	var done bool
	var before *time.Time
	var newSales []sales.Record
	// go through the marketplace sales and try to find all new sales that we
	// do not already have in the db. we continuously loop as alpha art only
	// returns 20 results at a time making it possible that there had been more
	// than 20 sales in the last iteration.
findNewSales:
	for !done {
		marketplaceSales, err := s.FetchMarketplaceSales(marketplace, before)
		if err != nil {
			const msg = "unable to fetch marketplace sales"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}

		logger.Debug("fetched marketplace sales", zap.Int("numSales", len(marketplaceSales)))

		// done
		if len(marketplaceSales) == 0 {
			break
		}

		// sort list newest to oldest, AA "should" do this
		sort.Slice(marketplaceSales, func(i, j int) bool {
			// these should really never be nil
			if marketplaceSales[i].SaleTime == nil || marketplaceSales[j].SaleTime == nil {
				return false
			}

			return marketplaceSales[i].SaleTime.After(*marketplaceSales[j].SaleTime)
		})

		// if we find a transaction that is already in our db, we are done.
		// otherwise, we keep filling up our new sales list until we hit a max
		// of 500 sales. At which we flush the list to the db and start again
		for i := range marketplaceSales {
			// check if transaction exists
			_, err := s.reader.Get(marketplaceSales[i].ID)
			switch err {
			case nil:
				// we are fully caught up with all sales
				logger.Debug("found existing sales record, done searching")
				done = true
				break findNewSales
			case sales.ErrNotFound:
				// we found a new sale
				logger.Debug("new sale found", zap.String("salesId", marketplaceSales[i].ID))
				newSales = append(newSales, marketplaceSales[i])
			default:
				const msg = "unable to get sale record"
				logger.Error(msg, zap.Error(err))
				return fmt.Errorf(msg+": %w", err)
			}
		}

		// to avoid  racking up too much memory, we flush the list
		// every 500 sales. 500 sales json encoded is about ~200KB.
		if len(newSales) > 500 {
			logger.Debug("accumulated more than 500 sales, flushing to db", zap.Int("numSales", len(newSales)))

			if err := s.flushNewSales(logger, newSales); err != nil {
				const msg = "unable to flush new sales"
				logger.Error(msg, zap.Error(err))
				return fmt.Errorf(msg+": %w", err)
			}
			// reset list
			newSales = []sales.Record{}
		}

		// set before time to the oldest sale we have
		before = marketplaceSales[len(marketplaceSales)-1].SaleTime
		logger.Debug("before time set", zap.Timep("before", before))
		logger.Debug("new sales so far", zap.Int("numSales", len(newSales)))

		// tiny pause because this isn't actually a public API, and we don't
		// want to look sus. 500 is a long time in computer time, so it should
		// be sufficient in case they ever rate limit
		time.Sleep(time.Millisecond * 500)
	}

	if len(newSales) > 0 {
		logger.Debug("new sales to write", zap.Int("numSales", len(newSales)))
		if err := s.flushNewSales(logger, newSales); err != nil {
			const msg = "unable to flush new sales"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}
	}

	return nil
}

func (s *Service) FetchMarketplaceSales(marketplace sales.Marketplace, before *time.Time) ([]sales.Record, error) {
	logger := s.logger.With(zap.String("marketplace", marketplace.String()))

	var nftSales []sales.Record
	switch marketplace {
	case sales.AlphaArt:
		params := alphaart.QueryParam{
			Limit:        20,
			TradingTypes: []alphaart.TradingType{alphaart.Sale},
			Before:       before,
		}
		history, err := s.aaClient.GetActivityHistory(badBromotoesAlphaArtCollectionID, &params)
		if err != nil {
			const msg = "unable to get alpha art activity history"
			logger.Error(msg, zap.Error(err))
			return nil, fmt.Errorf(msg+": %w", err)
		}

		for i := range history.Activities {
			s, err := FromAAActivity(history.Activities[i], badBromotoesAlphaArtCollectionID)
			// TODO: completely error out for now, prob want dont want to do that
			//  in a live environment
			if err != nil {
				const msg = "unable to create sale from aa activity"
				logger.Error(msg, zap.Error(err))
				return nil, fmt.Errorf(msg+": %w", err)
			}
			nftSales = append(nftSales, *s)
		}
	default:
		const msg = "unsupported marketplace"
		logger.Error(msg)
		return nil, fmt.Errorf(msg+": %s", marketplace.String())
	}

	return nftSales, nil
}

func (s *Service) ListSaleRecords(wheres ...*reader.Where) ([]sales.Record, error) {
	records, err := s.reader.List(wheres...)
	if err != nil {
		const msg = "unable to list sale records"
		s.logger.Error(msg, zap.Error(err))
		return nil, fmt.Errorf(msg+": %w", err)
	}

	return records, nil
}

func (s *Service) flushNewSales(logger *zap.Logger, newSales []sales.Record) error {
	logger = logger.With(zap.Int("numSales", len(newSales)))

	for i := range newSales {
		if _, err := s.Create(newSales[i]); err != nil {
			const msg = "unable to create sales record"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}
	}

	return nil
}

func FromAAActivity(activity alphaart.Activity, collection sales.NFTCollection) (*sales.Record, error) {
	price, err := strconv.Atoi(activity.Price)
	if err != nil {
		return nil, fmt.Errorf("unable to convert price in lamports to int: %w", err)
	}
	if price < 0 {
		return nil, fmt.Errorf("price cannot be negative: %d", price)
	}

	return &sales.Record{
		ID:          activity.Signature,
		Buyer:       activity.ToPubkey,
		Collection:  collection,
		Marketplace: sales.AlphaArt,
		MintPubkey:  activity.MintPubkey,
		Price:       uint64(price),
		SaleTime:    &activity.CreatedAt,
		Seller:      activity.User,
	}, nil
}
