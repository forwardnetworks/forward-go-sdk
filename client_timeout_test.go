package forward

import "time"

// testClientTimeout bounds every root test client's requests. It was the client's own default timeout, which now belongs to the
// HTTP adapter; a test client needs only some bound, so the root tests keep their own.
const testClientTimeout = 60 * time.Second
