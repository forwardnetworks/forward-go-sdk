package domain

import (
	"errors"
	"strings"
	"time"
)

// DeploymentType is the platform an artifact can be deployed onto. An
// artifact may list several.
type DeploymentType string

const (
	DeploymentTypeVMwareHypervisor DeploymentType = "VMWARE_HYPERVISOR"
	DeploymentTypeKVMHypervisor    DeploymentType = "KVM_HYPERVISOR"
	DeploymentTypeAWSEC2           DeploymentType = "AWS_EC2"
	DeploymentTypeAWSGovCloudEC2   DeploymentType = "AWS_GOVCLOUD_EC2"
	DeploymentTypeAzureVM          DeploymentType = "AZURE_VM"
	DeploymentTypeAzureGovVM       DeploymentType = "AZURE_GOV_VM"
	DeploymentTypeGCPVM            DeploymentType = "GCP_VM"
)

// DeploymentWorkflow says what an artifact is FOR. INSTALLATION artifacts
// stand up a new instance (an OVA, a cloud image); APP_UPGRADE artifacts move
// an existing instance forward and carry a base-version window.
type DeploymentWorkflow string

const (
	DeploymentWorkflowInstallation DeploymentWorkflow = "INSTALLATION"
	DeploymentWorkflowAppUpgrade   DeploymentWorkflow = "APP_UPGRADE"
)

// DeploymentArtifactFileType is the on-disk format of one artifact version.
type DeploymentArtifactFileType string

const (
	DeploymentArtifactFileTypeOVA     DeploymentArtifactFileType = "OVA"
	DeploymentArtifactFileTypeTarGz   DeploymentArtifactFileType = "TAR_GZ"
	DeploymentArtifactFileTypeAirgap  DeploymentArtifactFileType = "AIRGAP"
	DeploymentArtifactFileTypePackage DeploymentArtifactFileType = "PACKAGE"
)

// Suffix is the file extension Forward attaches to the type when it names the
// downloaded file (mirrors DeploymentArtifactFileType.getSuffix). An unknown
// type yields "" rather than a guess, so a new server-side type produces a
// visibly extension-less name instead of a wrong one.
func (t DeploymentArtifactFileType) Suffix() string {
	switch t {
	case DeploymentArtifactFileTypeOVA:
		return ".ova"
	case DeploymentArtifactFileTypeTarGz:
		return ".tar.gz"
	case DeploymentArtifactFileTypeAirgap:
		return ".airgap"
	case DeploymentArtifactFileTypePackage:
		return ".package"
	default:
		return ""
	}
}

// DeploymentArtifact is one catalog entry with every version Forward still
// serves for it.
type DeploymentArtifact struct {
	ID              Identifier                  `json:"id"`
	Name            string                      `json:"name"`
	DeploymentTypes []DeploymentType            `json:"deploymentTypes"`
	Workflow        DeploymentWorkflow          `json:"workflow"`
	Versions        []DeploymentArtifactVersion `json:"versions"`
}

// DeploymentArtifactVersion is one downloadable file. UploadedAt is epoch
// milliseconds exactly as the server sends it (a JSON number); use
// UploadedTime for a time.Time.
type DeploymentArtifactVersion struct {
	ID             Identifier                 `json:"id"`
	BaseVersion    string                     `json:"baseVersion,omitempty"`
	MinBaseVersion string                     `json:"minBaseVersion,omitempty"`
	MaxBaseVersion string                     `json:"maxBaseVersion,omitempty"`
	Version        string                     `json:"version"`
	FileType       DeploymentArtifactFileType `json:"fileType"`
	SizeInBytes    int64                      `json:"sizeInBytes"`
	SHA256         string                     `json:"sha256"`
	UploadedBy     string                     `json:"uploadedBy"`
	UploadedAt     int64                      `json:"uploadedAt"`
}

// UploadedTime converts the epoch-millisecond upload stamp to a time.Time. A
// zero stamp yields the zero time rather than 1970.
func (v DeploymentArtifactVersion) UploadedTime() time.Time {
	if v.UploadedAt == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v.UploadedAt).UTC()
}

// FileName is the name Forward gives the file when it serves version v of
// artifact a: `<name>[-<baseVersion>]-<version><suffix>` (mirrors
// DeploymentController.getArtifactFileName). It is what a caller should
// write to disk so the local name matches what the Forward GUI would have
// saved.
func (a DeploymentArtifact) FileName(v DeploymentArtifactVersion) string {
	var b strings.Builder
	b.WriteString(a.Name)
	if v.BaseVersion != "" {
		b.WriteByte('-')
		b.WriteString(v.BaseVersion)
	}
	b.WriteByte('-')
	b.WriteString(v.Version)
	b.WriteString(v.FileType.Suffix())
	return b.String()
}

// DeploymentArtifactVersionRef is a version flattened together with the
// artifact it belongs to, so a caller can go straight from a list entry to a
// download without re-joining the two.
type DeploymentArtifactVersionRef struct {
	ArtifactID   Identifier
	ArtifactName string
	Version      DeploymentArtifactVersion
	FileName     string
}

// ErrDeploymentArtifactIDRequired guards the per-artifact routes. An empty id
// would build "/api/deployment-artifacts/" which is the LIST route with a
// trailing slash, and what the server makes of that is not something to find
// out by sending it.
var ErrDeploymentArtifactIDRequired = errors.New("forward: deployment artifact id is required")
