package service

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
