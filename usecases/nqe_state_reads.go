package usecases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
)

// The state-preservation reads: the same NQE queries and result columns Skyforge's change gate compares before and after a change
// (internal/changedemo/gate/state.go, kept verbatim from its Python original), as typed methods, so a caller does not copy query text
// and column names. Each is a RunAll read, so a result too large for one page is read whole and a read that cannot be made
// consistent fails rather than returning a partial set. Preview: the queries were proven on Skyforge's fleet, not run by this SDK.

const (
	// nqeBGPSessionsQuery lists every BGP neighbor of every network instance, with its session state.
	nqeBGPSessionsQuery = `foreach device in network.devices
foreach ni in device.networkInstances
foreach protocol in ni.protocols
where isPresent(protocol.bgp)
foreach neighbor in protocol.bgp.neighbors
select {
  Device: device.name, VRF: ni.name,
  NeighborIP: neighbor.neighborAddress, RemoteAS: neighbor.peerAS,
  SessionState: neighbor.sessionState
}`

	// The LLDP query is built in lldpQuery from these two halves, with the device and interface scoping between them.
	nqeLLDPQueryHead = `foreach device in network.devices
`
	nqeLLDPQueryTail = `foreach interface in device.interfaces
foreach neighbor in interface.lldp.neighbors
select {
  Device: device.name, LocalIntf: interface.name,
  RemoteSys: neighbor.deviceName, RemotePort: neighbor.portName
}`
)

// nqeInterfaceAdminQuery tracks each interface's ADMIN state. toString() on Forward's enum yields "AdminStatus.UP" or
// "AdminStatus.DOWN", not "UP" (measured by Skyforge on a real network, 2026-10-10): a gate that guessed "UP" matched nothing and
// let every shutdown through while still reporting a healthy-looking row count.
const nqeInterfaceAdminQuery = `foreach device in network.devices
foreach interface in device.interfaces
select {
  Device: device.name, Interface: interface.name,
  AdminStatus: toString(interface.adminStatus)
}`

// nqeACLQuery is one row per ACL entry.
const nqeACLQuery = `foreach device in network.devices
foreach acl in device.aclEntries
select { Device: device.name, AclName: acl.name, Action: toString(acl.action) }`

// BGPSessions reads every BGP neighbor with its session state from one snapshot, as typed rows. An empty snapshotID reads the latest
// processed snapshot. A network with no BGP is an empty list.
func (s *NQE) BGPSessions(ctx context.Context, networkID, snapshotID string, options domain.StateReadOptions) ([]domain.BGPSession, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	rows, resp, err := s.RunAll(ctx, networkID, snapshotID, domain.NQEQueryRequest{Query: nqeBGPSessionsQuery}, domain.NQEPagedOptions{
		PageSize: options.PageSize, MaxRows: options.MaxRows, Attempts: options.Attempts,
		KeyColumns: []string{"Device", "VRF", "NeighborIP"},
	})
	if err != nil {
		return nil, resp, err
	}
	out, err := domain.DecodeNQE[domain.BGPSession](normalizeASColumn(rows))
	return out, resp, err
}

// LLDPAdjacencies reads every LLDP neighbor from one snapshot, as typed rows, optionally restricted to some devices and without some
// interfaces (see LLDPOptions).
func (s *NQE) LLDPAdjacencies(ctx context.Context, networkID, snapshotID string, options domain.LLDPOptions) ([]domain.LLDPAdjacency, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	query, err := lldpQuery(options)
	if err != nil {
		return nil, nil, err
	}
	rows, resp, err := s.RunAll(ctx, networkID, snapshotID, domain.NQEQueryRequest{Query: query}, domain.NQEPagedOptions{
		PageSize: options.PageSize, MaxRows: options.MaxRows, Attempts: options.Attempts,
		KeyColumns: []string{"Device", "LocalIntf", "RemoteSys", "RemotePort"},
	})
	if err != nil {
		return nil, resp, err
	}
	out, err := domain.DecodeNQE[domain.LLDPAdjacency](rows)
	return out, resp, err
}

func lldpQuery(options domain.LLDPOptions) (string, error) {
	var b strings.Builder
	b.WriteString(nqeLLDPQueryHead)
	if len(options.Devices) != 0 {
		clauses := make([]string, len(options.Devices))
		for i, name := range options.Devices {
			literal, err := nqeStringLiteral(name)
			if err != nil {
				return "", err
			}
			clauses[i] = "device.name == " + literal
		}
		b.WriteString("where " + strings.Join(clauses, " || ") + "\n")
	}
	b.WriteString("foreach interface in device.interfaces\n")
	if len(options.SkipInterfaces) != 0 {
		clauses := make([]string, len(options.SkipInterfaces))
		for i, name := range options.SkipInterfaces {
			literal, err := nqeStringLiteral(name)
			if err != nil {
				return "", err
			}
			clauses[i] = "interface.name != " + literal
		}
		b.WriteString("where " + strings.Join(clauses, " && ") + "\n")
	}
	b.WriteString(strings.TrimPrefix(nqeLLDPQueryTail, "foreach interface in device.interfaces\n"))
	return b.String(), nil
}

func nqeStringLiteral(value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "\"\\\n\r") {
		return "", fmt.Errorf("forward: %q cannot be placed in an NQE string literal (empty, or has a quote, backslash or newline)", value)
	}
	return `"` + value + `"`, nil
}

// normalizeASColumn writes a numeric RemoteAS as text so BGPSession.RemoteAS decodes whichever way Forward sent it.
func normalizeASColumn(rows []domain.NQERecord) []domain.NQERecord {
	for _, row := range rows {
		if raw, ok := row["RemoteAS"]; ok && len(raw) != 0 && raw[0] != '"' && string(raw) != "null" {
			row["RemoteAS"] = []byte(`"` + string(raw) + `"`)
		}
	}
	return rows
}

// InterfaceAdmin reads every interface's administrative state from one snapshot, as typed rows.
func (s *NQE) InterfaceAdmin(ctx context.Context, networkID, snapshotID string, options domain.StateReadOptions) ([]domain.InterfaceAdmin, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	rows, resp, err := s.RunAll(ctx, networkID, snapshotID, domain.NQEQueryRequest{Query: nqeInterfaceAdminQuery}, domain.NQEPagedOptions{
		PageSize: options.PageSize, MaxRows: options.MaxRows, Attempts: options.Attempts,
		KeyColumns: []string{"Device", "Interface"},
	})
	if err != nil {
		return nil, resp, err
	}
	out, err := domain.DecodeNQE[domain.InterfaceAdmin](rows)
	return out, resp, err
}

// ACLEntries reads every ACL entry from one snapshot, as typed rows (one per entry). The rows have no unique column, so the read keeps
// RunAll's stable-total and row-count checks but cannot catch a repeated row that hides a missing one (see NQEPagedOptions.NonUniqueKey);
// ACLCounts is the usual consumer, a per-ACL entry count.
func (s *NQE) ACLEntries(ctx context.Context, networkID, snapshotID string, options domain.StateReadOptions) ([]domain.ACLEntry, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	rows, resp, err := s.RunAll(ctx, networkID, snapshotID, domain.NQEQueryRequest{Query: nqeACLQuery}, domain.NQEPagedOptions{
		PageSize: options.PageSize, MaxRows: options.MaxRows, Attempts: options.Attempts,
		KeyColumns: []string{"Device", "AclName"}, NonUniqueKey: true,
	})
	if err != nil {
		return nil, resp, err
	}
	out, err := domain.DecodeNQE[domain.ACLEntry](rows)
	return out, resp, err
}

// ACLEntriesWith is ACLEntries with a query and key of your own, for the stronger guarantee: select a column that makes each entry
// unique (aclEntry.uuid where the vendor has one, or the header-match columns Forward's own "ACL entries" library query keys on)
// and name it in KeyColumns.
func (s *NQE) ACLEntriesWith(ctx context.Context, networkID, snapshotID string, options domain.ACLReadOptions) ([]domain.ACLEntry, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	query := options.Query
	if query == "" {
		if len(options.KeyColumns) != 0 {
			return nil, nil, errors.New("forward: KeyColumns names columns the default ACL query does not select; give a Query that does")
		}
		query = nqeACLQuery
	}
	keys, unique := options.KeyColumns, true
	if len(keys) == 0 {
		keys, unique = []string{"Device", "AclName"}, false
	}
	rows, resp, err := s.RunAll(ctx, networkID, snapshotID, domain.NQEQueryRequest{Query: query}, domain.NQEPagedOptions{
		PageSize: options.PageSize, MaxRows: options.MaxRows, Attempts: options.Attempts,
		KeyColumns: keys, NonUniqueKey: !unique,
	})
	if err != nil {
		return nil, resp, err
	}
	out, err := domain.DecodeNQE[domain.ACLEntry](rows)
	if err != nil {
		return nil, resp, err
	}
	for i := range out {
		out[i].Row = rows[i]
		if unique {
			out[i].Identity = make([]string, len(keys))
			for j, column := range keys {
				out[i].Identity[j] = nqeCellText(rows[i][column])
			}
		}
	}
	return out, resp, nil
}

// nqeCellText renders one NQE value as text: a JSON string unquoted, anything else (a number, a list) as its JSON.
func nqeCellText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return strings.TrimSpace(string(raw))
}

// ACLCounts is the number of entries of each ACL, from ACLEntries: the "count" the change gate compares before and after.
func (s *NQE) ACLCounts(ctx context.Context, networkID, snapshotID string, options domain.StateReadOptions) (map[domain.ACLKey]int, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	entries, resp, err := s.ACLEntries(ctx, networkID, snapshotID, options)
	if err != nil {
		return nil, resp, err
	}
	counts := make(map[domain.ACLKey]int, len(entries))
	for _, entry := range entries {
		counts[domain.ACLKey{Device: entry.Device, AclName: entry.AclName}]++
	}
	return counts, resp, nil
}
