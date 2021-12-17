package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dghubble/oauth1"
	bin "github.com/gagliardetto/binary"
	token_metadata "github.com/gagliardetto/metaplex-go/clients/token-metadata"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
	"go.uber.org/zap"

	"bromato-sales/internal/sales"
	"bromato-sales/internal/sales/reader"
	"bromato-sales/internal/sales/writer"
)

const (
	badBromotoesAlphaArtCollectionID = "bad-bromatoes"
	maxChunkSizeInBytes              = 1024 * 1024
	solscanURL                       = "https://solscan.io"
)

type Service struct {
	logger       *zap.Logger
	solClient    *rpc.Client
	reader       *reader.Service
	writer       *writer.Service
	twitterToken string
}

func NewService(logger *zap.Logger, r *reader.Service, w *writer.Service, solClient *rpc.Client) (*Service, error) {
	s := Service{
		logger:       logger,
		solClient:    solClient,
		reader:       r,
		writer:       w,
		twitterToken: os.Getenv("TWITTER_TOKEN"),
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

func (s *Service) SaveNewSales(royaltyAddress string) error {
	logger := s.logger.With(zap.String("royaltyAddress", royaltyAddress))

	pk, err := solana.PublicKeyFromBase58(royaltyAddress)
	if err != nil {
		const msg = "unable to get public key from base58 string"
		logger.Error(msg, zap.Error(err))
		return fmt.Errorf(msg+": %w", err)
	}

	var until solana.Signature
	res, err := s.reader.List(reader.Condition{
		OrderBy:       "saleTime",
		SortDirection: "ASC",
		Limit:         1,
	})
	switch err {
	case nil:
		until, err = solana.SignatureFromBase58(res[0].ID)
		if err != nil {
			const msg = "unable to form signature from id"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}
	case sales.ErrNotFound:
	default:
		const msg = "unable to list oldest sale"
		logger.Error(msg, zap.Error(err))
		return fmt.Errorf(msg+": %w", err)
	}

	limit := 50
	var (
		done     bool
		backfill bool
		newSales int
		before   solana.Signature
	)

	// go through the marketplace sales and try to find all new sales that we
	// do not already have in the db. we continuously loop as alpha art only
	// returns 20 results at a time making it possible that there had been more
	// than 20 sales in the last iteration.
findNewSales:
	for !done {
		opts := rpc.GetSignaturesForAddressOpts{
			Before: before,
			Until:  until,
			Limit:  &limit,
		}
		signatures, err := s.solClient.GetSignaturesForAddressWithOpts(context.Background(), pk, &opts)
		if err != nil {
			const msg = "unable to get signatures for address"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}
		logger.Debug("fetched signatures", zap.Int("numSales", len(signatures)))

		// done
		if len(signatures) == 0 {
			break findNewSales
		}

		// if we find a transaction that is already in our db, we are done.
		// otherwise, we keep filling up our new sales list until we hit a max
		// of 500 sales. At which we flush the list to the db and start again
		for i := range signatures {
			logger := logger.With(zap.String("signature", signatures[i].Signature.String()))
			logger.Debug("processing signature")
			// check if the signature already exists in our db
			_, err := s.reader.Get(signatures[i].Signature.String())
			switch err {
			case nil:
				// could be caught up, set a before time
				logger.Debug("found existing sales record, done searching")
				break findNewSales
			case sales.ErrNotFound:
				backfill = false
			default:
				const msg = "unable to get sale record"
				logger.Error(msg, zap.Error(err))
				return fmt.Errorf(msg+": %w", err)
			}

			// get the signature transaction to ensure it was a marketplace
			// sale
			tx := new(rpc.GetTransactionResult)
			if err := s.retryRPC(func() error {
				tx, err = s.solClient.GetTransaction(context.Background(), signatures[i].Signature, nil)
				if err != nil {
					const msg = "unable to get transaction"
					logger.Error(msg, zap.Error(err), zap.String("signature", signatures[i].Signature.String()))
					return fmt.Errorf(msg+": %w", err)
				}

				return nil
			}, 3, time.Second*45); err != nil {
				fmt.Errorf("unable to get transaction: %w", err)
			}

			if tx.Meta == nil {
				logger.Warn("transaction does not have meta data")
				continue
			}

			if tx.Meta.Err != nil {
				logger.Warn("transaction has error")
				continue
			}

			keys := tx.Transaction.GetParsedTransaction().Message.AccountKeys
			m, ok := isMarketplaceSale(keys)
			if !ok {
				continue
			}

			// we found a marketplace sale, get the metadata and add to the list
			mint := tx.Meta.PostTokenBalances[0].Mint
			var pda solana.PublicKey
			s.retryRPC(func() error {
				pda, _, err = solana.FindTokenMetadataAddress(mint)
				if err != nil {
					const msg = "unable to get token metadata address"
					logger.Error(msg, zap.Error(err))
					return fmt.Errorf(msg+": %w", err)
				}

				return nil
			}, 3, time.Second*45)

			out := new(rpc.GetAccountInfoResult)
			s.retryRPC(func() error {
				out, err = s.solClient.GetAccountInfo(context.Background(), pda)
				if err != nil {
					const msg = "unable to get account info for pda"
					logger.Error(msg, zap.Error(err))
					return fmt.Errorf(msg+": %w", err)
				}
				return nil
			}, 3, time.Second*45)

			var meta token_metadata.Metadata

			dec := bin.NewBorshDecoder(out.Value.Data.GetBinary())
			if err := dec.Decode(&meta); err != nil {
				const msg = "unable to decode metadata"
				logger.Error(msg, zap.Error(err))
				return fmt.Errorf(msg+": %w", err)
			}

			saleTime := signatures[i].BlockTime.Time().UTC()
			sale := sales.Record{
				ID:          signatures[i].Signature.String(),
				Collection:  badBromotoesAlphaArtCollectionID,
				Marketplace: m,
				MintPubkey:  mint.String(),
				Price:       getPrice(tx.Meta.PreBalances[0], tx.Meta.PostBalances[0]),
				SaleTime:    &saleTime,
				NFT: sales.NFT{
					Name:        strings.Replace(meta.Data.Name, "\u0000", "", -1),
					Symbol:      strings.Replace(meta.Data.Symbol, "\u0000", "", -1),
					MetadataURI: strings.Replace(meta.Data.Uri, "\u0000", "", -1),
				},
			}
			//sale.Buyer = findBuyer(keys, tx.Meta, m)
			//sale.Seller = findSeller(keys, tx.Meta, m)

			// if the price is < .09 sol and the marketplace is solsea,
			// its prob not a sale and just something else.
			if sale.Price < 90000000 && sale.Marketplace == "Solsea" {
				continue
			}

			// we found a new sale
			logger.Debug("new sale found", zap.String("salesId", sale.ID), zap.Int("numNewSales", newSales), zap.Time("saleTime", saleTime))
			if _, err := s.Create(sale); err != nil {
				const msg = "unable to create sales record"
				logger.Error(msg, zap.Error(err))
				return fmt.Errorf(msg+": %w", err)
			}
			newSales++
			time.Sleep(time.Millisecond * 250)
		}

		// set before time to the oldest sale we have
		if !backfill {
			before = signatures[len(signatures)-1].Signature
		}
		logger.Debug("before time set", zap.Time("before", signatures[len(signatures)-1].BlockTime.Time()), zap.String("signature", signatures[len(signatures)-1].Signature.String()))
		logger.Debug("new sales so far", zap.Int("numSales", newSales))

		// pause for rate limiting
		time.Sleep(time.Second * 6)
	}

	logger.Debug("saved new sales", zap.Int("numSales", newSales))

	return nil
}

func (s *Service) PublishNewSales() error {
	oldestRes, err := s.reader.List(reader.Condition{
		Wheres: []reader.Where{
			{
				Field:    "publishDetails",
				Operator: "IS NULL",
			},
			{
				Field:    "saleTime",
				Operator: ">=",
				Value:    "2021-12-01T00:00:00Z",
			},
		},
		OrderBy:       "saleTime",
		SortDirection: "ASC",
		Limit:         1,
	})

	switch err {
	case nil:
	case sales.ErrNotFound:
		s.logger.Debug("no sales to publish")
		return nil
	default:
		const msg = "unable to get oldest sale"
		s.logger.Error(msg, zap.Error(err))
		return fmt.Errorf(msg+": %w", err)
	}

	oldest := oldestRes[0]
	logger := s.logger.With(zap.String("saleId", oldest.ID))

	logger.Debug("publishing oldest non-published sale")

	mediaID := oldest.TwitterMediaID
	if mediaID == "" {
		// get metadata
		c := new(http.Client)

		req, err := http.NewRequest(http.MethodGet, oldest.NFT.MetadataURI, nil)
		if err != nil {
			const msg = "unable to create metadata request"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}

		resp, err := c.Do(req)
		if err != nil {
			const msg = "unable to get metadata"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}

		var m map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
			const msg = "unable to decode metadata"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}
		resp.Body.Close()

		imageURI, ok := m["image"].(string)
		if !ok {
			const msg = "unable to find image uri from metadata"
			logger.Error(msg)
			return errors.New(msg)
		}

		// attempt to get the extension
		imageExt := "png"
		imageParts := strings.Split(imageURI, "?ext=")
		if len(imageParts) > 1 {
			imageExt = imageParts[1]
		}

		logger.Debug("image uri", zap.String("uri", imageURI))
		// download image
		req, err = http.NewRequest(http.MethodGet, imageURI, nil)
		if err != nil {
			const msg = "unable to create download image request"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}

		resp, err = c.Do(req)
		if err != nil {
			const msg = "unable to download image"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}

		// can chunk this when twitter api is there
		image, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			const msg = "unable to read image body"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}
		resp.Body.Close()

		logger.Debug("image size", zap.Int("size", len(image)))
		mediaID, err = s.uploadImageTwitter(logger, imageExt, image)
		if err != nil {
			const msg = "unable to upload image to twitter"
			logger.Error(msg, zap.Error(err))
			return fmt.Errorf(msg+": %w", err)
		}

		oldest.TwitterMediaID = mediaID
	}

	success := true
	id, err := s.publishSaleTweet(logger, oldest)
	if err != nil {
		const msg = "unable to publish sales tweet"
		logger.Error(msg, zap.Error(err))
		return fmt.Errorf(msg+": %w", err)
	}

	now := time.Now().UTC()
	publishDetails := sales.PublishDetails{
		ID:      id,
		Channel: sales.Twitter,
		Time:    &now,
		Success: success,
	}

	updates := []writer.Update{
		{
			Field: "twitterMediaId",
			Value: mediaID,
		},
		{
			Field: "publishDetails",
			Value: &publishDetails,
		},
	}
	if err := s.writer.UpdateFields(oldest.ID, updates...); err != nil {
		const msg = "unable to update fields to reflect twitter media id"
		logger.Error(msg, zap.Error(err))
		return fmt.Errorf(msg+": %w", err)
	}

	return nil
}

type Post struct {
	Text  string `json:"text"`
	Media Media  `json:"media"`
}

type Media struct {
	MediaIds []string `json:"media_ids"`
}

type TweetResp struct {
	TweetData TweetData `json:"data"`
}

type TweetData struct {
	ID string `json:"id"`
}

func (s *Service) publishSaleTweet(logger *zap.Logger, rec sales.Record) (string, error) {
	path := "https://api.twitter.com/2/tweets"
	saleText := "New Bromato Sale!\n" + "Name: " + rec.NFT.Name + "\n"

	// dont see the price on the transaction
	price := toSolPriceStr(rec.Price)
	if price != "" {
		saleText += "Price: " + toSolPriceStr(rec.Price) + " SOL\n"
	}

	if rec.Marketplace != "" {
		saleText += "Marketplace: " + rec.Marketplace + "\n"
	}

	if rec.SaleTime != nil {
		saleText += "Sale Time: " + rec.SaleTime.UTC().String() + "\n"
	}

	saleText += "Transaction: " + solscanURL + "/tx/" + rec.ID + "\n"
	saleText += "#Bromato"

	payload := Post{
		Text: saleText,
		Media: Media{
			MediaIds: []string{rec.TwitterMediaID},
		},
	}

	// upload image to twitter
	c := authTwitter(Credentials{
		ConsumerKey:       os.Getenv("TWITTER_CONSUMER_KEY"),
		ConsumerSecret:    os.Getenv("TWITTER_CONSUMER_SECRET"),
		AccessToken:       os.Getenv("TWITTER_ACCESS_TOKEN"),
		AccessTokenSecret: os.Getenv("TWITTER_ACCESS_TOKEN_SECRET"),
	})

	body, err := json.Marshal(payload)
	if err != nil {
		const msg = "unable to marshal tweet body"
		logger.Error(msg, zap.Error(err))
		return "", fmt.Errorf(msg+": %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		const msg = "unable to create request"
		logger.Error(msg, zap.Error(err))
		return "", fmt.Errorf(msg+": %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		const msg = "unable to post tweet"
		logger.Error(msg, zap.Error(err))
		return "", fmt.Errorf(msg+": %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if resp.StatusCode == 429 {
			epoch, err := strconv.Atoi(resp.Header["X-Rate-Limit-Reset"][0])
			if err != nil {
				const msg = "unable to convert rate limit reset to int"
				logger.Error(msg, zap.Error(err))
				return "", fmt.Errorf(msg+": %w", err)
			}
			reset := time.Unix(int64(epoch), 0)
			logger.Error(
				"rate limit, sleeping until reset",
				zap.Strings("rate-limit", resp.Header["X-Rate-Limit-Limit"]),
				zap.Strings("rate-limit-remaining", resp.Header["X-Rate-Limit-Remaining"]),
				zap.Int64("rate-limit-reset-minutes", int64(time.Until(reset).Minutes())),
			)

			time.Sleep(time.Until(reset))
		}
		if resp.Body != nil {
			b, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				const msg = "cant read body"
				logger.Error(msg, zap.Error(err))
				return "", fmt.Errorf(msg+": %w", err)
			}
			str := string(b)
			logger.Error("msg body", zap.String("body", str))
			//if strings.Contains(str, "Your media IDs are invalid") {
			//	updates := []writer.Update{
			//		{
			//			Field: "twitterMediaId",
			//			Value: "",
			//		},
			//	}
			//	if err := s.writer.UpdateFields(oldest.ID, updates...); err != nil {
			//		const msg = "unable to update fields to reflect twitter media id"
			//		logger.Error(msg, zap.Error(err))
			//		return fmt.Errorf(msg+": %w", err)
			//	}
			//}
		}
		const msg = "received non 200 response"
		logger.Error(msg, zap.Error(err), zap.Int("statusCode", resp.StatusCode))
		return "", fmt.Errorf(msg+": %w", err)
	}

	var tr TweetResp
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		const msg = "unable to decode tweet response"
		logger.Error(msg, zap.Error(err))
		return "", fmt.Errorf(msg+": %w", err)
	}

	return tr.TweetData.ID, nil
}

func (s *Service) uploadImageTwitter(logger *zap.Logger, ext string, image []byte) (string, error) {
	// upload image to twitter
	c := authTwitter(Credentials{
		ConsumerKey:       os.Getenv("TWITTER_CONSUMER_KEY"),
		ConsumerSecret:    os.Getenv("TWITTER_CONSUMER_SECRET"),
		AccessToken:       os.Getenv("TWITTER_ACCESS_TOKEN"),
		AccessTokenSecret: os.Getenv("TWITTER_ACCESS_TOKEN_SECRET"),
	})

	uploadURL, err := url.Parse("https://upload.twitter.com/1.1/media/upload.json")
	if err != nil {
		const msg = "unable to parse upload url"
		logger.Error(msg, zap.Error(err))
		return "", fmt.Errorf(msg+": %w", err)
	}
	q := uploadURL.Query()
	q.Set("command", "INIT")
	q.Set("media_type", "image/"+ext)
	q.Set("media_category", "tweet_image")
	q.Set("total_bytes", strconv.Itoa(len(image)))
	uploadURL.RawQuery = q.Encode()

	// create request
	req, err := http.NewRequest(http.MethodPost, uploadURL.String(), nil)
	if err != nil {
		const msg = "unable to create upload image request"
		logger.Error(msg, zap.Error(err))
		return "", fmt.Errorf(msg+": %w", err)
	}
	// set auth
	//req.Header.Set("Authorization", "Bearer "+s.twitterToken)

	// send request
	resp, err := c.Do(req)
	if err != nil {
		const msg = "unable to upload image"
		logger.Error(msg, zap.Error(err))
		return "", fmt.Errorf(msg+": %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.Body != nil {
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return "", fmt.Errorf("unable to read body: %w", err)
			}
			logger.Error("body", zap.String("body", string(body)))
		}
		const msg = "unable to init image"
		logger.Error(msg, zap.Int("status", resp.StatusCode))
		return "", errors.New(msg)
	}

	// decode response
	var r struct {
		MediaID string `json:"media_id_string"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		const msg = "unable to decode upload image response"
		logger.Error(msg, zap.Error(err))
		return "", fmt.Errorf(msg+": %w", err)
	}
	resp.Body.Close()

	logger.Debug("upload init start", zap.String("mediaId", r.MediaID))

	q = make(url.Values)
	q.Set("command", "APPEND")
	q.Set("media_id", r.MediaID)

	var i int
	for len(image) > 0 {
		chunk := image[:min(len(image), maxChunkSizeInBytes)]

		buf := new(bytes.Buffer)
		writer := multipart.NewWriter(buf)
		part, err := writer.CreateFormFile("media", "media")
		if err != nil {
			const msg = "unable to create form file"
			logger.Error(msg, zap.Error(err))
			return "", fmt.Errorf(msg+": %w", err)
		}
		if _, err := io.Copy(part, bytes.NewReader(chunk)); err != nil {
			const msg = "unable to copy"
			logger.Error(msg, zap.Error(err))
			return "", fmt.Errorf(msg+": %w", err)
		}
		writer.Close()

		q.Set("segment_index", strconv.Itoa(i))
		uploadURL.RawQuery = q.Encode()

		req, err = http.NewRequest(http.MethodPost, uploadURL.String(), buf)
		if err != nil {
			const msg = "unable to append image request"
			logger.Error(msg, zap.Error(err))
			return "", fmt.Errorf(msg+": %w", err)
		}
		//req.Header.Set("Authorization", "Bearer "+s.twitterToken)
		req.Header.Add("Content-Type", writer.FormDataContentType())
		req.Header.Set("Content-Length", strconv.Itoa(len(chunk)))

		resp, err := c.Do(req)
		if err != nil {
			const msg = "unable to append image"
			logger.Error(msg, zap.Error(err))
			return "", fmt.Errorf(msg+": %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			const msg = "recieved non-200 response from twitter"
			logger.Error(msg, zap.Int("status", resp.StatusCode))
			if resp.Body != nil {
				body, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					return "", fmt.Errorf("unable to read body: %w", err)
				}
				logger.Error("body", zap.String("body", string(body)))
			}
			return "", fmt.Errorf(msg+": %d", resp.StatusCode)
		}
		i++
		image = image[min(len(image), maxChunkSizeInBytes):]
	}
	logger.Debug("uploaded image", zap.String("mediaId", r.MediaID))

	// create request to finalize upload
	q = make(url.Values)
	q.Set("command", "FINALIZE")
	q.Set("media_id", r.MediaID)
	uploadURL.RawQuery = q.Encode()
	req, err = http.NewRequest(http.MethodPost, uploadURL.String(), nil)
	if err != nil {
		const msg = "unable to create finalize image upload request"
		logger.Error(msg, zap.Error(err))
		return "", fmt.Errorf(msg+": %w", err)
	}

	resp, err = c.Do(req)
	if err != nil {
		return "", fmt.Errorf("unable to finalize image upload: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.Body != nil {
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return "", fmt.Errorf("unable to read body: %w", err)
			}
			logger.Error("body", zap.String("body", string(body)))
		}
		const msg = "recieved non-200 response from twitter"
		logger.Error(msg, zap.Int("status", resp.StatusCode))
		return "", fmt.Errorf(msg+": %d", resp.StatusCode)
	}

	return r.MediaID, nil
}

type Credentials struct {
	ConsumerKey       string
	ConsumerSecret    string
	AccessToken       string
	AccessTokenSecret string
}

func authTwitter(creds Credentials) *http.Client {
	// Credentials stores all of our access/consumer tokens
	// and secret keys needed for authentication against
	// the twitter REST API.

	// Pass in your consumer key (API Key) and your Consumer Secret (API Secret)
	config := oauth1.NewConfig(creds.ConsumerKey, creds.ConsumerSecret)
	// Pass in your Access Token and your Access Token Secret
	token := oauth1.NewToken(creds.AccessToken, creds.AccessTokenSecret)

	return config.Client(oauth1.NoContext, token)
}

func isMarketplaceSale(keys []solana.PublicKey) (string, bool) {
	addressMap := map[string]string{
		"MEisE1HzehtrDpAAT8PnLHjpSSkRYakotTuJRPjTpo8":  "Magic Eden",
		"HZaWndaNWHFDd9Dhk5pqUUtsmoBCqzb1MLu3NAh1VX6B": "Alpha Art",
		"617jbWo616ggkDxvW1Le8pV38XLbVSyWY8ae6QUmGBAU": "Solsea",
		"CJsLwbP1iu5DuUikHEJnLfANgKy6stB2uFgvBBHoyxwz": "Solanart",
		"A7p8451ktDCHq5yYaHczeLMYsjRsAkzc3hCXcSrwYHU7": "Digital Eyes",
		"AmK5g2XcyptVLCFESBCJqoSfwV3znGoVYQnqEnaAZKWn": "Exchange Art",
	}

	for i := range keys {
		m, ok := addressMap[keys[i].String()]
		if ok {
			return m, true
		}
	}

	return "", false
}

func (s *Service) retryRPC(do func() error, retries int, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	var retry int

	for retry < retries {
		select {
		case <-timer.C:
			return fmt.Errorf("timeout after %s", timeout)
		default:
			err := do()
			switch err {
			case nil:
				return nil
			default:
				var rpcErr jsonrpc.RPCError
				if !strings.Contains(err.Error(), "429") {
					return fmt.Errorf("unexpected error: %d, %w", rpcErr.Code, err)
				}
				s.logger.Debug("rate limited, sleeping...")
				retry++
				time.Sleep(time.Second * 10)
			}
		}
	}

	return errors.New("error exceeded retries")
}

func findBuyer(accounts []solana.PublicKey, meta *rpc.TransactionMeta, marketplace string) string {
	switch marketplace {
	case "Magic Eden", "Digital Eyes", "Alpha Art":
		if len(accounts) > 0 {
			return accounts[0].String()
		}
		return ""
	case "Solsea":
	default:
		return ""
	}

	if len(meta.PreBalances) == 0 || len(meta.PostBalances) == 0 {
		return ""
	}

	// find one with all zeros
	for i := range meta.PreBalances {
		if meta.PreBalances[i] == 0 && meta.PostBalances[i] == 0 {
			return accounts[i].String()
		}
	}

	return ""
}

func findSeller(accounts []solana.PublicKey, meta *rpc.TransactionMeta, marketplace string) string {
	switch marketplace {
	case "Solsea":
		return accounts[0].String()
	}

	if len(meta.PreBalances) == 0 || len(meta.PostBalances) == 0 {
		return ""
	}

	// assume first balance change is that of the signer aka the buyer
	if meta.PreBalances[0] < meta.PostBalances[0] {
		return ""
	}

	amountWithFees := meta.PreBalances[0] - meta.PostBalances[0]

	// find a balance difference in which the post > pre and the amount is
	// roughly within 20% sol to account for fees
	within := (amountWithFees / 100) * 20

	for i := range meta.PreBalances {
		if meta.PostBalances[i] < meta.PreBalances[i] {
			continue
		}
		diff := amountWithFees - (meta.PostBalances[i] - meta.PreBalances[i])
		if diff <= within && len(accounts) > i {
			return accounts[i].String()
		}
	}

	return ""
}

func getPrice(pre, post uint64) uint64 {
	if pre < post {
		return post - pre
	}
	return pre - post
}

func toSolPriceStr(price uint64) string {
	if price < 100000000 {
		return ""
	}

	const lamportTensInSol = 9
	str := strconv.FormatUint(price, 10)
	arr := make([]string, len(str))
	for i := len(arr) - 1; i >= 0; i-- {
		arr[len(arr)-1-i] = string(str[i])
	}

	if len(arr) <= lamportTensInSol {
		priceStr := strings.Join(reverseArr(append(arr, "."+strings.Repeat("0", lamportTensInSol-len(arr)))), "")
		if len(priceStr) > 0 && priceStr[0] == '.' {
			return "0" + priceStr
		}
	}

	for i := range arr {
		if i == lamportTensInSol-1 {
			arr = append(append(arr[:i], "."), arr[i+1:]...)
			break
		}
	}

	return strings.Join(reverseArr(arr), "")
}

func reverseArr(arr []string) []string {
	r := make([]string, len(arr))
	for i := len(arr) - 1; i >= 0; i-- {
		r[len(arr)-1-i] = arr[i]
	}

	return r
}

func min(i, j int) int {
	if i < j {
		return i
	}

	return j
}

func na(str string) string {
	if str == "" {
		return "NA"
	}

	return str
}
