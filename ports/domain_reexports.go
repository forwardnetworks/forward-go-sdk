package ports

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"time"
)

// The domain names the HTTP adapter reaches, re-exported so it imports ports and nothing else (decision 1).
const AnalysisModeBehavioral = domain.AnalysisModeBehavioral
const AnalysisModeConfigOnly = domain.AnalysisModeConfigOnly
const AppConnectorCollectionManual = domain.AppConnectorCollectionManual
const AppConnectorCollectionSSH = domain.AppConnectorCollectionSSH
const AuditLogMaxLimit = domain.AuditLogMaxLimit
const AuthModeBrowser = domain.AuthModeBrowser
const AuthModeCollector = domain.AuthModeCollector
const AuthModeNone = domain.AuthModeNone
const AuthModeService = domain.AuthModeService
const AuthModeUser = domain.AuthModeUser
const CVEAgeMonth = domain.CVEAgeMonth
const CVEAgeOlder = domain.CVEAgeOlder
const CVEAgeYear = domain.CVEAgeYear
const CapClientCertificateNotParsed = domain.CapClientCertificateNotParsed
const CapClientCertificatePasswordRequired = domain.CapClientCertificatePasswordRequired
const CapabilityCollectorProgress = domain.CapabilityCollectorProgress
const CapabilityPersistentSnapshotChecks = domain.CapabilityPersistentSnapshotChecks
const CapabilityPredict = domain.CapabilityPredict
const CapabilitySnapshotMultipartMerge = domain.CapabilitySnapshotMultipartMerge
const CapabilitySnapshotSubsetExport = domain.CapabilitySnapshotSubsetExport
const CapabilityStructuredBGPAdvertisements = domain.CapabilityStructuredBGPAdvertisements
const CapabilitySupported = domain.CapabilitySupported
const CapabilityUnknown = domain.CapabilityUnknown
const CapabilityUnsupported = domain.CapabilityUnsupported
const CapabilityWorkspaceNetworks = domain.CapabilityWorkspaceNetworks
const ChangeSetCheckNQE = domain.ChangeSetCheckNQE
const ChangeSetTagsAdd = domain.ChangeSetTagsAdd
const ChangeSetTagsRemove = domain.ChangeSetTagsRemove
const ChangeSetTagsRemoveAll = domain.ChangeSetTagsRemoveAll
const CloudDiscoveryTimeout = domain.CloudDiscoveryTimeout
const CollectorTaskQueued = domain.CollectorTaskQueued
const CollectorTaskRunning = domain.CollectorTaskRunning
const ConnectivityDiffAdded = domain.ConnectivityDiffAdded
const ConnectivityDiffDeleted = domain.ConnectivityDiffDeleted
const ConnectivityDiffModified = domain.ConnectivityDiffModified
const CriticalityBusinessCritical = domain.CriticalityBusinessCritical
const CriticalityCriticalInfra = domain.CriticalityCriticalInfra
const CriticalityCrownJewel = domain.CriticalityCrownJewel
const DataConnectorTestTimeout = domain.DataConnectorTestTimeout
const DataFileCSV = domain.DataFileCSV
const DataFileSTIG = domain.DataFileSTIG
const DecorateTimeout = domain.DecorateTimeout
const DecoratorEntityDevice = domain.DecoratorEntityDevice
const DecoratorEntityHost = domain.DecoratorEntityHost
const DecoratorEntityInterface = domain.DecoratorEntityInterface
const DecoratorEntityPath = domain.DecoratorEntityPath
const DecoratorEntityPathDevice = domain.DecoratorEntityPathDevice
const DecoratorEntityPathInterface = domain.DecoratorEntityPathInterface
const DefaultCommitNote = domain.DefaultCommitNote
const DiffRoutingLoop = domain.DiffRoutingLoop
const ErrorKindUnknown = domain.ErrorKindUnknown
const FirewallAddressGroups = domain.FirewallAddressGroups
const FirewallAddressObjects = domain.FirewallAddressObjects
const FirewallApplicationGroups = domain.FirewallApplicationGroups
const FirewallApplicationObjects = domain.FirewallApplicationObjects
const FirewallRulebasePrimary = domain.FirewallRulebasePrimary
const FirewallScopeLocal = domain.FirewallScopeLocal
const FirewallServiceGroups = domain.FirewallServiceGroups
const FirewallServiceObjects = domain.FirewallServiceObjects
const FirewallURLObjects = domain.FirewallURLObjects
const FirewallUserObjects = domain.FirewallUserObjects
const FirewallZones = domain.FirewallZones
const IEntryCheckExists = domain.IEntryCheckExists
const IEntryCheckNotExists = domain.IEntryCheckNotExists
const IEntryTypeIPv4 = domain.IEntryTypeIPv4
const IEntryTypeIPv6 = domain.IEntryTypeIPv6
const InterestingPathsFirewall = domain.InterestingPathsFirewall
const InterestingPathsForwardingDomainBlackhole = domain.InterestingPathsForwardingDomainBlackhole
const InterestingPathsIPMPLSBlackhole = domain.InterestingPathsIPMPLSBlackhole
const InterestingPathsIPMPLSDrop = domain.InterestingPathsIPMPLSDrop
const InterestingPathsL2Storm = domain.InterestingPathsL2Storm
const InterestingPathsLoadBalancer = domain.InterestingPathsLoadBalancer
const InterestingPathsNonStaticL3Loop = domain.InterestingPathsNonStaticL3Loop
const MaxConnectivityTestDevices = domain.MaxConnectivityTestDevices
const MaxPathBulkQueries = domain.MaxPathBulkQueries
const PredictProtocolBGP = domain.PredictProtocolBGP
const PredictProtocolConfig = domain.PredictProtocolConfig
const PredictProtocolEIGRP = domain.PredictProtocolEIGRP
const PredictProtocolISIS = domain.PredictProtocolISIS
const PredictProtocolOMP = domain.PredictProtocolOMP
const PredictProtocolOSPF = domain.PredictProtocolOSPF
const PredictProtocolRIP = domain.PredictProtocolRIP
const PredictProtocolSTP = domain.PredictProtocolSTP
const ProtocolModelingReadTimeout = domain.ProtocolModelingReadTimeout
const RawSNMPDataTimeout = domain.RawSNMPDataTimeout
const RouteOutcomeFilteredOut = domain.RouteOutcomeFilteredOut
const RouteOutcomeInstalled = domain.RouteOutcomeInstalled
const RouteOutcomeNotPreferred = domain.RouteOutcomeNotPreferred
const SNMPVersionV2C = domain.SNMPVersionV2C
const SNMPVersionV3 = domain.SNMPVersionV3
const SaseSetupTypeZscaler = domain.SaseSetupTypeZscaler
const SecurityActionPermit = domain.SecurityActionPermit
const ServiceNowImpactHigh = domain.ServiceNowImpactHigh
const ServiceNowImpactLow = domain.ServiceNowImpactLow
const ServiceNowImpactMedium = domain.ServiceNowImpactMedium
const ServiceNowIncidentLevelHigh = domain.ServiceNowIncidentLevelHigh
const ServiceNowIncidentLevelLow = domain.ServiceNowIncidentLevelLow
const ServiceNowIncidentLevelMedium = domain.ServiceNowIncidentLevelMedium
const ServiceNowMaxAttachmentBytes = domain.ServiceNowMaxAttachmentBytes
const ServiceNowPriorityCritical = domain.ServiceNowPriorityCritical
const ServiceNowPriorityHigh = domain.ServiceNowPriorityHigh
const ServiceNowPriorityLow = domain.ServiceNowPriorityLow
const ServiceNowPriorityModerate = domain.ServiceNowPriorityModerate
const SnapshotMergeNone = domain.SnapshotMergeNone
const SnapshotMergePreserveExisting = domain.SnapshotMergePreserveExisting
const SnapshotMergeReplaceExisting = domain.SnapshotMergeReplaceExisting
const StorageTypeAll = domain.StorageTypeAll
const StorageTypeInternal = domain.StorageTypeInternal
const StorageTypeS3 = domain.StorageTypeS3
const SyntheticAdjacentNetwork = domain.SyntheticAdjacentNetwork
const SyntheticInternet = domain.SyntheticInternet
const SyntheticIntranet = domain.SyntheticIntranet
const SyntheticL2VPN = domain.SyntheticL2VPN
const SyntheticL3VPN = domain.SyntheticL3VPN
const SyntheticPerformanceTimeout = domain.SyntheticPerformanceTimeout
const TenableTestTimeout = domain.TenableTestTimeout
const VerifyPredictModelingTimeout = domain.VerifyPredictModelingTimeout
const VulnerabilityScannerTenableVM = domain.VulnerabilityScannerTenableVM
const VulnerabilityScannerTestTimeout = domain.VulnerabilityScannerTestTimeout
const VulnerabilitySeverityCritical = domain.VulnerabilitySeverityCritical
const VulnerabilitySeverityHigh = domain.VulnerabilitySeverityHigh
const VulnerabilitySeverityLow = domain.VulnerabilitySeverityLow
const VulnerabilitySeverityMedium = domain.VulnerabilitySeverityMedium
const VulnerabilitySeverityNone = domain.VulnerabilitySeverityNone

func NilServiceError(service string) error   { return domain.NilServiceError(service) }
func Ptr[T any](value T) *T                  { return domain.Ptr[T](value) }
func RequireDecoratorName(name string) error { return domain.RequireDecoratorName(name) }
func RequireDeviceRef(networkID string, deviceName string) (string, string, error) {
	return domain.RequireDeviceRef(networkID, deviceName)
}
func RequireNetworkID(networkID string) (string, error) { return domain.RequireNetworkID(networkID) }
func ResolveNetworkID(explicit string, bound string) (string, error) {
	return domain.ResolveNetworkID(explicit, bound)
}
func WaitInterval(ctx context.Context, interval time.Duration) error {
	return domain.WaitInterval(ctx, interval)
}

type AppConnectorSetup = domain.AppConnectorSetup
type AsciiCodedSignedLicenseKey = domain.AsciiCodedSignedLicenseKey
type CapabilitySupport = domain.CapabilitySupport
type CustomOID = domain.CustomOID
type DeviceVRF = domain.DeviceVRF
type DeviceVRFProtocol = domain.DeviceVRFProtocol
type DisabledCommand = domain.DisabledCommand
type HTTPEndpoint = domain.HTTPEndpoint
type IntervalSchedule = domain.IntervalSchedule
type NQEQuerySource = domain.NQEQuerySource
type NQERecord = domain.NQERecord
type RoutingLookupError = domain.RoutingLookupError
type SnapshotRetentionGranularity = domain.SnapshotRetentionGranularity
type TopologyOverrideLink = domain.TopologyOverrideLink
type TracePathGroup = domain.TracePathGroup
type UnavailableError = domain.UnavailableError
type UnsupportedCapabilityError = domain.UnsupportedCapabilityError

var ErrAllowedCommandsUnavailable = domain.ErrAllowedCommandsUnavailable
var ErrCVEIndexNotGzip = domain.ErrCVEIndexNotGzip
var ErrCustomCommandGroupNotFound = domain.ErrCustomCommandGroupNotFound
var ErrDeploymentArtifactIDRequired = domain.ErrDeploymentArtifactIDRequired
var ErrLicenseKeyRequired = domain.ErrLicenseKeyRequired
var ErrLicenseOrgIDRequired = domain.ErrLicenseOrgIDRequired
var ErrNQEEmptySource = domain.ErrNQEEmptySource
var ErrNoAffectedDevices = domain.ErrNoAffectedDevices
var ErrPermanent = domain.ErrPermanent
var ErrTraceEmpty = domain.ErrTraceEmpty

// The access table's types and parsers, for the file-system adapter.
type AccessTable = domain.AccessTable

func ParseAccessTable(source string, read func(name string) (string, error)) (*AccessTable, error) {
	return domain.ParseAccessTable(source, read)
}

func ParseAccessTableJSON(raw []byte, path string) (*AccessTable, error) {
	return domain.ParseAccessTableJSON(raw, path)
}

// Domain names the driving adapters (cmd/fwdctl, forwardtest) name, re-exported so they import the root and ports only.
type CollectionStartOptions = domain.CollectionStartOptions
type NQEPagedOptions = domain.NQEPagedOptions

const ExplainLimit = domain.ExplainLimit
const CheckDiffProcessing = domain.CheckDiffProcessing
const (
	BGPChangeAbsent     = domain.BGPChangeAbsent
	BGPChangeDuplicated = domain.BGPChangeDuplicated
	BGPChangeInjected   = domain.BGPChangeInjected
	BGPChangeModified   = domain.BGPChangeModified
	BGPChangeWithdrawn  = domain.BGPChangeWithdrawn
)

var ErrBGPKeyMatchedNothing = domain.ErrBGPKeyMatchedNothing

// Meraki cloud-managed setups.
type MerakiRegion = domain.MerakiRegion
type NewMerakiSetup = domain.NewMerakiSetup
type MerakiSetup = domain.MerakiSetup
type MerakiSetupPatch = domain.MerakiSetupPatch
type MerakiTestResult = domain.MerakiTestResult
type MerakiDiscoveredHost = domain.MerakiDiscoveredHost
type MerakiDiscovery = domain.MerakiDiscovery
type MerakiConnectivityTest = domain.MerakiConnectivityTest
