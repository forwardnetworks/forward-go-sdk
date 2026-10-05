package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// WanCircuitsService manages a network's WAN circuits: synthetic devices that
// each join two customer-edge ports, as a carrier circuit would. The published
// WAN Circuits API (fwd/api/apis/wan-circuits.yaml, WanCircuitController),
// served on primary 15398425a69 and stable 67e89c87124. Unlike the other
// synthetic devices, a WAN circuit cannot take an NQE query.
type WanCircuitsService service

// WanCircuit is one circuit. Source is read-only (Forward reports how the
// circuit was defined) and is never sent.
type WanCircuit struct {
	Name        string               `json:"name"`
	Connection1 WanCircuitConnection `json:"connection1"`
	Connection2 WanCircuitConnection `json:"connection2"`
	Source      string               `json:"source,omitempty"`
}

// WanCircuitConnection is one end of a circuit: a port on a customer edge
// device. VLAN is nil for untagged traffic; Name, if set, names the interface
// created on the circuit device.
type WanCircuitConnection struct {
	Device string `json:"device"`
	Port   string `json:"port"`
	VLAN   *int   `json:"vlan,omitempty"`
	Name   string `json:"name,omitempty"`
}

// WanCircuitPatch changes some of a circuit: nil fields are left alone. A Name
// renames the circuit.
type WanCircuitPatch struct {
	Name        *string               `json:"name,omitempty"`
	Connection1 *WanCircuitConnection `json:"connection1,omitempty"`
	Connection2 *WanCircuitConnection `json:"connection2,omitempty"`
}

func (s *WanCircuitsService) collectionPath(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/wan-circuits", nil
}

func (s *WanCircuitsService) circuitPath(networkID, name string) (string, error) {
	path, err := s.collectionPath(networkID)
	if err != nil {
		return "", err
	}
	if name = strings.TrimSpace(name); name == "" {
		// Never collapse onto the collection route, where a PUT replaces every circuit.
		return "", errors.New("forward: WAN circuit name is required")
	}
	return path + "/" + url.PathEscape(name), nil
}

// List returns the network's WAN circuits. GET .../wan-circuits (getWanCircuits).
func (s *WanCircuitsService) List(ctx context.Context, networkID string) ([]WanCircuit, *Response, error) {
	path, err := s.collectionPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "WanCircuits.List")
	result := listResponse[WanCircuit]{Keys: []string{"wanCircuits"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Get returns one circuit, or (nil, nil) when it does not exist. GET
// .../wan-circuits/{name} (getWanCircuit).
func (s *WanCircuitsService) Get(ctx context.Context, networkID, name string) (*WanCircuit, *Response, error) {
	path, err := s.circuitPath(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "WanCircuits.Get")
	out := new(WanCircuit)
	resp, err := s.client.doRequired(req, out)
	if isStatus(err, http.StatusNotFound) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Put adds or replaces one circuit. PUT .../wan-circuits/{name} (putWanCircuit).
// Forward stores the circuit under the body's name, not the path's, so an
// empty circuit.Name takes name and a different one is refused.
func (s *WanCircuitsService) Put(ctx context.Context, networkID, name string, circuit WanCircuit) (*Response, error) {
	path, err := s.circuitPath(networkID, name)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if circuit.Name = strings.TrimSpace(circuit.Name); circuit.Name == "" {
		circuit.Name = name
	} else if circuit.Name != name {
		return nil, errors.New("forward: WAN circuit body name " + circuit.Name + " differs from " + name + "; use Patch to rename")
	}
	circuit.Source = "" // reported by Forward, never sent
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, circuit)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "WanCircuits.Put")
	return s.client.Do(req, nil)
}

// Patch changes the stated parts of one circuit and returns it as Forward now
// holds it. PATCH .../wan-circuits/{name} (patchWanCircuit).
func (s *WanCircuitsService) Patch(ctx context.Context, networkID, name string, patch WanCircuitPatch) (*WanCircuit, *Response, error) {
	path, err := s.circuitPath(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "WanCircuits.Patch")
	out := new(WanCircuit)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Delete removes one circuit. A 404 is success. DELETE .../wan-circuits/{name}
// (deleteWanCircuit).
func (s *WanCircuitsService) Delete(ctx context.Context, networkID, name string) (*Response, error) {
	path, err := s.circuitPath(networkID, name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "WanCircuits.Delete")
	resp, err := s.client.Do(req, nil)
	if isGone(err) {
		return resp, nil
	}
	return resp, err
}

// ReplaceAll replaces EVERY circuit of the network with circuits. PUT
// .../wan-circuits (putWanCircuits). An empty list deletes them all, so it is
// refused unless allowEmpty says that is the intent.
func (s *WanCircuitsService) ReplaceAll(ctx context.Context, networkID string, circuits []WanCircuit, allowEmpty bool) (*Response, error) {
	if len(circuits) == 0 && !allowEmpty {
		return nil, errors.New("forward: an empty list removes every WAN circuit; pass allowEmpty to mean that")
	}
	path, err := s.collectionPath(networkID)
	if err != nil {
		return nil, err
	}
	sent := make([]WanCircuit, len(circuits))
	for i, circuit := range circuits {
		circuit.Source = ""
		sent[i] = circuit
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, map[string][]WanCircuit{"wanCircuits": sent})
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "WanCircuits.ReplaceAll")
	return s.client.Do(req, nil)
}

// Backdate applies the network's CURRENT WAN circuits from snapshotID onward
// and INVALIDATES every snapshot from that one on, so they reprocess and their
// answers are unavailable until they finish. POST
// .../wan-circuits?op=backdate&snapshotId= (WanCircuitService.backdateTo:
// repo.backdateTo, then invalidateAffectedSnapshots from the snapshot's
// creation instant with no end). Needs MANAGE_COLLECTION_SOURCES and
// INVALIDATE_SNAPSHOTS. Preview: not in the published spec.
func (s *WanCircuitsService) Backdate(ctx context.Context, networkID, snapshotID string) (*Response, error) {
	path, err := s.collectionPath(networkID)
	if err != nil {
		return nil, err
	}
	return backdate(ctx, s.client, path, "op", snapshotID, "WanCircuits.Backdate")
}

// backdate sends one backdate request. param is "op" for the synthetic
// device collections and "action" for link overrides -- Forward is not
// consistent -- and the operation name is what marks it destructive to a hook.
func backdate(ctx context.Context, c *Client, path, param, snapshotID, operation string) (*Response, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, errors.New("forward: snapshot ID is required to backdate (an empty one would date nothing and still invalidate)")
	}
	query := url.Values{param: []string{"backdate"}, "snapshotId": []string{snapshotID}}
	req, err := c.NewRequest(ctx, http.MethodPost, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	return c.Do(req, nil)
}
