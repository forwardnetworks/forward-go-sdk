package domain

import (
	"strings"
)

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

// ACLEntry is one row of the ACL entry read: one per entry of device.aclEntries, so Device and AclName can repeat. AclName is the
// entry's own name (NQE's aclEntry.name, which Forward's library query calls "Rule"), which a vendor may leave empty or share across
// entries. Action is Forward's enum text (for example "AclAction.PERMIT"). UUID and Description are optional columns a query may add
// (aclEntry.uuid, aclEntry.description): UUID is the only per-entry identifier Forward's data model has, and only for vendors that give
// entries one.
type ACLEntry struct {
	Device      string `json:"Device"`
	AclName     string `json:"AclName"`
	Action      string `json:"Action"`
	UUID        string `json:"UUID,omitempty"`
	Description string `json:"Description,omitempty"`
	// Row is the whole row as the query returned it, every selected column, so a query that selects more than the typed fields (Forward's
	// library identity has eighteen) loses none. Set by ACLEntriesWith; nil from ACLEntries, whose query selects only the typed columns.
	Row NQERecord `json:"-"`
	// Identity holds the value of each of ACLReadOptions.KeyColumns, in that order, as text (a string column unquoted, any other value
	// as its JSON). Set by ACLEntriesWith when KeyColumns is given; compare entries on IdentityKey rather than the five typed fields.
	Identity []string `json:"-"`
}

// IdentityKey is Identity joined into one comparable string, for use as a map key; empty when the read named no KeyColumns.
func (e ACLEntry) IdentityKey() string { return strings.Join(e.Identity, "\x00") }

// Key is the entry's identity as far as the row says: Device, AclName and UUID (empty when the query did not select one). It is
// unique only if the query selects an identifying column; with UUID empty, two entries sharing a name have the same Key.
func (e ACLEntry) Key() [3]string { return [3]string{e.Device, e.AclName, e.UUID} }

// ACLKey identifies one ACL.
type ACLKey struct{ Device, AclName string }

// ACLReadOptions is for ACLEntriesWith: a query of your own that selects an identifying column, and the columns that identify an
// entry in it.
type ACLReadOptions struct {
	StateReadOptions
	// Query is the NQE source. It must select the Device, AclName and Action columns of ACLEntry, and every column in KeyColumns. Empty
	// uses the default query (Skyforge's: Device, AclName and Action only), which cannot identify an entry, so KeyColumns must be empty
	// with it.
	Query string
	// KeyColumns identify an entry, e.g. {"Device", "AclName", "UUID"}. When set the read keeps RunAll's repeated-key check on the
	// tuple, so a repeated row that hides a missing one is caught (and an identical key repeating is ErrNQEPagingInconsistent). When
	// empty the key is {Device, AclName} and the repeated-key check is off (see NQEPagedOptions.NonUniqueKey).
	KeyColumns []string
}
