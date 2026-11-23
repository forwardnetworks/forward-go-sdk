package domain

// ClientPackageType names a Forward client package (ClientPackageType on the
// server). The on-prem appserver serves only WINDOWS_X64, LINUX and
// UPDATES_XML from this route; the headless packages come from fwd.app.
type ClientPackageType string

const (
	ClientPackageManualZip          ClientPackageType = "MANUAL_ZIP"
	ClientPackageWindowsX64         ClientPackageType = "WINDOWS_X64"
	ClientPackageHeadlessWindowsX64 ClientPackageType = "HEADLESS_WINDOWS_X64"
	ClientPackageOSX                ClientPackageType = "OSX"
	ClientPackageLinux              ClientPackageType = "LINUX"
	ClientPackageHeadlessLinux      ClientPackageType = "HEADLESS_LINUX"
	ClientPackageUpdatesXML         ClientPackageType = "UPDATES_XML"
)

// ClientPackage describes a downloaded client package.
type ClientPackage struct {
	// FileName is from Content-Disposition, e.g. "fwd-unix-26.9.0-18.tar.gz"
	// (its version is the server's release). Empty if the server sent none.
	FileName string
	Size     int64
	// SHA256 is the lowercase hex digest of the bytes written.
	SHA256 string
}
