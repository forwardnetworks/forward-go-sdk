package main

import "github.com/spf13/cobra"

// helpTopic is a page of `fwdctl help <name>`: what the SDK calls behind the
// commands (and behind anything built on the SDK) do that a method name does not
// say. Each fact here was read from Forward's source, or measured, and the doc
// comment of the SDK method it concerns carries the evidence; this is the short
// form for someone at a terminal.
type helpTopic struct{ name, short, long string }

var helpTopics = []helpTopic{
	{"tls", "Trusting a private CA instead of turning verification off", `A Forward with a self-signed or private-CA certificate should be reached with a CA bundle, not --insecure:

  fwdctl --ca-bundle ca.pem ...        (env FWD_CA_BUNDLE)

The bundle REPLACES the system roots (only its certificates are trusted) and verification stays on. In Go: forward.LoadRootCAs(path) -> Config.RootCAs. --insecure and --ca-bundle contradict each other and are refused together. --insecure is the default only for a localhost URL, and a --ca-bundle turns that default off.`},

	{"writes", "What a change to a device or a setting really does", `PutBatch REPLACES the device. ClassicDevices.PutBatch builds the stored device from the item alone (ClassicDeviceService.saveDevices) and replaces the existing one, so any field you leave out (collect, collectorId, credential 2 and 3, the BGP and logging options) resets to Forward's default. State every field on every PUT, or use Patch.

Patch and PatchMany. ClassicDevices.Patch changes only the fields it states. ClassicDevices.PatchMany applies ONE patch to many named devices in one request and one server transaction: every named device changes or none does (a bad credential, jump server or collector id fails the batch). Names match case-insensitively and an unknown name is skipped silently, so compare with List when that matters. Do not loop thousands of single PATCHes.

Clearing. A pointer field can only omit; Forward's patch fields treat an explicit JSON null as "clear". ClassicDevicePatch.Clear names the properties to send as null (reset the collector to the network default, fall back to an auto-associated credential, return an option to its default). Naming a property the patch also sets is an error.

Credentials. cliCredential2Id must name a PRIVILEGED_MODE credential (Cisco-like, Checkpoint) or a SHELL one (Avi); cliCredential3Id an EXPERT_MODE one. bgpTableType and bgpPeerType take effect only when collectBgpAdvertisements is true in the same change; Forward drops them otherwise.

Org disabled commands. OrgCollectionSettingsPatch.DisabledCommands REPLACES the whole stored map, so adding one command is read, copy, add, patch, with no concurrency guard. An empty map clears. Each entry needs a command and both version bounds. Forward's release notes date it to 26.8.3 (BUG-27961); an older build most likely answers 400 for the unknown property.

Retries and rate limits. Config.Retry is opt-in (forward.DefaultRetryPolicy() is a sensible one: five attempts, exponential backoff from 1s capped at 2 minutes, half jitter, Retry-After honored exactly). 429 and 503 are retried for any method. A failure to OBTAIN a connection (a dial error, a TLS handshake timeout: proven by net/http's connection trace) is retried for any method too, because not a byte was written; a certificate failure is not retried. Any other transport error, 502, 504 and 5xx are retried only for an idempotent method, because a replayed POST could create a second snapshot.

Forward's API rate limiter (off unless the deployment enables API_RATE_LIMITING, an on-premises setting) locks the USER out: more than MAX_API_REQ_PER_USER_PER_MIN requests in a minute (default 2000) blocks that login, every session and client sharing it, for API_RATE_LIMITER_LOCKOUT_MINUTES (default 1, at most 60). The 429 ("Rate limit exceeded") carries the remaining lockout in Retry-After; errors.Is(err, ErrRateLimited) and forward.RateLimitLockout(err) read it. Requests made during a lockout do not extend it. With a retry policy the client shares one cooldown across all its requests, so concurrent callers wait out the lockout together; a lockout longer than MaxDelay is returned to you, not slept through. Retries count against the limit, so a tight bulk loop plus retries can cause the lockout: prefer PatchMany and GetMany, which are one request.`},

	{"reads", "Reads that look complete and are not, or mean less than they say", `Paging. GET classic-devices is not paged: Forward returns every device in one response. NQE offset/limit pages ARE separate queries with no documented row order, and a sort on a column with ties is not stable either, so a repeated row can hide a missing one with the total unchanged. NQE.RunAll needs a unique KeyColumn, pins the snapshot the first page answered for, advances by the rows received (a page can be short), and fails with ErrNQEPagingInconsistent instead of returning a partial set.

Cloud coverage. Azure: an empty subscriptionIds means every discovered subscription; a discovered one with no testResults entry was never tested, not tested and failed. GCP: an empty projectIds means every project. AWS C2S Access Portal (CAP): an EMPTY awsC2SAccountInfos does NOT mean every portal account, collection fails on it (only discovery and the setup test treat empty as no filter); assumeRoleInfos Enabled is ignored for CAP; a wrong id or role matches nothing, silently. CloudAccounts.Discover lists ALL portal accounts with their roles, needs the collector online and can take up to about 20 minutes; a failed discovery is HTTP 200 with Error not NONE. CloudAccounts.NewlyDiscovered does not cover CAP.

CAP certificates. GetAWSCapCredential reports the CA bundle's count and earliest NotAfter (the first date after which the bundle stops being fully trusted, and an expired CA can fail the whole AWS collection). A bare DER CA is not loaded by Forward and reads as unreadable. The client .p12's expiry is not readable (PASSWORD_REQUIRED or PKCS12_NOT_PARSED), and Forward dates no server certificate: the CAP test returns only a test instant and an error, an expired portal certificate shows up only as a collection error with no date.

Collector metrics. Collectors.Metrics: CPU.Load (cpuLoad) is a 0 to 1 system-wide fraction (below 0 means not measured); GC.TimeSeconds (gcTimeSeconds) is CUMULATIVE since the JVM started, so take differences between samples; JVMFreeMB (jvmMemFreeMb) is within the committed heap; there are no network, thread or open-session series. An unknown collector name is a 400.

NQE explain and metrics. NQE.ExplainQuery / ExplainPath work on a SAVED library query or a workspace path, never raw text; a query with diagnostics is a 400 with them. NQE.RunMetrics ids are strings; times and durations are decoded from the epoch-millisecond wire values into time.Time and time.Duration.

Banners. Banners.Announcement is SaaS only (on-prem answers ErrEndpointNotServed). Banners.CriticalStates differs by deployment (on-prem storage states, a SaaS maintenance window); UNLICENSED, LICENSE_EXPIRED and LICENSE_NOT_STARTED_YET are derived from the license status (LicenseStatusCriticalState), not sent.

Diffs. DiffEntry reads diffType, a and b (before v0.4.13 every Diffs.Devices entry decoded empty). Diffs.ACLDevices is counts per device; Diffs.DeviceACL lists the rules.

Errors. A Spring "No endpoint ..." 404 (a route this deployment does not serve) is ErrEndpointNotServed and is checked first. Otherwise a 404 on /api/networks/{id} or a collection directly under it reads as ErrNetworkNotFound whatever its message says; deeper (a credential, a device by name) it is ErrNetworkNotFound only when the message itself says network, not and found, else a plain 404. So errors.Is(err, ErrNetworkNotFound) on a 404 from a device or collection route does not by itself prove the network is missing.`},

	{"permissions", "The permissions behind a typed 403", `A refusal arrives as Forward's own typed 403: errors.Is(err, forward.ErrPermissionDenied), and forward.MissingPermission(err) names the operation.

  OrgOperation.DEBUG_NQE               NQE.RunMetrics
  OrgOperation.DEBUG_COLLECTORS        Collectors.Metrics
  OrgOperation.VIEW_NQE_LIBRARY        NQE.ExplainQuery, NQE.ExplainPath
  NetworkOperation.VIEW_DEVICE_CREDENTIALS   CloudAccounts.GetAWSCapCredential
  NetworkOperation.MANAGE_COLLECTION_SOURCES CloudAccounts.Discover, ClassicDevices.PatchMany, device and cloud-account updates
                                             (creating a cloud account also needs EDIT_DEVICE_CREDENTIALS)
  NetworkOperation.DELETE_COLLECTION_SOURCES deleting a classic device or a cloud account
  NetworkOperation.VIEW_COLLECTION_SOURCES   ClassicDevices.GetMany, CloudAccounts.NewlyDiscovered
  OrgOperation.EDIT_ORG_COLLECTION_SETTINGS  Collectors.PatchOrganizationSettings

Most logins lack DEBUG_NQE and DEBUG_COLLECTORS; report that as a limit, not a failure. The SDK sends every request and lets Forward decide: it adds no client-side permission check except on the Admin service.`},
}

func addHelpTopics(root *cobra.Command) {
	for _, topic := range helpTopics {
		// No Run and no subcommands: cobra lists it under "Additional help topics"
		// and prints Long for `fwdctl help <name>`.
		root.AddCommand(&cobra.Command{Use: topic.name, Short: topic.short, Long: topic.long})
	}
}
