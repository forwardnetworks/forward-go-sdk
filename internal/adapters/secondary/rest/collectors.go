package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// CollectorsService manages collector registration and network attachment.
type Collectors service

func validateDisabledCommands(commands map[string][]ports.DisabledCommand) error {
	for deviceType, list := range commands {
		if strings.TrimSpace(deviceType) == "" {
			return errors.New("forward: a disabled-commands device type must not be empty")
		}
		for i, command := range list {
			if strings.TrimSpace(command.Command) == "" || strings.TrimSpace(command.Version.Start) == "" || strings.TrimSpace(command.Version.End) == "" {
				return fmt.Errorf("forward: disabled command %d for %q needs a command and both version.start and version.end", i, deviceType)
			}
		}
	}
	return nil
}

func (s *Collectors) List(ctx context.Context) ([]ports.Collector, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.Collector]{Keys: []string{"collectors", "items", "data"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collectors", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *Collectors) Register(ctx context.Context, input ports.CollectorRegistrationRequest) (*ports.CollectorRegistration, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	input.CollectorName = strings.TrimSpace(input.CollectorName)
	if input.CollectorName == "" {
		return nil, nil, errors.New("forward: collector name is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/collectors", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.Register")
	out := new(ports.CollectorRegistration)
	response, err := s.client.doRequired(req, out)
	if err == nil && (out.ID == "" || strings.TrimSpace(out.Username) == "" || out.AuthorizationKey == "") {
		err = errors.New("forward: collector registration returned an incomplete authorization identity")
	}
	return out, response, err
}

func (s *Collectors) Delete(ctx context.Context, collectorIDOrName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	value := strings.TrimSpace(collectorIDOrName)
	if value == "" {
		return nil, errors.New("forward: collector ID or name is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/collectors/"+url.PathEscape(value), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.Delete")
	response, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return response, nil
	}
	return response, err
}

func (s *Collectors) Attachment(ctx context.Context, networkID string) (*ports.CollectorAttachment, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	// /collector, not /collector/status -- see the CollectorAttachment doc
	// comment. The /status route is gone server-side and 404s regardless of
	// network or attachment state.
	path, err := s.networkPath(networkID, "/collector")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.Attachment")
	out := new(ports.CollectorAttachment)
	response, err := s.client.doRequired(req, out)
	if err == nil && out != nil {
		out.IsSet = strings.TrimSpace(out.CollectorUsername) != ""
	}
	return out, response, err
}

// Attach makes the collector with the given username the network's DEFAULT collector, used by the devices that name none. PUT /api/networks/{networkId}/collector
// (JSON {"username": ...}). The username is the collector's (for example "collector-7ddceb66"), not its name or id.
// Check the result with Attachment, which reads the same route.
func (s *Collectors) Attach(ctx context.Context, networkID string, input ports.CollectorAttachmentRequest) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" {
		return nil, errors.New("forward: collector username is required")
	}
	path, err := s.networkPath(networkID, "/collector")
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.Attach")
	return s.client.Do(req, nil)
}

func (s *Collectors) SetPerformanceCollection(ctx context.Context, networkID string, settings ports.PerformanceCollectionSettings) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := s.networkPath(networkID, "/performance/settings")
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, settings)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.SetPerformanceCollection")
	return s.client.Do(req, nil)
}

func (s *Collectors) PatchOrganizationSettings(ctx context.Context, patch ports.OrgCollectionSettingsPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if patch.DisabledCommands != nil {
		if err := validateDisabledCommands(*patch.DisabledCommands); err != nil {
			return nil, err
		}
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/collection-settings", patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.PatchOrganizationSettings")
	return s.client.Do(req, nil)
}

func (s *Collectors) PatchSettings(ctx context.Context, collectorID string, patch ports.CollectorCollectionSettingsPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	collectorID = strings.TrimSpace(collectorID)
	if collectorID == "" {
		return nil, errors.New("forward: collector ID is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/collectors/"+url.PathEscape(collectorID)+"/collection-settings", patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.PatchSettings")
	return s.client.Do(req, nil)
}

func (s *Collectors) networkPath(networkID, suffix string) (string, error) {
	return s.client.networkSubPath(networkID, strings.TrimPrefix(suffix, "/"))
}

// GetSettings returns the collection settings stored for a collector: only
// what was set, so a nil field is a default, not zero. GET
// /api/collectors/{collectorId}/collection-settings (CollectionController;
// VIEW_COLLECTORS). Preview: not in the published spec.
func (s *Collectors) GetSettings(ctx context.Context, collectorID string) (*ports.CollectorCollectionSettings, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if collectorID = strings.TrimSpace(collectorID); collectorID == "" {
		return nil, nil, errors.New("forward: collector ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collectors/"+url.PathEscape(collectorID)+"/collection-settings", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.GetSettings")
	out := new(ports.CollectorCollectionSettings)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// GetOrganizationSettings returns the collection settings stored for the
// organization's collectors: only what was set, so a nil field is a default,
// not zero. GET /api/collection-settings (CollectionController;
// VIEW_ORG_COLLECTION_SETTINGS). The sibling ?for=collector form is the
// collectors' own binary feed, not this. Preview: not in the published spec.
func (s *Collectors) GetOrganizationSettings(ctx context.Context) (*ports.OrgCollectionSettings, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collection-settings", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.GetOrganizationSettings")
	out := new(ports.OrgCollectionSettings)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Upgrade asks Forward to upgrade one collector to the current release and returns once the upgrade is queued, not
// finished. Poll List until CollectorUpdating clears, then read UpdateStatus and Version: UP_TO_DATE means it worked,
// while a failed or timed-out upgrade also clears CollectorUpdating and leaves UpdateStatus OUTDATED. The upgrade is a
// blocking task, so it first waits for a collection running on that collector to finish (Forward gives the task 15
// minutes), then restarts the collector: collections queued behind it fail with "Collector version changed". POST
// /api/collectors/{collectorName}?action=upgrade (CollectorSetupController.upgradeCollector; MANAGE_COLLECTORS, org
// admin; Preview: not in the published spec). Forward answers 400 when the collector is offline, never connected, not
// remotely upgradeable, already up to date, at end of life, bundled with an on-prem deployment, or upgrades are off
// (BUNDLED_ONLY); 404 for an unknown name; and 409 when an upgrade is already in progress. See Collector.UpgradeBlocker
// for the checks that can be made first.
func (s *Collectors) Upgrade(ctx context.Context, collectorName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	collectorName = strings.TrimSpace(collectorName)
	if collectorName == "" {
		return nil, errors.New("forward: collector name is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/collectors/"+url.PathEscape(collectorName)+"?action=upgrade", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.Upgrade")
	return s.client.Do(req, nil)
}

// UpgradeAll queues an upgrade for every collector in the org that can be upgraded remotely (online, outdated or
// unsupported but not end of life, not bundled, installed as a service) and returns their names; collectors with an
// upgrade already queued are left out. An empty list means nothing qualified. POST /api/collectors?action=upgrade
// (CollectorSetupController.upgradeCollectors; MANAGE_COLLECTORS; Preview: not in the published spec). The order of
// the names is unspecified. Like Upgrade, it returns when queued, with the same polling and restart behaviour.
func (s *Collectors) UpgradeAll(ctx context.Context) ([]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/collectors?action=upgrade", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.UpgradeAll")
	var names []string
	response, err := s.client.doRequired(req, &names)
	if err != nil {
		return nil, response, err
	}
	if names == nil {
		names = []string{}
	}
	return names, response, err
}
