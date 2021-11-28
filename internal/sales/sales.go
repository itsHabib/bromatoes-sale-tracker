package sales

import "time"

const (
	// CouchbaseScope is the Couchbase scope in which the sale records are stored
	CouchbaseScope = "nfts"

	// CouchbaseCollection is the Couchbase collection in which the sales records
	// are stored
	CouchbaseCollection = "sales"

	// AlphaArt represents the Alpha Art marketplace
	AlphaArt Marketplace = "alpha-art"

	// MagicEden represents the Alpha Art marketplace
	MagicEden Marketplace = "magic-eden"

	// DigitalEyes represents the Digital Eyes marketplace
	DigitalEyes Marketplace = "digital-eyes"

	Twitter PublishChannel = "twitter"

	Discord PublishChannel = "discord"
)

// Record represents the sales record of the NFT
type Record struct {
	// ID of the sales record. The ID is the transaction signature in which the
	// sales occurred
	ID string `json:"id"`

	// Buyer is the buyer's pub key address of the nft
	Buyer string `json:"buyer"`

	// Collection communicates the NFT collection e.g. bad-bromatoes
	Collection NFTCollection `json:"collection"`

	// CreatedAt is the time at which the sales record was created. Not to be
	//confused with the SaleTime which is the time of sale.
	CreatedAt *time.Time `json:"createdAt"`

	// ImageURI is the uri of the nft image
	ImageURI string `json:"imageUri"`

	// Marketplace is the marketplace where the sale took place
	Marketplace Marketplace `json:"marketplace"`

	// MintPubkey is the public key of the mint account
	MintPubkey string `json:"mintPubkey"`

	// Price of the sale, in lamports
	Price uint64 `json:"price"`

	// PublishDetails communicates the details of the posting to different
	// social media channels. e.g. twitter, discord
	PublishDetails []PublishDetails `json:"publishDetails"`

	// Seller is the pubkey address of the seller of the nft
	Seller string `json:"seller"`

	// SaleTime is the time in which the sale occurred
	SaleTime *time.Time `json:"saleTime"`
}

type PublishDetails struct {
	Channel PublishChannel `json:"channel"`
	Time    *time.Time     `json:"time"`
}

type PublishChannel string

type Marketplace string

func (m Marketplace) String() string { return string(m) }

type NFTCollection string
