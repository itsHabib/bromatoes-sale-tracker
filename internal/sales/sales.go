package sales

import "time"

const (
	// CouchbaseScope is the Couchbase scope in which the sale records are stored
	CouchbaseScope = "nfts"

	// CouchbaseCollection is the Couchbase collection in which the sales records
	// are stored
	CouchbaseCollection = "sales"

	Twitter PublishChannel = "twitter"
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

	// Marketplace is the marketplace where the sale took place
	Marketplace string `json:"marketplace"`

	// MintPubkey is the public key of the mint account
	MintPubkey string `json:"mintPubkey"`

	// Price of the sale, in lamports
	Price uint64 `json:"price"`

	// PublishDetails communicates the details of the posting to twitter
	PublishDetails *PublishDetails `json:"publishDetails"`

	// Seller is the pubkey address of the seller of the nft
	Seller string `json:"seller"`

	// SaleTime is the time in which the sale occurred
	SaleTime *time.Time `json:"saleTime"`

	NFT NFT `json:"nft"`

	// TwitterMediaID represents the media id of the bromato PNG file.
	// This is needed to have the picture of the bromato in the tweet.
	TwitterMediaID string `json:"twitterMediaId"`
}

// NFT represents an NFT's metadata
type NFT struct {
	Name        string `json:"name"`
	Symbol      string `json:"symbol"`
	MetadataURI string `json:"metadataURI"`
}

// PublishDetails is the object that holds the information regarding the
// publishing of the sales to a certain social media platform. At the moment
// only twitter is allowed.
type PublishDetails struct {
	ID      string         `json:"id"`
	Channel PublishChannel `json:"channel"`
	Time    *time.Time     `json:"time"`
	Success bool           `json:"success"`
}

type PublishChannel string

type Marketplace string

func (m Marketplace) String() string { return string(m) }

type NFTCollection string

func FullyQualifiedCollectionName(bucket string) string {
	return "`" + bucket + "`" + "." + CouchbaseScope + "." + CouchbaseCollection
}
