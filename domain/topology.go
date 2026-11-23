package domain

type TopologyLink struct {
	SourcePort string `json:"sourcePort"`
	TargetPort string `json:"targetPort"`
}

type TopologyOverrideLink struct {
	Port1 string `json:"port1"`
	Port2 string `json:"port2"`
}

type TopologyOverrides struct {
	Absent  []TopologyOverrideLink `json:"absent"`
	Present []TopologyOverrideLink `json:"present"`
}

type TopologyOverridesEdit struct {
	AbsentAdditions  []TopologyOverrideLink `json:"absentAdditions,omitempty"`
	AbsentRemovals   []TopologyOverrideLink `json:"absentRemovals,omitempty"`
	PresentAdditions []TopologyOverrideLink `json:"presentAdditions,omitempty"`
	PresentRemovals  []TopologyOverrideLink `json:"presentRemovals,omitempty"`
}
