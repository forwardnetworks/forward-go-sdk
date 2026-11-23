package ports

import (
	"net/http"
)

type BrowserLoginResult struct {
	Location        string         `json:"location"`
	Token           string         `json:"token,omitempty"`
	PasswordExpired bool           `json:"passwordExpired,omitempty"`
	Cookies         []*http.Cookie `json:"-"`
}
