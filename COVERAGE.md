# Skyforge Forward API typed coverage

This file is generated from `coverage_manifest.json`; run `go generate ./...` to refresh it. The manifest was derived from Skyforge docs/forward-api-sdk-migration-audit.md plus the corrections appended there on 2026-08-05; 2026-09-06: access control, SAML settings and user roles added after diffing every route Skyforge sends against this manifest.

Current inventory: **223 semantic call sites**, **153 distinct normalized method+route pairs**, **153 COVERED**, **0 PARTIAL**, **0 MISSING**.

The inventory becomes stale when Skyforge adds, removes, or changes a Forward wire call. Run `go run ./cmd/skyforge-coverage -audit /path/to/skyforge/docs/forward-api-sdk-migration-audit.md` to diff the audited route set. The command fails when its audit input is missing.

| Method | Normalized route | Typed SDK symbol(s) | Class |
|---|---|---|---|
| DELETE | `/api/access-control-groups/{groupId}` | `AccessControl.DeleteGroup` | COVERED |
| DELETE | `/api/admin/orgs/{orgId}` | `Organizations.Delete` | COVERED |
| DELETE | `/api/collectors/{collectorIdOrName}` | `Collectors.Delete` | COVERED |
| DELETE | `/api/device-access-labels/{labelId}` | `AccessControl.DeleteDeviceAccessLabel` | COVERED |
| DELETE | `/api/integrations/servicenow` | `Integrations.DeleteServiceNow` | COVERED |
| DELETE | `/api/networks/{networkId}` | `Networks.Delete` | COVERED |
| DELETE | `/api/orgs/{orgId}/config/software_central` | `Properties.ClearOrganization` | COVERED |
| DELETE | `/api/users/current/tokens/{tokenName}` | `Users.DeleteToken` | COVERED |
| DELETE | `/api/users/{userId}` | `Admin.DeleteUser` | COVERED |
| GET | `/api/access-control-groups` | `AccessControl.ListGroups` | COVERED |
| GET | `/api/admin/impersonate` | `Browser.Impersonate`, `Browser.ImpersonateWithServiceCredential` | COVERED |
| GET | `/api/admin/networks` | `Admin.ListNetworks` | COVERED |
| GET | `/api/admin/orgs` | `Organizations.List` | COVERED |
| GET | `/api/admin/users` | `Admin.ListUsers` | COVERED |
| GET | `/api/admin/users/{idOrUsername}` | `Admin.LookupUser` | COVERED |
| GET | `/api/ai-chats/{chatId}` | `AI.GetChat` | COVERED |
| GET | `/api/ai-chats/{chatId}/messages` | `AI.ListMessages` | COVERED |
| GET | `/api/auth/saml-settings` | `SAML.GetSettings` | COVERED |
| GET | `/api/collector-tasks` | `CollectorTasks.List`, `CollectorTasks.Progress` | COVERED |
| GET | `/api/collectors` | `Collectors.List` | COVERED |
| GET | `/api/collectors/{collectorIdOrName}` | `Collectors.Get` | COVERED |
| GET | `/api/custom-banners` | `Banners.List` | COVERED |
| GET | `/api/deployment-config/{property}` | `Configuration.GetDeployment` | COVERED |
| GET | `/api/device-access-labels` | `AccessControl.ListDeviceAccessLabels` | COVERED |
| GET | `/api/endpoint-profiles` | `Endpoints.ListProfiles` | COVERED |
| GET | `/api/global-config` | `Properties.Global` | COVERED |
| GET | `/api/integrations/infoblox` | `Integrations.ListInfobloxLegacy` | COVERED |
| GET | `/api/integrations/infoblox/instances` | `Integrations.ListInfoblox` | COVERED |
| GET | `/api/integrations/servicenow` | `Integrations.GetServiceNow` | COVERED |
| GET | `/api/networks` | `Networks.List`, `Networks.CheckAccess` | COVERED |
| GET | `/api/networks/{networkId}/change-sets/{changeSetId}/predicted-snapshots` | `Predict.ListPredictedSnapshots` | COVERED |
| GET | `/api/networks/{networkId}/classic-devices` | `ClassicDevices.List`, `ClassicDevices.ListTestStatuses` | COVERED |
| GET | `/api/networks/{networkId}/cloudAccounts` | `CloudAccounts.List` | COVERED |
| GET | `/api/networks/{networkId}/collectionProgress` | `Collections.Progress` | COVERED |
| GET | `/api/networks/{networkId}/collections` | `Collections.List` | COVERED |
| GET | `/api/networks/{networkId}/collector/status` | `Collectors.Attachment` | COVERED |
| GET | `/api/networks/{networkId}/device-metrics` | `Performance.DeviceMetrics`, `Performance.DeviceMetricsDocument` | COVERED |
| GET | `/api/networks/{networkId}/device-statuses` | `Collections.DeviceStatuses` | COVERED |
| GET | `/api/networks/{networkId}/device-tags` | `DeviceTags.List` | COVERED |
| GET | `/api/networks/{networkId}/devices` | `Devices.List` | COVERED |
| GET | `/api/networks/{networkId}/end-host-scanners` | `Integrations.ListRapid7` | COVERED |
| GET | `/api/networks/{networkId}/endpoints` | `Endpoints.List`, `Endpoints.ListTestStatuses` | COVERED |
| GET | `/api/networks/{networkId}/http-credentials` | `Credentials.ListHTTP` | COVERED |
| GET | `/api/networks/{networkId}/interface-metrics` | `Performance.InterfaceMetrics`, `Performance.InterfaceMetricsDocument` | COVERED |
| GET | `/api/networks/{networkId}/locations` | `Locations.List` | COVERED |
| GET | `/api/networks/{networkId}/locations/{locationId}/clusters` | `Locations.ListClusters` | COVERED |
| GET | `/api/networks/{networkId}/paths` | `Networks.Paths` | COVERED |
| GET | `/api/networks/{networkId}/proxies` | `Proxies.List` | COVERED |
| GET | `/api/networks/{networkId}/snapshots` | `Snapshots.List`, `Snapshots.ListDocument`, `Compatibility.ListSnapshots` | COVERED |
| GET | `/api/networks/{networkId}/snmpCredentials` | `Credentials.ListSNMP` | COVERED |
| GET | `/api/networks/{networkId}/unhealthy-devices` | `Performance.UnhealthyDevices` | COVERED |
| GET | `/api/nqe/repos/fwd/commits/head/queries` | `NQE.ListQueries` | COVERED |
| GET | `/api/nqe/repos/org/commits/head` | `NQERepository.Head` | COVERED |
| GET | `/api/nqe/repos/org/commits/head/queries` | `NQERepository.ListHeadQueries` | COVERED |
| GET | `/api/nqe/repos/org/commits/{commitId}/queries` | `NQERepository.GetQuery` | COVERED |
| GET | `/api/orgs/current` | `Organizations.Current` | COVERED |
| GET | `/api/public/csrf` | `Browser.PublicCSRFAPI` | COVERED |
| GET | `/api/snapshots/{snapshotId}` | `Snapshots.Download` | COVERED |
| GET | `/api/snapshots/{snapshotId}/checks` | `Checks.List`, `Compatibility.Checks` | COVERED |
| GET | `/api/snapshots/{snapshotId}/topology` | `Topology.List` | COVERED |
| GET | `/api/snapshots/{snapshotId}/topology/overrides` | `Topology.Overrides` | COVERED |
| GET | `/api/users/current` | `Users.Current`, `Browser.CurrentUser` | COVERED |
| GET | `/api/users/current/tokens` | `Users.ListTokens` | COVERED |
| GET | `/api/users/{userId}/roles` | `Users.Roles` | COVERED |
| GET | `/api/version` | `Version.Get`, `Version.Reachable` | COVERED |
| GET | `/api/webhooks` | `Webhooks.List` | COVERED |
| GET | `/backup-settings` | `Backups.GetSettings` | COVERED |
| GET | `/backup-settings/storage` | `Backups.GetS3Storage` | COVERED |
| GET | `/backups` | `Backups.Last` | COVERED |
| GET | `/login` | `Browser.LoginPageCSRF` | COVERED |
| GET | `/public/csrf` | `Browser.PublicCSRFLegacy` | COVERED |
| PATCH | `/api/collection-settings` | `Collectors.PatchOrganizationSettings` | COVERED |
| PATCH | `/api/collectors/{collectorId}/collection-settings` | `Collectors.PatchSettings` | COVERED |
| PATCH | `/api/device-access-labels/{labelId}` | `AccessControl.UpdateDeviceAccessLabel` | COVERED |
| PATCH | `/api/integrations/servicenow` | `Integrations.PatchServiceNow` | COVERED |
| PATCH | `/api/networks/{networkId}/atlas` | `Locations.Assign` | COVERED |
| PATCH | `/api/networks/{networkId}/cloudAccounts/{accountName}` | `CloudAccounts.Update` | COVERED |
| PATCH | `/api/networks/{networkId}/endpoints/{name}` | `Endpoints.Patch` | COVERED |
| PATCH | `/api/networks/{networkId}/http-credentials/{credentialId}` | `Credentials.UpdateHTTPWithResult` | COVERED |
| PATCH | `/api/networks/{networkId}/locations/{locationId}/clusters/{clusterName}` | `Locations.PatchCluster` | COVERED |
| PATCH | `/api/networks/{networkId}/performance/settings` | `Collectors.SetPerformanceCollection` | COVERED |
| PATCH | `/api/networks/{networkId}/proxies/{proxyId}` | `Proxies.Update` | COVERED |
| PATCH | `/api/networks/{networkId}/rapid7-sources/{sourceName}` | `Integrations.UpdateRapid7` | COVERED |
| PATCH | `/api/snapshots/{snapshotId}` | `Snapshots.Favorite` | COVERED |
| PATCH | `/api/users/{userId}` | `Admin.PatchUser` | COVERED |
| PATCH | `/api/webhooks/{name}` | `Webhooks.Update` | COVERED |
| PATCH | `/backup-settings` | `Backups.UpdateSettings` | COVERED |
| PATCH | `/backup-settings/storage` | `Backups.UpdateS3Storage` | COVERED |
| POST | `/api/access-control-groups` | `AccessControl.CreateGroup` | COVERED |
| POST | `/api/access-control-groups/{groupId}` | `AccessControl.UpdateGroup` | COVERED |
| POST | `/api/admin/orgs` | `Organizations.Create` | COVERED |
| POST | `/api/ai-chats` | `AI.StartChat` | COVERED |
| POST | `/api/ai-chats/{chatId}/messages` | `AI.AddMessage` | COVERED |
| POST | `/api/auth/login` | `Browser.LoginAPI` | COVERED |
| POST | `/api/collector-tasks` | `CollectorTasks.Start`, `Compatibility.StartCollectorTask` | COVERED |
| POST | `/api/collectors` | `Collectors.Register` | COVERED |
| POST | `/api/custom-banners` | `Banners.Create` | COVERED |
| POST | `/api/device-access-labels` | `AccessControl.CreateDeviceAccessLabel` | COVERED |
| POST | `/api/endpoint-profiles` | `Endpoints.CreateProfile` | COVERED |
| POST | `/api/integrations/infoblox/instances` | `Integrations.CreateInfoblox` | COVERED |
| POST | `/api/integrations/servicenow-cmdb` | `Integrations.ServiceNowCMDBSchema`, `Integrations.EnableServiceNowCMDB` | COVERED |
| POST | `/api/integrations/servicenow-cmdb/configuration` | `Integrations.SaveServiceNowCMDB` | COVERED |
| POST | `/api/internal/networks/{networkId}/performance` | `Performance.GenerateSynthetic` | COVERED |
| POST | `/api/networks` | `Networks.Create` | COVERED |
| POST | `/api/networks/{networkId}/change-sets` | `Predict.CreateChangeSet` | COVERED |
| POST | `/api/networks/{networkId}/change-sets/{changeSetId}` | `Predict.Run` | COVERED |
| POST | `/api/networks/{networkId}/change-sets/{changeSetId}/devices/{device}/cli-assists` | `AIAssist.GeneratePredictCLI` | COVERED |
| POST | `/api/networks/{networkId}/change-sets/{changeSetId}/draft/devices/{device}/bgp-advertisements` | `Predict.StageBGPAdvertisement` | COVERED |
| POST | `/api/networks/{networkId}/change-sets/{changeSetId}/overview-assists` | `AIAssist.GeneratePredictOverview` | COVERED |
| POST | `/api/networks/{networkId}/classic-devices` | `ClassicDevices.PutBatch` | COVERED |
| POST | `/api/networks/{networkId}/cli-credentials` | `Credentials.CreateCLI` | COVERED |
| POST | `/api/networks/{networkId}/cloudAccounts` | `CloudAccounts.Create` | COVERED |
| POST | `/api/networks/{networkId}/cloudAccounts/{accountName}/test` | `CloudAccounts.Test` | COVERED |
| POST | `/api/networks/{networkId}/device-metrics-history` | `Performance.DeviceMetricHistory`, `Performance.DeviceMetricHistoryDocument` | COVERED |
| POST | `/api/networks/{networkId}/device-tags` | `DeviceTags.AddBatch`, `DeviceTags.AddBatchTo` | COVERED |
| POST | `/api/networks/{networkId}/endpoints` | `Endpoints.AddBatch` | COVERED |
| POST | `/api/networks/{networkId}/http-credentials` | `Credentials.CreateHTTP` | COVERED |
| POST | `/api/networks/{networkId}/interface-metrics-history` | `Performance.InterfaceMetricHistory`, `Performance.InterfaceMetricHistoryDocument` | COVERED |
| POST | `/api/networks/{networkId}/jump-servers` | `JumpServers.Create` | COVERED |
| POST | `/api/networks/{networkId}/jumpServers` | `JumpServers.CreateLegacy` | COVERED |
| POST | `/api/networks/{networkId}/locations` | `Locations.Create` | COVERED |
| POST | `/api/networks/{networkId}/locations/{locationId}/clusters` | `Locations.CreateCluster` | COVERED |
| POST | `/api/networks/{networkId}/performance` | `Performance.UploadWithIdentity` | COVERED |
| POST | `/api/networks/{networkId}/proxies` | `Proxies.Create` | COVERED |
| POST | `/api/networks/{networkId}/rapid7-sources` | `Integrations.CreateRapid7` | COVERED |
| POST | `/api/networks/{networkId}/snapshots` | `Snapshots.Upload`, `Snapshots.UploadMergeCompatibility` | COVERED |
| POST | `/api/networks/{networkId}/snmpCredentials` | `Credentials.CreateSNMP` | COVERED |
| POST | `/api/networks/{networkId}/startcollection` | `Collectors.StartLegacy` | COVERED |
| POST | `/api/networks/{networkId}/unhealthy-interfaces` | `Performance.UnhealthyInterfaces` | COVERED |
| POST | `/api/networks/{networkId}/workspaces` | `Networks.CreateWorkspace` | COVERED |
| POST | `/api/nqe` | `NQE.Run` | COVERED |
| POST | `/api/nqe/repos/org/commits` | `NQERepository.Commit` | COVERED |
| POST | `/api/orgs/{orgId}/users` | `Admin.CreateOrganizationUser` | COVERED |
| POST | `/api/snapshots/{snapshotId}` | `Snapshots.Reprocess`, `Snapshots.Invalidate`, `Snapshots.ExportSubset`, `Snapshots.ExportSubsetCompatibility` | COVERED |
| POST | `/api/snapshots/{snapshotId}/checks` | `Checks.CreatePersistent` | COVERED |
| POST | `/api/snapshots/{snapshotId}/topology/overrides` | `Topology.EditOverrides` | COVERED |
| POST | `/api/users/current/nqe/changes` | `NQERepository.DeleteDirectory`, `NQERepository.AddDirectory`, `NQERepository.AddQuery` | COVERED |
| POST | `/api/users/current/password` | `Users.ResetPassword` | COVERED |
| POST | `/api/users/current/tokens` | `Users.CreateToken` | COVERED |
| POST | `/api/users/{userId}/roles/org/ADMIN` | `Admin.GrantOrganizationAdmin` | COVERED |
| POST | `/api/users/{userId}/supported-orgs` | `Admin.AddSupportedOrganization` | COVERED |
| POST | `/api/webhooks` | `Webhooks.Create` | COVERED |
| POST | `/backup-settings` | `Backups.SetS3BucketOwnership` | COVERED |
| POST | `/backups` | `Backups.Trigger` | COVERED |
| POST | `/login` | `Browser.LoginLegacy` | COVERED |
| PUT | `/api/auth/saml-settings` | `SAML.PutSettings` | COVERED |
| PUT | `/api/custom-banners/{bannerId}` | `Banners.Replace` | COVERED |
| PUT | `/api/deployment-config/{property}` | `Configuration.SetDeployment` | COVERED |
| PUT | `/api/integrations/servicenow` | `Integrations.PutServiceNow` | COVERED |
| PUT | `/api/networks/{networkId}/change-sets/{changeSetId}/draft/devices/{device}/commands` | `Predict.StageCommands` | COVERED |
| PUT | `/api/networks/{networkId}/collector` | `Collectors.Attach` | COVERED |
| PUT | `/api/orgs/{orgId}/config/{property}` | `Properties.SetOrganization` | COVERED |
| PUT | `/api/users/{userId}/supported-orgs` | `Admin.SetSupportedOrganizations` | COVERED |
