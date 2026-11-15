package forward

import (
	"context"
	"fmt"
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

// InterfaceAdminUp is the AdminStatus text of an administratively up interface as the NQE read returns it (the enum's toString, not "UP").
const InterfaceAdminUp = "AdminStatus.UP"

// BGPSessionEstablished is the SessionState of an established BGP session.
const BGPSessionEstablished = "ESTABLISHED"

// BGPSession is one BGP neighbor as the NQE BGP-sessions read returns it. The identity is Device, VRF and NeighborIP (the columns a
// before/after diff keys on); SessionState is the value that changes. RemoteAS stays a string: NQE can return a 32-bit AS as text
// or a number across builds, so it is kept as Forward wrote it.
type BGPSession struct {
	Device       string `json:"Device"`
	VRF          string `json:"VRF"`
	NeighborIP   string `json:"NeighborIP"`
	RemoteAS     string `json:"RemoteAS,omitempty"`
	SessionState string `json:"SessionState"`
}

// Key is the session's identity, the tuple a diff matches on.
func (s BGPSession) Key() [3]string { return [3]string{s.Device, s.VRF, s.NeighborIP} }

// Established reports SessionState == ESTABLISHED.
func (s BGPSession) Established() bool {
	return strings.EqualFold(s.SessionState, BGPSessionEstablished)
}

// LLDPAdjacency is one LLDP neighbor as the NQE LLDP read returns it. The identity is all four fields.
type LLDPAdjacency struct {
	Device     string `json:"Device"`
	LocalIntf  string `json:"LocalIntf"`
	RemoteSys  string `json:"RemoteSys"`
	RemotePort string `json:"RemotePort"`
}

// Key is the adjacency's identity, the tuple a diff matches on.
func (a LLDPAdjacency) Key() [4]string {
	return [4]string{a.Device, a.LocalIntf, a.RemoteSys, a.RemotePort}
}

// StateReadOptions bounds a state read. Zero values use RunAll's defaults (1000 rows a page, one million rows at most, three attempts).
type StateReadOptions struct {
	PageSize int32
	MaxRows  int64
	Attempts int
}

// LLDPOptions narrows the LLDP read.
type LLDPOptions struct {
	StateReadOptions
	// Devices restricts the read to these device names (Skyforge's backbone scope). Empty reads every device. A name is matched
	// exactly and must not contain a double quote or a backslash, which would break out of the query's string literal; such a name is
	// an error rather than a mismatched query.
	Devices []string
	// SkipInterfaces leaves out interfaces with these names, as Skyforge's backbone read skips the management port "ma0".
	SkipInterfaces []string
}

// BGPSessions reads every BGP neighbor with its session state from one snapshot, as typed rows. An empty snapshotID reads the latest
// processed snapshot. A network with no BGP is an empty list.
func (s *NQEService) BGPSessions(ctx context.Context, networkID, snapshotID string, options StateReadOptions) ([]BGPSession, *Response, error) {
	rows, resp, err := s.RunAll(ctx, networkID, snapshotID, NQEQueryRequest{Query: nqeBGPSessionsQuery}, NQEPagedOptions{
		PageSize: options.PageSize, MaxRows: options.MaxRows, Attempts: options.Attempts,
		KeyColumns: []string{"Device", "VRF", "NeighborIP"},
	})
	if err != nil {
		return nil, resp, err
	}
	out, err := DecodeNQE[BGPSession](normalizeASColumn(rows))
	return out, resp, err
}

// LLDPAdjacencies reads every LLDP neighbor from one snapshot, as typed rows, optionally restricted to some devices and without some
// interfaces (see LLDPOptions).
func (s *NQEService) LLDPAdjacencies(ctx context.Context, networkID, snapshotID string, options LLDPOptions) ([]LLDPAdjacency, *Response, error) {
	query, err := lldpQuery(options)
	if err != nil {
		return nil, nil, err
	}
	rows, resp, err := s.RunAll(ctx, networkID, snapshotID, NQEQueryRequest{Query: query}, NQEPagedOptions{
		PageSize: options.PageSize, MaxRows: options.MaxRows, Attempts: options.Attempts,
		KeyColumns: []string{"Device", "LocalIntf", "RemoteSys", "RemotePort"},
	})
	if err != nil {
		return nil, resp, err
	}
	out, err := DecodeNQE[LLDPAdjacency](rows)
	return out, resp, err
}

func lldpQuery(options LLDPOptions) (string, error) {
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
func normalizeASColumn(rows []NQERecord) []NQERecord {
	for _, row := range rows {
		if raw, ok := row["RemoteAS"]; ok && len(raw) != 0 && raw[0] != '"' && string(raw) != "null" {
			row["RemoteAS"] = []byte(`"` + string(raw) + `"`)
		}
	}
	return rows
}

// InterfaceAdmin is one interface's administrative state. The identity is Device and Interface. AdminStatus is Forward's enum text
// ("AdminStatus.UP", "AdminStatus.DOWN"); use Up() rather than comparing text.
type InterfaceAdmin struct {
	Device      string `json:"Device"`
	Interface   string `json:"Interface"`
	AdminStatus string `json:"AdminStatus"`
}

// Key is the interface's identity, the tuple a diff matches on.
func (i InterfaceAdmin) Key() [2]string { return [2]string{i.Device, i.Interface} }

// Up reports an administratively up interface.
func (i InterfaceAdmin) Up() bool { return i.AdminStatus == InterfaceAdminUp }

// ACLEntry is one entry of an ACL: the NQE read has one row per entry, so Device and AclName repeat and only count the entries of
// each ACL. Action is Forward's enum text (for example "AclAction.PERMIT").
type ACLEntry struct {
	Device  string `json:"Device"`
	AclName string `json:"AclName"`
	Action  string `json:"Action"`
}

// ACLKey identifies one ACL.
type ACLKey struct{ Device, AclName string }

// InterfaceAdmin reads every interface's administrative state from one snapshot, as typed rows.
func (s *NQEService) InterfaceAdmin(ctx context.Context, networkID, snapshotID string, options StateReadOptions) ([]InterfaceAdmin, *Response, error) {
	rows, resp, err := s.RunAll(ctx, networkID, snapshotID, NQEQueryRequest{Query: nqeInterfaceAdminQuery}, NQEPagedOptions{
		PageSize: options.PageSize, MaxRows: options.MaxRows, Attempts: options.Attempts,
		KeyColumns: []string{"Device", "Interface"},
	})
	if err != nil {
		return nil, resp, err
	}
	out, err := DecodeNQE[InterfaceAdmin](rows)
	return out, resp, err
}

// ACLEntries reads every ACL entry from one snapshot, as typed rows (one per entry). The rows have no unique column, so the read keeps
// RunAll's stable-total and row-count checks but cannot catch a repeated row that hides a missing one (see NQEPagedOptions.NonUniqueKey);
// ACLCounts is the usual consumer, a per-ACL entry count.
func (s *NQEService) ACLEntries(ctx context.Context, networkID, snapshotID string, options StateReadOptions) ([]ACLEntry, *Response, error) {
	rows, resp, err := s.RunAll(ctx, networkID, snapshotID, NQEQueryRequest{Query: nqeACLQuery}, NQEPagedOptions{
		PageSize: options.PageSize, MaxRows: options.MaxRows, Attempts: options.Attempts,
		KeyColumns: []string{"Device", "AclName"}, NonUniqueKey: true,
	})
	if err != nil {
		return nil, resp, err
	}
	out, err := DecodeNQE[ACLEntry](rows)
	return out, resp, err
}

// ACLCounts is the number of entries of each ACL, from ACLEntries: the "count" the change gate compares before and after.
func (s *NQEService) ACLCounts(ctx context.Context, networkID, snapshotID string, options StateReadOptions) (map[ACLKey]int, *Response, error) {
	entries, resp, err := s.ACLEntries(ctx, networkID, snapshotID, options)
	if err != nil {
		return nil, resp, err
	}
	counts := make(map[ACLKey]int, len(entries))
	for _, entry := range entries {
		counts[ACLKey{entry.Device, entry.AclName}]++
	}
	return counts, resp, nil
}
