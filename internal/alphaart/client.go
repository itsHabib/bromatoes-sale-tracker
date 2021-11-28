package alphaart

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	badBromotoesCollectionID = "bad-bromatoes"

	httpTimeout = time.Second * 3

	// APIURL is the url of the alpha art api
	APIURL = "https://apis.alpha.art"

	// ActivityAPIURL is the url of the activity resource in the alpha
	// art api
	ActivityAPIURL = APIURL + "/api/v1/activity"
)

// Client is responsible for interacting with the Alpha Art api to retrieve
// a list of activity for a certain collection id.
type Client struct {
	logger *zap.Logger
	c      *http.Client
}

// NewClient returns an instantiated instance of a new aa client. The client
// has the following dependencies:
//
// logger - for structured logging
//
// Usage Example:
//  c, err := NewClient(logger)
//  if err != nil { // handle err }
//
//  // retrieve the bad bromatoes activity history using default parameters
//  history, err := c.GetActivityHistory("bad-bromatoes", nil)
//  if err != nil { // handle err }
//
//  // retrieve the bad bromatoes activity history limited to 2 records and only
//  // sale trading types that occurred before 11-27-2021 UTC
//  before := time.Date(2021, 11, 27, 0, 0, 0, 0, time.UTC)
//  params := QueryParams {
//    Limit: 2,
//    Before: &before,
//    TradingTypes: []TradingType{Sale},
//  }
//  history, err := c.GetActivityHistory("bad-bromatoes", &params)
//  if err != nil { // handle err }
func NewClient(logger *zap.Logger) (*Client, error) {
	if logger == nil {
		return nil, errors.New("unable to initialize a new Alpha Art client due to the missing logger depedency")
	}

	return &Client{
		logger: logger,
		c:      &http.Client{Timeout: httpTimeout},
	}, nil
}

// GetActivityHistory returns the Alpha Art activity history for a given
// collection id using the provided params.
func (c *Client) GetActivityHistory(collectionID string, params *QueryParam) (*ActivityHistory, error) {
	logger := c.logger.With(zap.String("collectionId", collectionID))

	if params == nil {
		params = defaultQueryParam()
	}

	if err := configureParams(params); err != nil {
		logger.Error("invalid params", zap.Error(err))
		return nil, err
	}

	payload := params.toActivityHistoryPayload(collectionID)

	b, err := json.Marshal(payload)
	if err != nil {
		const msg = "unable to marshal payload"
		logger.Error(msg, zap.Error(err))
		return nil, fmt.Errorf(msg+": %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, ActivityAPIURL, bytes.NewReader(b))
	if err != nil {
		const msg = "unable to marshal payload"
		logger.Error(msg, zap.Error(err))
		return nil, fmt.Errorf(msg+": %w", err)
	}

	resp, err := c.c.Do(req)
	if err != nil {
		const msg = "unable to get activity"
		logger.Error(msg, zap.Error(err))
		return nil, fmt.Errorf(msg+": %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		const msg = "received non-200 status code"
		logger.Error(msg, zap.Error(err), zap.Int("statusCode", resp.StatusCode))
		return nil, fmt.Errorf(msg+": %w", err)
	}

	var history ActivityHistory
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		log.Fatalf("unable to decode body: %s", err)
	}
	resp.Body.Close()

	return &history, nil
}

// QueryParam allows a caller to configure the get activity history call by
// setting the limit, tradingTypes, and before fields. The default parameters
// are a limit of 20, all trading types of Listing, Sale, and Offer, and
// and a nil before time.
type QueryParam struct {
	Limit        int
	TradingTypes []TradingType
	Before       *time.Time
}

func (q *QueryParam) toActivityHistoryPayload(collectionID string) activityHistoryPayload {
	return activityHistoryPayload{
		ID:               collectionID,
		ResourceType:     Collection,
		TradingTypes:     q.TradingTypes,
		Before:           q.Before,
		Limit:            q.Limit,
		NoForeignListing: true,
	}
}

func (q *QueryParam) validateTradingTypes() error {
	if len(q.TradingTypes) < 1 {
		return fmt.Errorf("at least one trading type must be supplied")
	}

	if len(q.TradingTypes) > 3 {
		return fmt.Errorf("can not have more than three trading types")
	}

	validTradingTypes := map[TradingType]struct{}{
		Sale:    {},
		Listing: {},
		Offer:   {},
	}

	var unsupportedTypes []string
	for i := range q.TradingTypes {
		if _, ok := validTradingTypes[q.TradingTypes[i]]; !ok {
			unsupportedTypes = append(unsupportedTypes, q.TradingTypes[i].String())
		}
	}

	if len(unsupportedTypes) > 0 {
		return fmt.Errorf(
			"received (%d) unsupported types: %s",
			len(unsupportedTypes),
			strings.Join(unsupportedTypes, ","),
		)
	}

	return nil
}

// activityHistoryPayload represents the payload needed to query AlphaArts
// activity endpoint. Please note this is not documented and this was only
// found by inspecting the network requests that AlphaArt uses to populate its
// activity tab.
type activityHistoryPayload struct {
	// ID of the collection e.g. bad-bromatoes
	ID string `json:"id"`

	// ResourceType is the resource in which the activity is for. In our
	// case, COLLECTION. As stated above, this is the only resource type
	// that has been set yet.
	ResourceType ResourceType `json:"resourceType"`

	// TradingTypes represent a list of trading types that the AlphaArt api
	// should filter by.
	TradingTypes []TradingType `json:"tradingTypes"`

	// Before communicates that the AlphaArt api should look for activities that
	// occure before this given time. If there is nothing provided, now time
	// is considered the default.
	Before *time.Time `json:"before"`

	// Limit is the maximum number of records that the AlphaArt api should return.
	// In practice, it seems that the maximum allowed is 20.
	Limit int `json:"limit"`

	// NoForeignListing is not yet known. There did not seem to be a difference
	// when submitting this as false/true. From the network tab, this is
	// always set true.
	NoForeignListing bool `json:"NoForeignListing"`
}

func defaultQueryParam() *QueryParam {
	return &QueryParam{
		Limit:        20,
		TradingTypes: []TradingType{Sale, Listing, Offer},
	}
}

// ConfigureParams takes in the user defined parameters and validates that
// the parameters are valid. Refer to the validate function to see the rules.
func configureParams(params *QueryParam) error {
	if params == nil {
		params = defaultQueryParam()
		return nil
	}

	if err := params.validateTradingTypes(); err != nil {
		return err
	}

	// if the before time is in the future, set it to nil. a nil before time
	// tells the AA api to use "now" time
	if params.Before != nil && params.Before.After(time.Now()) {
		params.Before = nil
	}

	// if param is set to 0, set it to 20, this is the same behavior that AA
	// does. we also ensure limit is b/w 1 and 20. AA seems to already do this
	// for us but in case they don't we want to make sure this field is ok
	if params.Limit == 0 {
		params.Limit = 20
	} else {
		params.Limit = max(min(params.Limit, 20), 1)
	}

	return nil
}

func max(i, j int) int {
	if i > j {
		return i
	}

	return j
}

func min(i, j int) int {
	if i < j {
		return i
	}

	return j
}
