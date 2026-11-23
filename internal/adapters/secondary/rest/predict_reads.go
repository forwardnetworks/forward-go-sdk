package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Predict reads that were missing (ChangeSetController, FirewallPredictController). All are Preview: not in the published OpenAPI.

// PredictOSes lists the vendor operating systems Predict has a command grammar or editor for (GET /api/predict-oses): the eight in
// PredictCommandOSes plus "pan_os" (edited as security rules) on current Forward, which is why it has more names than that list. It needs only the organization property
// PREDICT_MODELING, no network permission, and it is not scoped to a network.
func (s *Predict) PredictOSes(ctx context.Context) ([]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := listResponse[string]{Keys: []string{"oses"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/predict-oses", nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.firewallDo(markOperation(req, "Predict.PredictOSes"), &out)
	if err != nil {
		return nil, resp, err
	}
	sort.Strings(out.Items)
	return out.Items, resp, nil
}

// AllowedCommands returns, as text, the commands Predict accepts for one protocol on one vendor OS (GET /api/predict-oses/{vendorOs}/
// allowed-commands?protocol=): Forward's rendering of its mode trie. An OS Predict does not model answers an empty string, not an
// error. vendorOS is a name from PredictOSes, lowercase as Forward sends it; a name Forward does not know is a 400.
//
// The empty answer is normal for PAN-OS and Junos: neither has a command tree (PAN-OS changes are security rules; Junos is checked in
// code, not by a grammar).
//
// Forward's own code fails (500) for every protocol on the Cisco-like OSes (cisco_ios, cisco_ios_xe, cisco_ios_xr, cisco_nxos, cisco_asa
// and arista_eos): only juniper_srx answers. Reproduced against Forward's source at fe0daf63c899: ModeTriePrettyPrinter.render walks each
// OS's command tree without a cycle guard, and those trees loop where a command takes an unbounded list (EOS "ip community-list standard
// <name> deny <asn>:<value> ...", IOS "access-list <n> <n> deny ... any any ...", NX-OS "ip as-path access-list <name> deny (<string>)*
// ..."), so the walk never ends and the request dies with a StackOverflowError. Nothing the caller sends changes it, whatever the protocol.
// Bounds: reproduced by running that code on the real trees (EOS, IOS, IOS-XE, IOS-XR, NX-OS, ASA, SRX; BGP, OSPF and CONFIG on each,
// every protocol on EOS). ModeTriePrettyPrinter and all the tree files are byte-identical at primary 5f7e4ec1841, stable b1feb6fe1c34
// and fe0daf63c899, so every build this SDK targets has it. Not run: the Forward service itself, and the other protocols on the other
// OSes. Only Forward can fix it (a visited set or depth cap in render).
// Any 500 is returned as ErrAllowedCommandsUnavailable, which also wraps the underlying error. juniper_srx's BGP answer is about 19 MB.
func (s *Predict) AllowedCommands(ctx context.Context, vendorOS string, protocol ports.PredictProtocol) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	if vendorOS = strings.TrimSpace(vendorOS); vendorOS == "" {
		return "", nil, errors.New("forward: vendor OS is required")
	}
	switch protocol {
	case ports.PredictProtocolBGP, ports.PredictProtocolOSPF, ports.PredictProtocolISIS, ports.PredictProtocolEIGRP, ports.PredictProtocolRIP, ports.PredictProtocolConfig, ports.PredictProtocolOMP, ports.PredictProtocolSTP:
	default:
		return "", nil, fmt.Errorf("forward: unknown Predict protocol %q", protocol)
	}
	path := "/api/predict-oses/" + url.PathEscape(vendorOS) + "/allowed-commands?" + url.Values{"protocol": []string{string(protocol)}}.Encode()
	text, resp, err := s.textGet(ctx, path, "Predict.AllowedCommands")
	if err != nil && ports.IsStatus(err, http.StatusInternalServerError) {
		err = fmt.Errorf("%w: %w", ports.ErrAllowedCommandsUnavailable, err)
	}
	return text, resp, err
}

// PrettifyCommands returns the commands reformatted the way Forward would store them for the device (POST .../change-sets/{id}/devices/
// {device}/commands?action=prettify). It changes nothing in the change set. The body is sent as plain text.
func (s *Predict) PrettifyCommands(ctx context.Context, networkID, changeSetID, deviceName, commands string) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(commands) == "" {
		return "", nil, errors.New("forward: commands to prettify are required")
	}
	if strings.TrimSpace(deviceName) == "" {
		return "", nil, errors.New("forward: device name is required")
	}
	path, err := s.csPath(networkID, changeSetID)
	if err != nil {
		return "", nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/devices/"+url.PathEscape(strings.TrimSpace(deviceName))+"/commands?action=prettify", strings.NewReader(commands))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req = markOperation(req, "Predict.PrettifyCommands")
	var out struct {
		Prettified string `json:"prettified"`
	}
	resp, err := s.firewallDo(req, &out)
	return out.Prettified, resp, err
}

// ExportChangeSetZip writes the change set's head commit as a zip to w (GET .../change-sets/{id}?format=zip). The pinned builds also
// need the organization property PREDICT_DIFF_INSIGHT_ENHANCEMENTS (Forward master retired it; there PREDICT_MODELING and
// MANAGE_CHANGE_SETS suffice). Forward audits the export and answers 404 when the change set has no commit.
// Forward sends the 200 headers before it writes the zip, so a failure mid-stream leaves partial bytes in w along with the error; the
// client's timeout covers the whole export.
func (s *Predict) ExportChangeSetZip(ctx context.Context, networkID, changeSetID string, w io.Writer) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if w == nil {
		return nil, errors.New("forward: a writer is required")
	}
	path, err := s.csPath(networkID, changeSetID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?format=zip", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/zip, application/json")
	req = markOperation(req, "Predict.ExportChangeSetZip")
	return s.firewallDo(req, w)
}

// FirewallObjects returns one object collection of a firewall as the change set's BASELINE snapshot has it (GET .../change-sets/{id}/
// devices/{device}/{kind}; VIEW_CHANGE_SET_SECURITY_DATA and the organization property FIREWALL_PREDICT, plus device access). The body
// is Forward's collection type for that kind (AddressObjectsCollection, SecurityZoneCollection, ...), returned as raw JSON: their
// shapes differ per kind and per vendor, and this SDK does not model them.
func (s *Predict) FirewallObjects(ctx context.Context, networkID, changeSetID, deviceName string, kind ports.FirewallObjectKind) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	switch kind {
	case ports.FirewallAddressObjects, ports.FirewallAddressGroups, ports.FirewallApplicationObjects, ports.FirewallApplicationGroups, ports.FirewallURLObjects,
		ports.FirewallServiceObjects, ports.FirewallServiceGroups, ports.FirewallUserObjects, ports.FirewallZones:
	default:
		return nil, nil, fmt.Errorf("forward: unknown firewall object kind %q", kind)
	}
	if strings.TrimSpace(deviceName) == "" {
		return nil, nil, errors.New("forward: device name is required")
	}
	path, err := s.csPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	var out json.RawMessage
	resp, err := s.doJSON(ctx, http.MethodGet, path+"/devices/"+url.PathEscape(strings.TrimSpace(deviceName))+"/"+string(kind), nil, &out, "Predict.FirewallObjects")
	return out, resp, err
}

// BGPAdvertisementDiffs returns what the draft's staged BGP advertisements change on a device, compared with the change set's base
// snapshot (GET .../change-sets/{id}/draft/devices/{device}/bgp-advertisements?view=diffs; MANAGE_CHANGE_SETS). Forward computes it in
// Predict and sends the JSON array already decompressed, so it decodes as an ordinary body. A device with no BGP changes is an empty list.
func (s *Predict) BGPAdvertisementDiffs(ctx context.Context, networkID, changeSetID, deviceName string) ([]ports.BGPAdvertisementDiff, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.csDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	return s.bgpDiffs(ctx, path+"/bgp-advertisements?view=diffs", "Predict.BGPAdvertisementDiffs")
}

// CommittedBGPAdvertisementDiffs is BGPAdvertisementDiffs for a commit (GET .../change-sets/{id}/commits/{commitId}/devices/{device}/
// bgp-advertisements?view=diffs).
func (s *Predict) CommittedBGPAdvertisementDiffs(ctx context.Context, networkID, changeSetID, commitID, deviceName string) ([]ports.BGPAdvertisementDiff, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(deviceName) == "" {
		return nil, nil, errors.New("forward: device name is required")
	}
	path, err := s.commitPath(networkID, changeSetID, commitID)
	if err != nil {
		return nil, nil, err
	}
	return s.bgpDiffs(ctx, path+"/devices/"+url.PathEscape(strings.TrimSpace(deviceName))+"/bgp-advertisements?view=diffs", "Predict.CommittedBGPAdvertisementDiffs")
}

func (s *Predict) bgpDiffs(ctx context.Context, path, operation string) ([]ports.BGPAdvertisementDiff, *ports.Response, error) {
	out := listResponse[ports.BGPAdvertisementDiff]{Keys: []string{"diffs"}}
	resp, err := s.doJSON(ctx, http.MethodGet, path, nil, &out, operation)
	if err != nil {
		return nil, resp, err
	}
	if out.Items == nil {
		out.Items = []ports.BGPAdvertisementDiff{}
	}
	return out.Items, resp, nil
}

// TriggerProtocolModeling starts protocol modeling for a snapshot and returns at once (POST /api/internal/snapshots/{snapshotId}/
// protocol-modeling?action=trigger; 202; MANAGE_PROTOCOL_MODELING and PREDICT_MODELING). Poll ProtocolModelingDone. Preview: an internal
// route. RetriggerProtocolModeling runs it again for a snapshot that already has a result.
func (s *Predict) TriggerProtocolModeling(ctx context.Context, snapshotID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.protocolModelingAction(ctx, snapshotID, "trigger", "Predict.TriggerProtocolModeling")
}

// RetriggerProtocolModeling is TriggerProtocolModeling for a snapshot that already has a modeling result (?action=retrigger; 202).
func (s *Predict) RetriggerProtocolModeling(ctx context.Context, snapshotID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.protocolModelingAction(ctx, snapshotID, "retrigger", "Predict.RetriggerProtocolModeling")
}

func (s *Predict) protocolModelingAction(ctx context.Context, snapshotID, action, operation string) (*ports.Response, error) {
	path, err := protocolModelingPath(snapshotID)
	if err != nil {
		return nil, err
	}
	return s.doJSON(ctx, http.MethodPost, path+"?action="+action, nil, nil, operation)
}

// ProtocolModelingDone reports whether protocol modeling has finished for a snapshot (GET /api/internal/snapshots/{snapshotId}/
// protocol-modeling?view=status; MANAGE_PROTOCOL_MODELING and PREDICT_MODELING, so a read-only user is refused). Forward sends
// {"done": bool}: true once a successful result is stored. A modeling job that failed also reads false, so bound the wait on the
// caller's side. Preview: an internal route, not in the published OpenAPI.
func (s *Predict) ProtocolModelingDone(ctx context.Context, snapshotID string) (bool, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return false, nil, err
	}
	path, err := protocolModelingPath(snapshotID)
	if err != nil {
		return false, nil, err
	}
	var out struct {
		Done bool `json:"done"`
	}
	resp, err := s.doJSON(ctx, http.MethodGet, path+"?view=status", nil, &out, "Predict.ProtocolModelingDone")
	return out.Done, resp, err
}

// AffectedDevices lists the devices a change set affected in a PREDICTED snapshot, from its protocol modeling (GET .../protocol-modeling?
// view=affected-devices; VIEW_NETWORK_AND_SNAPSHOTS and PREDICT_MODELING; protocol modeling must have run). A base snapshot gives
// ErrNoAffectedDevices. Forward compresses the list; it arrives already decompressed. On a PAN-OS prediction it answered EVERY device of
// the network (49 of 49 seen live), not the ones the change set changed: the list is the devices that got a protocol-modeling output saved,
// and Forward's config generator emits interface and static routes for every device, so any device with an L3 address appears (not
// specific to PAN-OS). Preview: an internal route.
func (s *Predict) AffectedDevices(ctx context.Context, snapshotID string) ([]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := protocolModelingPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	var raw json.RawMessage
	resp, err := s.doJSON(ctx, http.MethodGet, path+"?view=affected-devices", nil, &raw, "Predict.AffectedDevices")
	if err != nil {
		return nil, resp, err
	}
	var failed struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &failed) == nil && failed.Error != "" {
		return nil, resp, fmt.Errorf("%w: %s", ports.ErrNoAffectedDevices, failed.Error)
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, resp, fmt.Errorf("forward: decode affected devices: %w", err)
	}
	if names == nil {
		names = []string{}
	}
	return names, resp, nil
}

func protocolModelingPath(snapshotID string) (string, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return "", errors.New("forward: snapshot ID is required")
	}
	return "/api/internal/snapshots/" + url.PathEscape(snapshotID) + "/protocol-modeling", nil
}

// ChangeSetNQEChecks lists the NQE checks added to a change set (GET /api/nqe/verificationQueries?networkId=&changeSetId=; USE_NQE
// with MANAGE_CHANGE_SETS, and PREDICT_MODELING). A change set without NQE checks gives an empty list. Preview.
func (s *Predict) ChangeSetNQEChecks(ctx context.Context, networkID, changeSetID string) ([]ports.NQECheckInfo, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	if changeSetID = strings.TrimSpace(changeSetID); changeSetID == "" {
		return nil, nil, errors.New("forward: change-set ID is required")
	}
	query := url.Values{"networkId": []string{networkID}, "changeSetId": []string{changeSetID}}
	out := listResponse[ports.NQECheckInfo]{Keys: []string{"checks"}}
	resp, err := s.doJSON(ctx, http.MethodGet, "/api/nqe/verificationQueries?"+query.Encode(), nil, &out, "Predict.ChangeSetNQEChecks")
	if err != nil {
		return nil, resp, err
	}
	if out.Items == nil {
		out.Items = []ports.NQECheckInfo{}
	}
	return out.Items, resp, nil
}
