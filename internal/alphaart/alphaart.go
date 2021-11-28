package alphaart

import (
	"time"
)

const (

	// Sale communicates that the activity was a sale
	Sale TradingType = "SALE"

	// Listing communicates that the activity was a listing
	Listing TradingType = "LISTING"

	// Offer communicates that the activity was an offer
	Offer TradingType = "OFFER"

	// Collection is the default resource collection for an alpha art activity
	// request payload. I still have yet to see another type
	Collection ResourceType = "COLLECTION"
)

// ActivityHistory represents the response type given by AlphaArts activity
// REST call. As of 11-26-21, the call to /api/v1/activity will return the
// below structure.
type ActivityHistory struct {
	Activities []Activity `json:"history"`
}

// Activity represents the singular activity record that Alpha Art
// returns in their API. Please note
type Activity struct {
	// Signature is the sig of the transaction
	Signature string `json:"signature"`

	// MintPubkey is the public key of the mint account
	MintPubkey string `json:"mintPubkey"`

	// User that completed the action. In the case of SALE, this is the selling
	// user.
	User string `json:"user"`

	// TradingType is the type of action that occurred. One of SALE, LISTING, OFFER
	TradingType TradingType `json:"tradingType"`

	// CreatedAt is the time of the TradingType action
	CreatedAt time.Time `json:"createdAt"`

	// Marketplace represents where the activity took place. For sales, this
	//ca
	MarketPlace string `json:"marketplace"`

	// Price in lamports
	Price string `json:"price"`

	ToPubkey string `json:"toPubkey"`
}

// TradingType communicates the type of activity, can be one of SALE, LISTING,
// or OFFER
type TradingType string

func (t TradingType) String() string { return string(t) }

// ResourceType is the type of resource the activity is for. I have not seen
// anything but "COLLECTION" for this field.
type ResourceType string
