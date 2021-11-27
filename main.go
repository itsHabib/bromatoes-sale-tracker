package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"time"
)

/*
   {
     "signature": "3ToM4JAoJDhQzv2aPSzogh2q63onzHwBnH93d942KtpqDon9NNMWzrg8rms21RNWjZmDjcsfd6KmFa3huHchD7TJ",
     "mintPubkey": "J6RiiaMpVFAqx2ZfE42sPbRNpT5DQ16MafdtvKp14Ruu",
     "user": "EBiZzVZDgpkipXEGs4Jk3uqDmwkMe9EkXFnqaveWY1wP",
     "tradingType": "SALE",
     "createdAt": "2021-11-26T22:54:42Z",
     "price": "2102044280",
     "toPubkey": "Fakzu1tWjyqErcgPfjHGQD7h77tFRtaKknFm9wqF8D73",
     "marketplace": "alpha.art"
   }
 */

const (
	// Sale communicates that the activity was a sale
	Sale AlphaTradingType = "SALE"

	// Listing communicates that the activity was a listing
	Listing AlphaTradingType = "LISTING"

	// Offer communicates that the activity was an offer
	Offer AlphaTradingType = "OFFER"

	// Collection is the default resource collection for an alpha art activity
	// request payload. I still have yet to see another type
	Collection  ResourceType = "COLLECTION"

	// AlphaArtAPIURL is the url of the alpha art api
	AlphaArtAPIURL = "https://apis.alpha.art"

	// AlphaArtAPIActivityURL is the url of the activity resource in the alpha
	// art api
	AlphaArtAPIActivityURL = AlphaArtAPIURL + "/api/v1/activity"

	BadBromotoesAlphaArtCollectionID = "bad-bromatoes"
)

// AlphaArtActivity represents the response type given by AlphaArts
// activity REST call. As of 11-26-21, the call to /api/v1/activity
// will return the below structure.
type AlphaArtActivity struct {
	// Signature is the sig of the transaction:w
	Signature string `json:"signature"`

	// MintPubkey is the public key of the mint account
	MintPubkey string `json:"mintPubkey"`

	// User that completed the action. In the case of SALE, this is the selling
	// user.
	User string `json:"user"`

	// TradingType is the type of action that occurred. One of SALE, LISTING, OFFER
	TradingType AlphaTradingType `json:"tradingType"`

	// CreatedAt is the time of the TradingType action
	CreatedAt time.Time `json:"createdAt"`

	// Marketplace represents where the activity took place. For sales, this
	//ca
	MarketPlace string `json:"marketplace"`

	// Price in lamports
	Price string `json:"price"`

	ToPubkey string `json:"toPubkey"`
}

type AlphaArtActivityHistory struct {
	Activities []AlphaArtActivity `json:"history"`
}
/*
{
	"id":"bad-bromatoes",
	"resourceType":"COLLECTION",
	"tradingTypes":["SALE"],
	"before":"2021-11-26T06:55:03Z",
	"limit":20,
	"noForeignListing":true,
}
 */

// AlphaArtActivityPayload represents the payload needed to query AlphaArts
// activity endpoint. Please note this is not documented and this was only
// found by inspecting the network requests that AlphaArt uses to populate its
// activity tab.
type AlphaArtActivityPayload struct {
	// ID of the collection e.g. bad-bromatoes
	ID string `json:"id"`

	// ResourceType is the resource in which the activity is for. In our
	// case, COLLECTION. As stated above, this is the only resource type
	// that has been set yet.
	ResourceType ResourceType `json:"resourceType"`

	// TradingTypes represent a list of trading types that the AlphaArt api
	// should filter by.
	TradingTypes []AlphaTradingType `json:"tradingTypes"`

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

type AlphaTradingType string

type ResourceType string

func main() {
	c := new(http.Client)

	payload := AlphaArtActivityPayload{
		ID:               BadBromotoesAlphaArtCollectionID,
		ResourceType:     Collection,
		TradingTypes:    []AlphaTradingType{Sale},
		Limit:            20,
		NoForeignListing: true,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("unable to marshal payload: %s", err)
	}
	log.Printf("body: %s", string(b))

	req, err := http.NewRequest(http.MethodPost, AlphaArtAPIActivityURL, bytes.NewReader(b))
	if err != nil {
		log.Fatalf("unable to create new activity request: %s", err)
	}
	log.Printf("url: %s", req.URL.String())

	resp,  err := c.Do(req)
	if err != nil {
		log.Fatalf("unable to get activity: %s", err)

	}

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("received non-200 status code: %d", resp.StatusCode)
	}

	if resp.Body == nil {
		log.Fatalf("received nil body from AA api")
	}

	var history AlphaArtActivityHistory
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		log.Fatalf("unable to decode body: %s", err)
	}

	sort.Slice(history.Activities, func(i, j int) bool {
		return history.Activities[i].CreatedAt.After(history.Activities[j].CreatedAt)
	})

	if len(history.Activities) == 0 {
		log.Print("no activities found")
		return
	}

	log.Printf("found %d activities, last sale: %s, price: %s", len(history.Activities), history.Activities[0].CreatedAt.String(), history.Activities[0].Price)
}
