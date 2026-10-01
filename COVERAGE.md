# Forward API typed coverage

This file is generated from `coverage_manifest.json`; run `go generate ./...` to refresh it. The manifest was derived from Skyforge docs/forward-api-sdk-migration-audit.md plus the corrections appended there on 2026-08-05; 2026-09-06: access control, SAML settings and user roles added after diffing every route Skyforge sends against this manifest; 2026-09-07: controller-managed setup guests -- ManagedDevice plus the setup PATCH that declares them; 2026-09-11: Software Central deployment artifacts for the vSphere Forward-VM lane; 2026-09-12: /api/cve-index (GET body + view=metadata, PUT ?sha=, DELETE) from CveIndexCloudController, for the CVE index sync that used to be curl.; 2026-09-13: trusted-certificates List/Add/Apply/Delete added for forwardEnsureCollectorCATrust (cloud-proxy MITM CA trust + per-collector apply dispatch), replacing the disabled org-global-apply belief in forward_client_cloud.go.; 2026-09-14: change-set commits (Predict.Commit) for the cloud-predict change loop.; 2026-09-23: org-property surface for the live Experimental card -- GET /api/orgs/{orgId}/config (Organization, DescribeOrganization), PUT/DELETE /api/global-config/{property}. Audit rows C028-C030.; 2026-09-27: dead routes removed after checking every route against the Spring mappings at primary 15398425a69 and stable 67e89c87124 -- JumpServers.Create onto /jumpServers (NewJumpServer body), Backups onto /api/backup-settings + /api/backups, and deleted: Collections.List/Progress, Collectors.Get, Collectors.StartLegacy, Browser.LoginAPI/PublicCSRFLegacy, Integrations.PutServiceNow.; 2026-09-27: every request-issuing exported service method mapped (coverage_completeness_test.go now fails on any that is not), including kind-dependent methods that send several routes (SyntheticNodes Get/Put/Delete/List) and composites that send their callees' routes (Diffs.MaterialSummary, Snapshots.Get/Operation, CollectorTasks.StartOperation, Properties.DescribeOrganization now also lists GET /api/global-config). predict_cloud.go stays out as pre-release (FWD-59003).; 2026-09-27: Snapshots.Get reads the listing only -- GET /api/networks/{networkId}/snapshots/{snapshotId} is not mapped at primary 15398425a69 or stable 67e89c87124.; 2026-09-28: change-demo review surface for the Skyforge changedemo SDK migration -- Diffs.Checks (per-type check diff rows), Diffs subnet-connectivity summary + wait + view=locations/locationPair, Predict.StageBGPAdvertisements (action=bulkAdd), Predict.ListChangeSetChecks (change-set checks per snapshot), Predict.ValidateCommands (devices/{d}/commands?action=validate); every route checked against the Spring mappings at primary 15398425a69 and stable 67e89c87124 (Skyforge forwardrawgate routes tables).; 2026-09-29: org-admin user lifecycle for workshop seat provisioning -- POST/GET /api/users (Users.Create/List; /api/orgs/{orgId}/users is support-admin only), per-network roles /api/users/{userId}/roles/network/{networkId}[/{role}], another user's tokens /api/users/{userId}/tokens[/{tokenName}], and GET /api/software/client for the headless collector, from UserController and ReadOnlyClientSoftwareController.; 2026-09-29: NQERepository.GetQueryByID -- GET /api/nqe/queries/{queryId}/source-code?commitId= (NqeLibController.getQuerySourceCode, mapped at primary 15398425a69 and stable 67e89c87124): a committed query by stable id at a commit, for the change demo state families (the raw read it replaces, GET .../commits/{id}/queries with no path, is mapped on neither build).; 2026-09-29: NQE.ListQueries corrected to GET /api/nqe/queries (NqeLibController.java:262 on primary 15398425a69 and stable 67e89c87124) -- the code has always sent that route, while the manifest said GET /api/nqe/repos/fwd/commits/head/queries, so route liveness checked a route the method never calls. coverage_routes_test.go now drives every manifest symbol against a recording server and fails on any request the manifest does not declare for it.; 2026-09-30: Snapshots.Metrics (GET /api/snapshots/{snapshotId}/metrics) and Snapshots.Exceptions (GET /api/snapshots/{snapshotId}/exceptions?view=json), SnapshotController on primary 15398425a69 and stable 67e89c87124, for the Forward Skills collection-failure investigation.; 2026-09-30: Devices.Missing -- GET /api/networks/{networkId}/missing-devices?snapshotId= (DeviceController.getMissingDevices, published; primary 15398425a69 and stable 67e89c87124), for the Forward Skills collection-failure investigation.; 2026-09-30: Vulnerabilities.List (GET /api/networks/{networkId}/vulnerabilities?v=2, getVulnerabilities), Vulnerabilities.ListByOS (the same route without v=2, getOsVulnerabilities) and Vulnerabilities.Get (GET /api/networks/{networkId}/vulnerabilities/{cveId}, getVulnerability) -- the published Vulnerability Analysis API (fwd/api/apis/vulnerability-analysis.yaml), VulnerabilityAnalysisController on primary 15398425a69 and stable 67e89c87124.; 2026-09-30: Aliases.List (GET /api/snapshots/{snapshotId}/aliases), Checks.ListPredefined (GET /api/predefinedChecks), Networks.L7Applications (GET /api/l7-applications) -- all published, on primary 15398425a69 and stable 67e89c87124; Diffs.Files joins GET /api/diffs/{a}/{b}/files (the changed-file listing; Forward has no diff-text route) and Devices.DownloadFileHead joins the device file download (a client-side cap, since the route does not honor Range).; 2026-10-01: DeviceTags.RemoveBatchFrom joins the device-tags POST route (action=removeBatchFrom, the undo of AddBatchTo; operationId removeDeviceTagsFromDevices). 2026-10-01: SyntheticNodes.SetQuery (PATCH queryId on the internet, intranet and L3 VPN node routes; preview, FWD-37393).; 2026-10-01: SyntheticNodes.ComputeQuery (POST ?action=computeNqeBasedConnections&queryId= on the internet-node, intranet-nodes and l3-vpns routes, a dry run of a node's NQE query) and SyntheticNodes.CompatibleQueries (GET /api/synthetic-device-queries?type=&networkId=) -- preview, on primary 15398425a69 and stable 67e89c87124. 2026-10-01: SyntheticNodes.ComputeConnections, ListQueries, InternetConnectionSuggestions and the L2 VPN and adjacent network kinds (preview, FWD-37393).; 2026-10-01: L2 VPN and adjacent-network kinds on SyntheticNodes (Get, List, SetQuery, ComputeQuery, Delete; Put refuses them, their connection shapes are not modelled), CompatibleQueries for all five kinds, and SyntheticNodes.InternetConnectionSuggestions (GET /api/networks/{networkId}/internet-node/connection-suggestions) -- preview, on primary 15398425a69 and stable 67e89c87124.; 2026-10-01: WanCircuits (List, Get, Put, Patch, Delete, ReplaceAll; the published WAN Circuits API) and backdate -- POST ?op=backdate&snapshotId= on internet-node, intranet-nodes, l3-vpns, l2-vpns, adjacent-networks (SyntheticNodes.Backdate) and wan-circuits (WanCircuits.Backdate), ?action=backdate on link-overrides (Topology.BackdateLinkOverrides); each invalidates every snapshot from the one named onward. All on primary 15398425a69 and stable 67e89c87124..

Current inventory: **419 semantic call sites**, **290 distinct normalized method+route pairs**, **290 COVERED**, **0 PARTIAL**, **0 MISSING**.

The inventory becomes stale when the consumer adds, removes, or changes a Forward wire call. Run `go run ./cmd/skyforge-coverage -audit /path/to/skyforge/docs/forward-api-sdk-migration-audit.md` to diff the audited route set. The command fails when its audit input is missing.

| Method | Normalized route | Typed SDK symbol(s) | Class |
|---|---|---|---|
| DELETE | `/api/access-control-groups/{groupId}` | `AccessControl.DeleteGroup` | COVERED |
| DELETE | `/api/admin/orgs/{orgId}` | `Organizations.Delete` | COVERED |
| DELETE | `/api/ai-chats/{chatId}` | `AI.DeleteChat` | COVERED |
| DELETE | `/api/collectors/{collectorIdOrName}` | `Collectors.Delete` | COVERED |
| DELETE | `/api/config/{property}` | `Properties.ClearCurrent` | COVERED |
| DELETE | `/api/cve-index` | `CVEIndex.Delete` | COVERED |
| DELETE | `/api/device-access-labels/{labelId}` | `AccessControl.DeleteDeviceAccessLabel` | COVERED |
| DELETE | `/api/global-config/{property}` | `Properties.ClearGlobal` | COVERED |
| DELETE | `/api/integrations/servicenow` | `Integrations.DeleteServiceNow` | COVERED |
| DELETE | `/api/networks/{networkId}` | `Networks.Delete` | COVERED |
| DELETE | `/api/networks/{networkId}/adjacent-networks/{adjacentNetworkName}` | `SyntheticNodes.Delete` | COVERED |
| DELETE | `/api/networks/{networkId}/change-sets/{changeSetId}/devices/{deviceName}/scopes/{scopeId}/rulebases/{rulebaseId}/security-rules/{uuid}` | `Predict.RemoveSecurityRule` | COVERED |
| DELETE | `/api/networks/{networkId}/change-sets/{id}` | `Predict.DeleteChangeSet` | COVERED |
| DELETE | `/api/networks/{networkId}/classic-devices/{deviceName}` | `ClassicDevices.Delete` | COVERED |
| DELETE | `/api/networks/{networkId}/cli-credentials/{credentialId}` | `Credentials.DeleteCLI` | COVERED |
| DELETE | `/api/networks/{networkId}/cloud-managed-setups/{setupName}` | `CloudManagedSetups.DeleteMist` | COVERED |
| DELETE | `/api/networks/{networkId}/cloudAccounts/{accountName}` | `CloudAccounts.Delete` | COVERED |
| DELETE | `/api/networks/{networkId}/controller-managed-setups/{setupName}` | `ControllerManagedSetups.Delete` | COVERED |
| DELETE | `/api/networks/{networkId}/endpoints/{name}` | `Endpoints.Delete` | COVERED |
| DELETE | `/api/networks/{networkId}/http-credentials/{credentialId}` | `Credentials.DeleteHTTP` | COVERED |
| DELETE | `/api/networks/{networkId}/intranet-nodes/{nodeName}` | `SyntheticNodes.DeleteIntranetNode`, `SyntheticNodes.Delete` | COVERED |
| DELETE | `/api/networks/{networkId}/l2-vpns/{l2VpnName}` | `SyntheticNodes.Delete` | COVERED |
| DELETE | `/api/networks/{networkId}/l3-vpns/{l3VpnName}` | `SyntheticNodes.DeleteL3VPN`, `SyntheticNodes.Delete` | COVERED |
| DELETE | `/api/networks/{networkId}/snmpCredentials/{credentialId}` | `Credentials.DeleteSNMP` | COVERED |
| DELETE | `/api/networks/{networkId}/wan-circuits/{wanCircuitName}` | `WanCircuits.Delete` | COVERED |
| DELETE | `/api/orgs/{orgId}/config/software_central` | `Properties.ClearOrganization` | COVERED |
| DELETE | `/api/orgs/{orgId}/licenses` | `Licensing.RemoveAllForOrg` | COVERED |
| DELETE | `/api/snapshots/{snapshotId}` | `Snapshots.Delete` | COVERED |
| DELETE | `/api/snapshots/{snapshotId}/checks` | `Checks.DeactivateAll` | COVERED |
| DELETE | `/api/snapshots/{snapshotId}/checks/{checkId}` | `Checks.Deactivate` | COVERED |
| DELETE | `/api/trusted-certificates/{name}` | `TrustedCertificates.Delete` | COVERED |
| DELETE | `/api/users/current/tokens/{tokenName}` | `Users.DeleteToken` | COVERED |
| DELETE | `/api/users/{userId}` | `Admin.DeleteUser` | COVERED |
| DELETE | `/api/users/{userId}/roles/network/{networkId}` | `Users.ClearNetworkRoles` | COVERED |
| DELETE | `/api/users/{userId}/roles/network/{networkId}/{role}` | `Users.RemoveNetworkRole` | COVERED |
| DELETE | `/api/users/{userId}/tokens/{tokenName}` | `Users.DeleteTokenFor` | COVERED |
| DELETE | `/api/webhooks/{webhookName}` | `Webhooks.Delete` | COVERED |
| GET | `/api/access-control-groups` | `AccessControl.ListGroups` | COVERED |
| GET | `/api/admin/impersonate` | `Browser.Impersonate`, `Browser.ImpersonateWithServiceCredential` | COVERED |
| GET | `/api/admin/networks` | `Admin.ListNetworks` | COVERED |
| GET | `/api/admin/orgs` | `Organizations.List` | COVERED |
| GET | `/api/admin/users` | `Admin.ListUsers` | COVERED |
| GET | `/api/admin/users/{idOrUsername}` | `Admin.LookupUser` | COVERED |
| GET | `/api/ai-chats` | `AI.ListChats` | COVERED |
| GET | `/api/ai-chats/{chatId}` | `AI.GetChat`, `AI.StartChatOperation` | COVERED |
| GET | `/api/ai-chats/{chatId}/messages` | `AI.ListMessages` | COVERED |
| GET | `/api/auth/saml-settings` | `SAML.GetSettings` | COVERED |
| GET | `/api/backup-settings` | `Backups.GetSettings` | COVERED |
| GET | `/api/backup-settings/storage` | `Backups.GetS3Storage` | COVERED |
| GET | `/api/backups` | `Backups.Last` | COVERED |
| GET | `/api/collector-tasks` | `CollectorTasks.List`, `CollectorTasks.Progress` | COVERED |
| GET | `/api/collector-tasks/{taskId}` | `CollectorTasks.Get`, `CollectorTasks.StartOperation`, `Snapshots.Collect`, `TrustedCertificates.ApplyOperations` | COVERED |
| GET | `/api/collectors` | `Collectors.List` | COVERED |
| GET | `/api/config` | `Properties.Current` | COVERED |
| GET | `/api/custom-banners` | `Banners.List` | COVERED |
| GET | `/api/cve-index` | `CVEIndex.Download`, `CVEIndex.Metadata` | COVERED |
| GET | `/api/deployment-artifacts` | `SoftwareCentral.ListDeploymentArtifacts`, `SoftwareCentral.ListForwardApplianceOVAs` | COVERED |
| GET | `/api/deployment-artifacts/{artifactId}` | `SoftwareCentral.DeploymentArtifactURL`, `SoftwareCentral.DownloadDeploymentArtifact` | COVERED |
| GET | `/api/deployment-config/{property}` | `Configuration.GetDeployment` | COVERED |
| GET | `/api/device-access-labels` | `AccessControl.ListDeviceAccessLabels` | COVERED |
| GET | `/api/diffs/{snapshotAId}/{snapshotBId}/acl` | `Diffs.Count`, `Diffs.MaterialSummary` | COVERED |
| GET | `/api/diffs/{snapshotAId}/{snapshotBId}/checks` | `Diffs.Checks`, `Diffs.ChecksCount`, `Diffs.MaterialSummary` | COVERED |
| GET | `/api/diffs/{snapshotAId}/{snapshotBId}/cloud-acl` | `Diffs.Count`, `Diffs.MaterialSummary` | COVERED |
| GET | `/api/diffs/{snapshotAId}/{snapshotBId}/cloud-objects` | `Diffs.Count`, `Diffs.MaterialSummary` | COVERED |
| GET | `/api/diffs/{snapshotAId}/{snapshotBId}/devices` | `Diffs.Count`, `Diffs.Devices`, `Diffs.MaterialSummary` | COVERED |
| GET | `/api/diffs/{snapshotAId}/{snapshotBId}/files` | `Diffs.FilesCount`, `Diffs.MaterialSummary`, `Diffs.Files` | COVERED |
| GET | `/api/diffs/{snapshotAId}/{snapshotBId}/interfaces` | `Diffs.Count`, `Diffs.MaterialSummary` | COVERED |
| GET | `/api/diffs/{snapshotAId}/{snapshotBId}/l2` | `Diffs.Count`, `Diffs.MaterialSummary` | COVERED |
| GET | `/api/diffs/{snapshotAId}/{snapshotBId}/routing-loop/count` | `Diffs.Count`, `Diffs.MaterialSummary` | COVERED |
| GET | `/api/diffs/{snapshotAId}/{snapshotBId}/subnet-connectivity` | `Diffs.ConnectivityDiffLocationPair`, `Diffs.ConnectivityDiffLocations`, `Diffs.SubnetConnectivity`, `Diffs.WaitForSubnetConnectivity` | COVERED |
| GET | `/api/diffs/{snapshotAId}/{snapshotBId}/topology` | `Diffs.Count`, `Diffs.MaterialSummary` | COVERED |
| GET | `/api/endpoint-profiles` | `Endpoints.ListProfiles` | COVERED |
| GET | `/api/global-config` | `Properties.Global`, `Properties.DescribeOrganization` | COVERED |
| GET | `/api/integrations/infoblox` | `Integrations.ListInfobloxLegacy` | COVERED |
| GET | `/api/integrations/infoblox/instances` | `Integrations.ListInfoblox` | COVERED |
| GET | `/api/integrations/servicenow` | `Integrations.GetServiceNow` | COVERED |
| GET | `/api/l7-applications` | `Networks.L7Applications` | COVERED |
| GET | `/api/networks` | `Networks.List`, `Networks.CheckAccess` | COVERED |
| GET | `/api/networks/{networkId}/adjacent-networks` | `SyntheticNodes.List` | COVERED |
| GET | `/api/networks/{networkId}/adjacent-networks/{adjacentNetworkName}` | `SyntheticNodes.Get` | COVERED |
| GET | `/api/networks/{networkId}/change-sets` | `Predict.ListChangeSets` | COVERED |
| GET | `/api/networks/{networkId}/change-sets/{changeSetId}/checks` | `Predict.ListChangeSetChecks` | COVERED |
| GET | `/api/networks/{networkId}/change-sets/{changeSetId}/devices/{deviceName}/security-rules-diff` | `Predict.SecurityRulesDiff` | COVERED |
| GET | `/api/networks/{networkId}/change-sets/{changeSetId}/predicted-snapshots` | `Predict.ListPredictedSnapshots` | COVERED |
| GET | `/api/networks/{networkId}/classic-devices` | `ClassicDevices.List`, `ClassicDevices.ListTestStatuses` | COVERED |
| GET | `/api/networks/{networkId}/classic-devices/{deviceName}` | `ClassicDevices.Get` | COVERED |
| GET | `/api/networks/{networkId}/cli-credentials` | `Credentials.ListCLI` | COVERED |
| GET | `/api/networks/{networkId}/cli-credentials/{credentialId}` | `Credentials.GetCLI` | COVERED |
| GET | `/api/networks/{networkId}/cloud-managed-setups` | `CloudManagedSetups.ListMist` | COVERED |
| GET | `/api/networks/{networkId}/cloudAccounts` | `CloudAccounts.List`, `CloudAccounts.Get` | COVERED |
| GET | `/api/networks/{networkId}/cloudAccounts/aws/assumeRole/externalId` | `CloudAccounts.AWSAssumeRoleExternalID` | COVERED |
| GET | `/api/networks/{networkId}/collector` | `Collectors.Attachment` | COVERED |
| GET | `/api/networks/{networkId}/controller-managed-setups` | `ControllerManagedSetups.List` | COVERED |
| GET | `/api/networks/{networkId}/device-metrics` | `Performance.DeviceMetrics`, `Performance.DeviceMetricsDocument` | COVERED |
| GET | `/api/networks/{networkId}/device-statuses` | `Collections.DeviceStatuses` | COVERED |
| GET | `/api/networks/{networkId}/device-tags` | `DeviceTags.List` | COVERED |
| GET | `/api/networks/{networkId}/devices` | `Devices.List` | COVERED |
| GET | `/api/networks/{networkId}/devices/{deviceIdOrName}` | `Devices.Get` | COVERED |
| GET | `/api/networks/{networkId}/devices/{deviceIdOrName}/files` | `Devices.ListFiles` | COVERED |
| GET | `/api/networks/{networkId}/devices/{deviceName}/files/{fileName}` | `Devices.DownloadFile`, `Devices.DownloadFileHead` | COVERED |
| GET | `/api/networks/{networkId}/end-host-scanners` | `Integrations.ListRapid7` | COVERED |
| GET | `/api/networks/{networkId}/endpoints` | `Endpoints.List`, `Endpoints.ListTestStatuses` | COVERED |
| GET | `/api/networks/{networkId}/http-credentials` | `Credentials.ListHTTP` | COVERED |
| GET | `/api/networks/{networkId}/http-credentials/{credentialId}` | `Credentials.GetHTTP` | COVERED |
| GET | `/api/networks/{networkId}/interface-metrics` | `Performance.InterfaceMetrics`, `Performance.InterfaceMetricsDocument` | COVERED |
| GET | `/api/networks/{networkId}/internet-node` | `SyntheticNodes.GetInternetNode`, `SyntheticNodes.Get` | COVERED |
| GET | `/api/networks/{networkId}/internet-node/connection-suggestions` | `SyntheticNodes.InternetConnectionSuggestions` | COVERED |
| GET | `/api/networks/{networkId}/intranet-nodes` | `SyntheticNodes.ListIntranetNodes`, `SyntheticNodes.List` | COVERED |
| GET | `/api/networks/{networkId}/intranet-nodes/{nodeName}` | `SyntheticNodes.GetIntranetNode`, `SyntheticNodes.Get` | COVERED |
| GET | `/api/networks/{networkId}/jumpServers` | `JumpServers.List` | COVERED |
| GET | `/api/networks/{networkId}/l2-vpns` | `SyntheticNodes.List` | COVERED |
| GET | `/api/networks/{networkId}/l2-vpns/{l2VpnName}` | `SyntheticNodes.Get` | COVERED |
| GET | `/api/networks/{networkId}/l3-vpns` | `SyntheticNodes.ListL3VPNs`, `SyntheticNodes.List` | COVERED |
| GET | `/api/networks/{networkId}/l3-vpns/{l3VpnName}` | `SyntheticNodes.GetL3VPN`, `SyntheticNodes.Get` | COVERED |
| GET | `/api/networks/{networkId}/locations` | `Locations.List` | COVERED |
| GET | `/api/networks/{networkId}/locations/{locationId}/clusters` | `Locations.ListClusters` | COVERED |
| GET | `/api/networks/{networkId}/missing-devices` | `Devices.Missing` | COVERED |
| GET | `/api/networks/{networkId}/nqe-executions/{executionKey}` | `NQE.StartOperation`, `NQE.Status` | COVERED |
| GET | `/api/networks/{networkId}/nqe-executions/{executionKey}/result` | `NQE.Result`, `NQE.ResultJSONLines` | COVERED |
| GET | `/api/networks/{networkId}/paths` | `Networks.Paths` | COVERED |
| GET | `/api/networks/{networkId}/proxies` | `Proxies.List` | COVERED |
| GET | `/api/networks/{networkId}/snapshots` | `Snapshots.List`, `Snapshots.ListDocument`, `Compatibility.ListSnapshots`, `CollectorTasks.StartCollectionOperation`, `Predict.RunOperation`, `Snapshots.Collect`, `Snapshots.ForCollectionTask`, `Snapshots.Get`, `Snapshots.LatestCollected`, `Snapshots.LatestProcessed`, `Snapshots.Operation`, `Snapshots.ResolveID` | COVERED |
| GET | `/api/networks/{networkId}/snmpCredentials` | `Credentials.ListSNMP`, `Credentials.ListSNMPCredentials` | COVERED |
| GET | `/api/networks/{networkId}/unhealthy-devices` | `Performance.UnhealthyDevices` | COVERED |
| GET | `/api/networks/{networkId}/vulnerabilities` | `Vulnerabilities.List`, `Vulnerabilities.ListByOS` | COVERED |
| GET | `/api/networks/{networkId}/vulnerabilities/{cveId}` | `Vulnerabilities.Get` | COVERED |
| GET | `/api/networks/{networkId}/wan-circuits` | `WanCircuits.List` | COVERED |
| GET | `/api/networks/{networkId}/wan-circuits/{wanCircuitName}` | `WanCircuits.Get` | COVERED |
| GET | `/api/nqe/queries` | `NQE.ListQueries` | COVERED |
| GET | `/api/nqe/queries/{queryId}/source-code` | `NQERepository.GetQueryByID` | COVERED |
| GET | `/api/nqe/repos/org/commits/head` | `NQERepository.Head` | COVERED |
| GET | `/api/nqe/repos/org/commits/head/queries` | `NQERepository.ListHeadQueries` | COVERED |
| GET | `/api/nqe/repos/org/commits/{commitId}/queries` | `NQERepository.GetQuery` | COVERED |
| GET | `/api/orgs/current` | `Organizations.Current` | COVERED |
| GET | `/api/orgs/{orgId}/config` | `Properties.Organization`, `Properties.DescribeOrganization` | COVERED |
| GET | `/api/orgs/{orgId}/licenses` | `Licensing.ListForOrg` | COVERED |
| GET | `/api/predefinedChecks` | `Checks.ListPredefined` | COVERED |
| GET | `/api/public/csrf` | `Browser.PublicCSRFAPI`, `Browser.Login` | COVERED |
| GET | `/api/snapshots/{snapshotId}` | `Snapshots.Download` | COVERED |
| GET | `/api/snapshots/{snapshotId}/aliases` | `Aliases.List` | COVERED |
| GET | `/api/snapshots/{snapshotId}/checks` | `Checks.List`, `Compatibility.Checks`, `Checks.ExistingNames`, `Checks.ForScoring` | COVERED |
| GET | `/api/snapshots/{snapshotId}/checks/{checkId}` | `Checks.Get` | COVERED |
| GET | `/api/snapshots/{snapshotId}/exceptions` | `Snapshots.Exceptions` | COVERED |
| GET | `/api/snapshots/{snapshotId}/metrics` | `Snapshots.Metrics` | COVERED |
| GET | `/api/snapshots/{snapshotId}/topology` | `Topology.List` | COVERED |
| GET | `/api/snapshots/{snapshotId}/topology/overrides` | `Topology.Overrides` | COVERED |
| GET | `/api/software/client` | `SoftwareCentral.DownloadClientPackage` | COVERED |
| GET | `/api/synthetic-device-queries` | `SyntheticNodes.CompatibleQueries` | COVERED |
| GET | `/api/trusted-certificates` | `TrustedCertificates.List` | COVERED |
| GET | `/api/users` | `Users.List` | COVERED |
| GET | `/api/users/current` | `Users.Current`, `Browser.CurrentUser`, `Browser.CurrentSession` | COVERED |
| GET | `/api/users/current/tokens` | `Users.ListTokens` | COVERED |
| GET | `/api/users/{userId}/roles` | `Users.Roles` | COVERED |
| GET | `/api/users/{userId}/tokens` | `Users.ListTokensFor` | COVERED |
| GET | `/api/version` | `Version.Get`, `Version.Reachable` | COVERED |
| GET | `/api/vm/instanceId` | `Licensing.Fingerprint` | COVERED |
| GET | `/api/webhooks` | `Webhooks.List` | COVERED |
| GET | `/login` | `Browser.LoginPageCSRF`, `Browser.Login` | COVERED |
| GET | `/saml2/authenticate/{registrationId}` | `Browser.SAMLAuthenticationRequest` | COVERED |
| PATCH | `/api/admin/orgs/{orgId}` | `Organizations.Update` | COVERED |
| PATCH | `/api/ai-chats/{chatId}` | `AI.RenameChat` | COVERED |
| PATCH | `/api/backup-settings` | `Backups.UpdateSettings` | COVERED |
| PATCH | `/api/backup-settings/storage` | `Backups.UpdateS3Storage` | COVERED |
| PATCH | `/api/collection-settings` | `Collectors.PatchOrganizationSettings` | COVERED |
| PATCH | `/api/collectors/{collectorId}/collection-settings` | `Collectors.PatchSettings` | COVERED |
| PATCH | `/api/device-access-labels/{labelId}` | `AccessControl.UpdateDeviceAccessLabel` | COVERED |
| PATCH | `/api/integrations/servicenow` | `Integrations.PatchServiceNow` | COVERED |
| PATCH | `/api/networks/{networkId}` | `Networks.Update` | COVERED |
| PATCH | `/api/networks/{networkId}/adjacent-networks/{adjacentNetworkName}` | `SyntheticNodes.SetQuery` | COVERED |
| PATCH | `/api/networks/{networkId}/atlas` | `Locations.Assign` | COVERED |
| PATCH | `/api/networks/{networkId}/classic-devices/{deviceName}` | `ClassicDevices.Patch` | COVERED |
| PATCH | `/api/networks/{networkId}/cli-credentials/{credentialId}` | `Credentials.UpdateCLI` | COVERED |
| PATCH | `/api/networks/{networkId}/cloudAccounts/{accountName}` | `CloudAccounts.Update` | COVERED |
| PATCH | `/api/networks/{networkId}/controller-managed-setups/{setupName}` | `ControllerManagedSetups.Patch` | COVERED |
| PATCH | `/api/networks/{networkId}/endpoints/{name}` | `Endpoints.Patch` | COVERED |
| PATCH | `/api/networks/{networkId}/http-credentials/{credentialId}` | `Credentials.UpdateHTTPWithResult`, `Credentials.UpdateHTTP` | COVERED |
| PATCH | `/api/networks/{networkId}/internet-node` | `SyntheticNodes.SetQuery` | COVERED |
| PATCH | `/api/networks/{networkId}/intranet-nodes/{nodeName}` | `SyntheticNodes.SetQuery` | COVERED |
| PATCH | `/api/networks/{networkId}/l2-vpns/{l2VpnName}` | `SyntheticNodes.SetQuery` | COVERED |
| PATCH | `/api/networks/{networkId}/l3-vpns/{l3VpnName}` | `SyntheticNodes.SetQuery` | COVERED |
| PATCH | `/api/networks/{networkId}/locations/{locationId}/clusters/{clusterName}` | `Locations.PatchCluster` | COVERED |
| PATCH | `/api/networks/{networkId}/performance/settings` | `Collectors.SetPerformanceCollection` | COVERED |
| PATCH | `/api/networks/{networkId}/proxies/{proxyId}` | `Proxies.Update` | COVERED |
| PATCH | `/api/networks/{networkId}/rapid7-sources/{sourceName}` | `Integrations.UpdateRapid7` | COVERED |
| PATCH | `/api/networks/{networkId}/snmpCredentials/{credentialId}` | `Credentials.UpdateSNMP` | COVERED |
| PATCH | `/api/networks/{networkId}/wan-circuits/{wanCircuitName}` | `WanCircuits.Patch` | COVERED |
| PATCH | `/api/snapshots/{snapshotId}` | `Snapshots.Favorite`, `Snapshots.SetNote` | COVERED |
| PATCH | `/api/users/{userId}` | `Admin.PatchUser` | COVERED |
| PATCH | `/api/webhooks/{name}` | `Webhooks.Update` | COVERED |
| POST | `/api/access-control-groups` | `AccessControl.CreateGroup` | COVERED |
| POST | `/api/access-control-groups/{groupId}` | `AccessControl.UpdateGroup` | COVERED |
| POST | `/api/admin/orgs` | `Organizations.Create` | COVERED |
| POST | `/api/admin/orgs/{orgId}` | `Organizations.SetEnabled` | COVERED |
| POST | `/api/ai-chats` | `AI.StartChat`, `AI.StartChatOperation` | COVERED |
| POST | `/api/ai-chats/{chatId}/messages` | `AI.AddMessage` | COVERED |
| POST | `/api/backup-settings` | `Backups.SetS3BucketOwnership` | COVERED |
| POST | `/api/backups` | `Backups.Trigger` | COVERED |
| POST | `/api/collector-tasks` | `CollectorTasks.Start`, `Compatibility.StartCollectorTask`, `CollectorTasks.StartCollectionOperation`, `CollectorTasks.StartOperation`, `Snapshots.Collect` | COVERED |
| POST | `/api/collector-tasks/{taskId}` | `CollectorTasks.Stop` | COVERED |
| POST | `/api/collectors` | `Collectors.Register` | COVERED |
| POST | `/api/custom-banners` | `Banners.Create` | COVERED |
| POST | `/api/device-access-labels` | `AccessControl.CreateDeviceAccessLabel` | COVERED |
| POST | `/api/diffs/{snapshotAId}/{snapshotBId}/config-summary-assists` | `AIAssist.SummarizeConfigDiff` | COVERED |
| POST | `/api/diffs/{snapshotAId}/{snapshotBId}/impact-summary-assists` | `AIAssist.SummarizeChangeImpact` | COVERED |
| POST | `/api/endpoint-profiles` | `Endpoints.CreateProfile` | COVERED |
| POST | `/api/integrations/infoblox/instances` | `Integrations.CreateInfoblox` | COVERED |
| POST | `/api/integrations/servicenow-cmdb` | `Integrations.ServiceNowCMDBSchema`, `Integrations.EnableServiceNowCMDB` | COVERED |
| POST | `/api/integrations/servicenow-cmdb/configuration` | `Integrations.SaveServiceNowCMDB` | COVERED |
| POST | `/api/internal/networks/{networkId}/performance` | `Performance.GenerateSynthetic` | COVERED |
| POST | `/api/licenses` | `Licensing.Apply`, `Licensing.Decode` | COVERED |
| POST | `/api/networks` | `Networks.Create` | COVERED |
| POST | `/api/networks/{networkId}/adjacent-networks` | `SyntheticNodes.ComputeQuery`, `SyntheticNodes.Backdate` | COVERED |
| POST | `/api/networks/{networkId}/change-sets` | `Predict.CreateChangeSet` | COVERED |
| POST | `/api/networks/{networkId}/change-sets/{changeSetId}` | `Predict.Run`, `Predict.RunOperation` | COVERED |
| POST | `/api/networks/{networkId}/change-sets/{changeSetId}/commits` | `Predict.Commit` | COVERED |
| POST | `/api/networks/{networkId}/change-sets/{changeSetId}/devices/{deviceName}/scopes/{scopeId}/rulebases/{rulebaseId}/security-rules` | `Predict.AddSecurityRule` | COVERED |
| POST | `/api/networks/{networkId}/change-sets/{changeSetId}/devices/{device}/cli-assists` | `AIAssist.GeneratePredictCLI` | COVERED |
| POST | `/api/networks/{networkId}/change-sets/{changeSetId}/devices/{device}/commands` | `Predict.ValidateCommands` | COVERED |
| POST | `/api/networks/{networkId}/change-sets/{changeSetId}/draft/devices/{device}/bgp-advertisements` | `Predict.StageBGPAdvertisement`, `Predict.StageBGPAdvertisements` | COVERED |
| POST | `/api/networks/{networkId}/change-sets/{changeSetId}/overview-assists` | `AIAssist.GeneratePredictOverview` | COVERED |
| POST | `/api/networks/{networkId}/classic-devices` | `ClassicDevices.PutBatch`, `ClassicDevices.Create` | COVERED |
| POST | `/api/networks/{networkId}/cli-credentials` | `Credentials.CreateCLI` | COVERED |
| POST | `/api/networks/{networkId}/cloud-managed-setups` | `CloudManagedSetups.CreateMist` | COVERED |
| POST | `/api/networks/{networkId}/cloud-managed-setups/{setupName}` | `CloudManagedSetups.DiscoverMist` | COVERED |
| POST | `/api/networks/{networkId}/cloudAccounts` | `CloudAccounts.Create` | COVERED |
| POST | `/api/networks/{networkId}/cloudAccounts/{accountName}/credential` | `CloudAccounts.UpdateCredential` | COVERED |
| POST | `/api/networks/{networkId}/cloudAccounts/{accountName}/test` | `CloudAccounts.Test` | COVERED |
| POST | `/api/networks/{networkId}/controller-managed-setups` | `ControllerManagedSetups.Create` | COVERED |
| POST | `/api/networks/{networkId}/device-metrics-history` | `Performance.DeviceMetricHistory`, `Performance.DeviceMetricHistoryDocument` | COVERED |
| POST | `/api/networks/{networkId}/device-tags` | `DeviceTags.AddBatch`, `DeviceTags.AddBatchTo`, `DeviceTags.RemoveBatchFrom` | COVERED |
| POST | `/api/networks/{networkId}/endpoints` | `Endpoints.AddBatch` | COVERED |
| POST | `/api/networks/{networkId}/http-credentials` | `Credentials.CreateHTTP` | COVERED |
| POST | `/api/networks/{networkId}/interface-metrics-history` | `Performance.InterfaceMetricHistory`, `Performance.InterfaceMetricHistoryDocument` | COVERED |
| POST | `/api/networks/{networkId}/internet-node` | `SyntheticNodes.ComputeQuery`, `SyntheticNodes.Backdate` | COVERED |
| POST | `/api/networks/{networkId}/intranet-nodes` | `SyntheticNodes.ComputeQuery`, `SyntheticNodes.Backdate` | COVERED |
| POST | `/api/networks/{networkId}/jumpServers` | `JumpServers.Create`, `JumpServers.CreateLegacy`, `JumpServers.CreateWithPassword` | COVERED |
| POST | `/api/networks/{networkId}/l2-vpns` | `SyntheticNodes.ComputeQuery`, `SyntheticNodes.Backdate` | COVERED |
| POST | `/api/networks/{networkId}/l3-vpns` | `SyntheticNodes.ComputeQuery`, `SyntheticNodes.Backdate` | COVERED |
| POST | `/api/networks/{networkId}/link-overrides` | `Topology.BackdateLinkOverrides` | COVERED |
| POST | `/api/networks/{networkId}/locations` | `Locations.Create` | COVERED |
| POST | `/api/networks/{networkId}/locations/{locationId}/clusters` | `Locations.CreateCluster` | COVERED |
| POST | `/api/networks/{networkId}/nqe-executions` | `NQE.Start`, `NQE.StartOperation` | COVERED |
| POST | `/api/networks/{networkId}/performance` | `Performance.UploadWithIdentity`, `Performance.Upload` | COVERED |
| POST | `/api/networks/{networkId}/proxies` | `Proxies.Create` | COVERED |
| POST | `/api/networks/{networkId}/rapid7-sources` | `Integrations.CreateRapid7` | COVERED |
| POST | `/api/networks/{networkId}/snapshots` | `Snapshots.Upload`, `Snapshots.UploadMergeCompatibility`, `Snapshots.StartUploadOperation` | COVERED |
| POST | `/api/networks/{networkId}/snmpCredentials` | `Credentials.CreateSNMP`, `Credentials.CreateSNMPCredential` | COVERED |
| POST | `/api/networks/{networkId}/unhealthy-interfaces` | `Performance.UnhealthyInterfaces` | COVERED |
| POST | `/api/networks/{networkId}/wan-circuits` | `WanCircuits.Backdate` | COVERED |
| POST | `/api/networks/{networkId}/workspaces` | `Networks.CreateWorkspace` | COVERED |
| POST | `/api/nqe` | `NQE.Run` | COVERED |
| POST | `/api/nqe-diffs/{before}/{after}` | `NQE.Diff` | COVERED |
| POST | `/api/nqe/doc-assists` | `AIAssist.AskNQEDocs` | COVERED |
| POST | `/api/nqe/doc-assists/{docAssistId}/followup` | `AIAssist.SuggestNQEDocFollowups` | COVERED |
| POST | `/api/nqe/query-assists` | `AIAssist.GenerateNQEQuery` | COVERED |
| POST | `/api/nqe/repos/org/commits` | `NQERepository.Commit` | COVERED |
| POST | `/api/nqe/summary-assists` | `AIAssist.SummarizeNQEQuery` | COVERED |
| POST | `/api/orgs/{orgId}/licenses/{licenseId}` | `Licensing.InvalidateForOrg` | COVERED |
| POST | `/api/orgs/{orgId}/users` | `Admin.CreateOrganizationUser` | COVERED |
| POST | `/api/snapshots/{snapshotId}` | `Snapshots.Reprocess`, `Snapshots.Invalidate`, `Snapshots.ExportSubset`, `Snapshots.ExportSubsetCompatibility` | COVERED |
| POST | `/api/snapshots/{snapshotId}/checks` | `Checks.CreatePersistent`, `Checks.Create` | COVERED |
| POST | `/api/snapshots/{snapshotId}/topology/overrides` | `Topology.EditOverrides` | COVERED |
| POST | `/api/trusted-certificates` | `TrustedCertificates.Add`, `TrustedCertificates.Apply`, `TrustedCertificates.ApplyOperations` | COVERED |
| POST | `/api/users` | `Users.Create` | COVERED |
| POST | `/api/users/current/nqe/changes` | `NQERepository.DeleteDirectory`, `NQERepository.AddDirectory`, `NQERepository.AddQuery`, `NQERepository.DeleteQuery` | COVERED |
| POST | `/api/users/current/password` | `Users.ResetPassword` | COVERED |
| POST | `/api/users/current/tokens` | `Users.CreateToken` | COVERED |
| POST | `/api/users/{userId}/roles/network/{networkId}` | `Users.SetNetworkRole` | COVERED |
| POST | `/api/users/{userId}/roles/network/{networkId}/{role}` | `Users.AddNetworkRole` | COVERED |
| POST | `/api/users/{userId}/roles/org/ADMIN` | `Admin.GrantOrganizationAdmin` | COVERED |
| POST | `/api/users/{userId}/supported-orgs` | `Admin.AddSupportedOrganization` | COVERED |
| POST | `/api/webhooks` | `Webhooks.Create`, `Webhooks.TestNew` | COVERED |
| POST | `/login` | `Browser.LoginLegacy`, `Browser.Login` | COVERED |
| POST | `/login/saml2/sso/{registrationId}` | `Browser.SAMLAssertionConsumer` | COVERED |
| PUT | `/api/auth/saml-settings` | `SAML.PutSettings` | COVERED |
| PUT | `/api/config/{property}` | `Properties.SetCurrent` | COVERED |
| PUT | `/api/custom-banners/{bannerId}` | `Banners.Replace` | COVERED |
| PUT | `/api/cve-index` | `CVEIndex.Put` | COVERED |
| PUT | `/api/deployment-config/{property}` | `Configuration.SetDeployment` | COVERED |
| PUT | `/api/global-config/{property}` | `Properties.SetGlobal` | COVERED |
| PUT | `/api/networks/{networkId}/change-sets/{changeSetId}/draft/devices/{device}/commands` | `Predict.StageCommands` | COVERED |
| PUT | `/api/networks/{networkId}/classic-devices/{deviceName}` | `ClassicDevices.Put` | COVERED |
| PUT | `/api/networks/{networkId}/collector` | `Collectors.Attach` | COVERED |
| PUT | `/api/networks/{networkId}/internet-node` | `SyntheticNodes.PutInternetNode`, `SyntheticNodes.Put` | COVERED |
| PUT | `/api/networks/{networkId}/intranet-nodes/{nodeName}` | `SyntheticNodes.PutIntranetNode`, `SyntheticNodes.Put` | COVERED |
| PUT | `/api/networks/{networkId}/l3-vpns/{l3VpnName}` | `SyntheticNodes.PutL3VPN`, `SyntheticNodes.Put` | COVERED |
| PUT | `/api/networks/{networkId}/wan-circuits` | `WanCircuits.ReplaceAll` | COVERED |
| PUT | `/api/networks/{networkId}/wan-circuits/{wanCircuitName}` | `WanCircuits.Put` | COVERED |
| PUT | `/api/orgs/{orgId}/config/{property}` | `Properties.SetOrganization` | COVERED |
| PUT | `/api/users/{userId}/supported-orgs` | `Admin.SetSupportedOrganizations` | COVERED |
