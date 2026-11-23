// Package usecases holds the SDK's use cases (the v0.9 hexagonal split): one struct per API family that embeds its port,
// plus the composites that combine one family's calls or several families' (Diffs.MaterialSummary, Snapshots.Collect), the
// generic Poller and the resumable operations. It imports only domain and ports; Wire builds every family over one client's
// ports.
package usecases
