package forwardtest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

// NQE answers POST /api/nqe with rows, paged the way Forward pages: queryOptions.offset and queryOptions.limit select the slice and
// totalNumItems is always the whole count. A request without a limit gets every row. snapshotID is echoed as the answer's snapshotId,
// which RunAll pins later pages to. Sorting is not simulated: rows come back in the order given, so give them in key order.
func (s *Server) NQE(snapshotID string, rows []map[string]any) {
	s.HandleFunc(http.MethodPost, "/api/nqe", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			QueryOptions struct {
				Offset *int `json:"offset"`
				Limit  *int `json:"limit"`
			} `json:"queryOptions"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &request)
		start, end := 0, len(rows)
		if request.QueryOptions.Offset != nil && *request.QueryOptions.Offset < end {
			start = *request.QueryOptions.Offset
		} else if request.QueryOptions.Offset != nil {
			start = end
		}
		if request.QueryOptions.Limit != nil && start+*request.QueryOptions.Limit < end {
			end = start + *request.QueryOptions.Limit
		}
		JSON(http.StatusOK, map[string]any{"snapshotId": snapshotID, "items": rows[start:end], "totalNumItems": len(rows)}).write(w)
	})
}

// CheckDiffs answers GET /api/diffs/{a}/{b}/checks for one check type: the NQE type returns PROCESSING rows for the first
// processingPolls reads (Forward evaluates NQE checks asynchronously and says so), then the final rows. Other types are computed in
// Forward before it answers, so give processingPolls 0 for them.
func (s *Server) CheckDiffs(a, b, checkType string, processingPolls int, final []forward.CheckDiffEntry) {
	replies := make([]Reply, 0, processingPolls+1)
	for i := 0; i < processingPolls; i++ {
		replies = append(replies, JSON(http.StatusOK, []forward.CheckDiffEntry{{DiffType: forward.CheckDiffProcessing}}))
	}
	replies = append(replies, JSON(http.StatusOK, final))
	s.HandleQuery(http.MethodGet, "/api/diffs/"+a+"/"+b+"/checks", url.Values{"type": {checkType}}, replies...)
}

// BGPDiff is one row of the staged-BGP diffs view (BgpAdvertisementDiffInfo). Build it with the BGP* constructors.
type BGPDiff = forward.BGPAdvertisementDiff

func rawJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic("forwardtest: " + err.Error())
	}
	return raw
}

// BGPInjected is a staged advertisement whose key the base snapshot does not have.
func BGPInjected(added forward.BGPAdvertisement) BGPDiff {
	return BGPDiff{Change: string(forward.BGPChangeInjected), UpdateAdded: rawJSON(added)}
}

// BGPModified is a staged advertisement that replaces a received one with the same key.
func BGPModified(original, added forward.BGPAdvertisement) BGPDiff {
	return BGPDiff{Change: string(forward.BGPChangeModified), Original: rawJSON(original), UpdateAdded: rawJSON(added)}
}

// BGPDuplicated is a staged advertisement identical to the received one: it changes nothing.
func BGPDuplicated(original, added forward.BGPAdvertisement) BGPDiff {
	return BGPDiff{Change: string(forward.BGPChangeDuplicated), Original: rawJSON(original), UpdateAdded: rawJSON(added)}
}

// BGPWithdrawn is a withdrawal that removed a received advertisement.
func BGPWithdrawn(original forward.BGPAdvertisement) BGPDiff {
	key := forward.BGPAdvertisementKey{VRF: original.VRF, ExternalPeer: original.ExternalPeer, Prefix: original.Prefix}
	return BGPDiff{Change: string(forward.BGPChangeWithdrawn), Original: rawJSON(original), UpdateRemoved: rawJSON(key)}
}

// BGPAbsent is a withdrawal of a key the base snapshot does not have. Forward does not refuse it when staged: it shows up only as this
// row, and the change set ignores the key.
func BGPAbsent(key forward.BGPAdvertisementKey) BGPDiff {
	return BGPDiff{Change: string(forward.BGPChangeAbsent), UpdateRemoved: rawJSON(key)}
}

// BGPDiffs answers the staged-BGP diffs view (GET .../change-sets/{changeSetID}/draft/devices/{device}/bgp-advertisements?view=diffs)
// with the given rows. The change-set routes are Forward's ChangeSetController; the row shape is from its source
// (BgpAdvertisementDiffInfo) and is not recorded live.
func (s *Server) BGPDiffs(networkID, changeSetID, device string, diffs ...BGPDiff) {
	if diffs == nil {
		diffs = []BGPDiff{}
	}
	s.HandleQuery(http.MethodGet, "/api/networks/"+networkID+"/change-sets/"+changeSetID+"/draft/devices/"+device+"/bgp-advertisements",
		url.Values{"view": {"diffs"}}, JSON(http.StatusOK, diffs))
}

// BGPStaging accepts the stage, withdraw and restore POSTs on a device's draft the way Forward does, so code that stages and then reads
// the diffs back runs end to end: ?action=add, bulkAdd, remove and bulkRemove answer 200 with an empty draft; restore and bulkRestore
// answer 204. Each can be overridden by a more specific HandleQuery. The posted bodies are in Requests.
func (s *Server) BGPStaging(networkID, changeSetID, device string) {
	path := "/api/networks/" + networkID + "/change-sets/" + changeSetID + "/draft/devices/" + device + "/bgp-advertisements"
	for _, action := range []string{"add", "bulkAdd", "remove", "bulkRemove"} {
		s.HandleQuery(http.MethodPost, path, url.Values{"action": {action}}, JSON(http.StatusOK, map[string]any{}))
	}
	for _, action := range []string{"restore", "bulkRestore"} {
		s.HandleQuery(http.MethodPost, path, url.Values{"action": {action}}, NoContent())
	}
}

// ReceivedBGP answers the received-advertisements read (GET /api/networks/{networkID}/devices/{device}/bgp-advertisements) with
// the rows for each vrf and prefix: a prefix not in the map answers an empty list, as Forward does for one with no advertisements. The
// prefix is matched as the client sent it; Forward writes a host route back bare ("10.0.0.2", no /32).
func (s *Server) ReceivedBGP(networkID, device string, rows map[[2]string][]forward.BGPAdvertisement) {
	s.HandleFunc(http.MethodGet, "/api/networks/"+networkID+"/devices/"+device+"/bgp-advertisements", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		found := rows[[2]string{query.Get("vrf"), strings.TrimSpace(query.Get("prefix"))}]
		if found == nil {
			found = []forward.BGPAdvertisement{}
		}
		JSON(http.StatusOK, found).write(w)
	})
}
