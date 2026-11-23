package ports

import (
	"fmt"
	"net/http"
	"testing"
)

// A delete that finds nothing has nothing left to do, so a 404 for the resource is gone; but a 404 meaning this deployment
// does not serve the route at all is not, or a clean-up would report success for work it could never do.
func TestIsGoneSeparatesAMissingResourceFromAnUnservedRoute(t *testing.T) {
	missing := &ErrorResponse{Response: &http.Response{StatusCode: http.StatusNotFound}, Message: "Network not found"}
	if !IsGone(missing) || !IsGone(fmt.Errorf("wrapped: %w", missing)) {
		t.Error("a plain 404 is gone")
	}
	unserved := &ErrorResponse{Response: &http.Response{StatusCode: http.StatusNotFound}, Kind: ErrorKindEndpointNotServed}
	if IsGone(unserved) {
		t.Error("an unserved route is not gone")
	}
	if IsGone(&ErrorResponse{Response: &http.Response{StatusCode: http.StatusForbidden}}) || IsGone(nil) {
		t.Error("only a 404 is gone")
	}
}
