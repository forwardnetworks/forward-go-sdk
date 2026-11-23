package domain

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
