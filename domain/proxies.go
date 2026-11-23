package domain

type ProxyServer struct {
	ID                  Identifier `json:"id,omitempty"`
	Name                string     `json:"name"`
	Host                string     `json:"host"`
	Port                int        `json:"port"`
	Protocol            string     `json:"protocol"`
	DisableCertChecking bool       `json:"disableCertChecking,omitempty"`
}
