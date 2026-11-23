package ports

import (
	"net/http"
)

// Response wraps an HTTP response returned by the Forward API.
type Response struct {
	*http.Response
}
