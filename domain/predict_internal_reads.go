package domain

import (
	"time"
)

// ProtocolModelingReadTimeout is the HTTP timeout the protocol-modeling blob and partitions reads use when the client's own timeout is
// shorter. Forward queues these as a search task that may wait up to 15 minutes, and builds the whole answer before sending a byte.
const ProtocolModelingReadTimeout = 20 * time.Minute

// ChangeSetSubsetOptions tunes ChangeSetSubset. Nil fields leave Forward's defaults (3 iterations, a threshold of 200 devices).
type ChangeSetSubsetOptions struct {
	NumIterations *int
	Threshold     *int
}
