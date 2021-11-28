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
)

// Record represents the sales record of the NFT
type Record struct {
	// ID of the sales record
	ID string `json:"id"`

	// Buyer is the buyer's pub key address of the nft
	Buyer string `json:"buyer"`

	// Collection communicates the NFT collection e.g. bad-bromatoes
	Collection NFTCollection `json:"collection"`

	// ImageURI is the uri of the nft image
	ImageURI string `json:"imageUri"`

	// Marketplace is the marketplace where the sale took place
	Marketplace Marketplace `json:"marketplace"`

	// MintPubkey is the public key of the mint account
	MintPubkey string `json:"mintPubkey"`

	// Price of the sale, in lamports
	Price uint64 `json:"price"`

	// Seller is the pubkey address of the seller of the nft
	Seller string `json:"seller"`

	// SaleTime is the time in which the sale occurred
	SaleTime *time.Time `json:"saleTime"`

	// TransactionSignature is the signature of the transaction that completed
	// the sale
	TransactionSignature string `json:"transactionSignature"`
}

type Marketplace string

type NFTCollection string
