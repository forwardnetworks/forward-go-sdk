package ports

import (
	"net/http"
)

type ForwardHTTPDocument struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}
