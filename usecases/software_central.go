package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"sort"
)

// ListForwardApplianceOVAs answers the one question the vSphere lane asks:
// which Forward appliance OVAs can I install right now? It keeps artifacts
// whose workflow is INSTALLATION and whose deployment types include
// VMWARE_HYPERVISOR, flattens them to their OVA versions, and orders the
// result newest first (UploadedAt descending, then Version descending as a
// tie-break) so index 0 is the current release.
//
// The server-side `type=VMWARE_HYPERVISOR` filter is applied AND re-checked
// client-side: the filter is the server's, the guarantee is ours.
func (s *SoftwareCentral) ListForwardApplianceOVAs(ctx context.Context) ([]domain.DeploymentArtifactVersionRef, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	artifacts, response, err := s.ListDeploymentArtifacts(ctx, domain.DeploymentTypeVMwareHypervisor)
	if err != nil {
		return nil, response, err
	}
	var refs []domain.DeploymentArtifactVersionRef
	for _, artifact := range artifacts {
		if artifact.Workflow != domain.DeploymentWorkflowInstallation || !hasDeploymentType(artifact.DeploymentTypes, domain.DeploymentTypeVMwareHypervisor) {
			continue
		}
		for _, version := range artifact.Versions {
			if version.FileType != domain.DeploymentArtifactFileTypeOVA {
				continue
			}
			refs = append(refs, domain.DeploymentArtifactVersionRef{
				ArtifactID:   artifact.ID,
				ArtifactName: artifact.Name,
				Version:      version,
				FileName:     artifact.FileName(version),
			})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool {
		left, right := refs[i].Version, refs[j].Version
		if left.UploadedAt != right.UploadedAt {
			return left.UploadedAt > right.UploadedAt
		}
		return left.Version > right.Version
	})
	return refs, response, nil
}

func hasDeploymentType(types []domain.DeploymentType, want domain.DeploymentType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}
