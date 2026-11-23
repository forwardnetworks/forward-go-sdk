package domain

type CustomBanner struct {
	ID              Identifier `json:"id"`
	Enabled         bool       `json:"enabled"`
	Message         string     `json:"message"`
	BackgroundColor string     `json:"backgroundColor"`
	NetworkIDs      []string   `json:"networkIds"`
}

type CustomBannerRequest struct {
	Enabled         bool     `json:"enabled"`
	Message         string   `json:"message"`
	BackgroundColor string   `json:"backgroundColor"`
	NetworkIDs      []string `json:"networkIds"`
}
