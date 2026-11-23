package domain

import (
	"fmt"
	"time"
)

// SnapshotExportOptions selects what Snapshots.Export writes. All zero exports
// the whole snapshot as it is.
//
//   - IncludeDevices or ExcludeDevices (device names or globs, never both)
//     export a subset. Forward also pulls in the devices related to an included
//     one.
//   - ObfuscationKey turns on obfuscation of sensitive data in the files (see
//     Forward's "Obfuscate a Snapshot"). Forward derives the replacements from
//     the key, and deobfuscating needs it, so keep it (I did not trace how
//     Forward's deobfuscation takes it). ObfuscateNames additionally replaces device
//     and other names, and requires a key. The key is a secret: this type's
//     String and GoString redact it, and no error from this package includes it.
//   - Only "CONFIG" restricts the ZIP to configuration files.
//   - Timeout bounds the whole call, including a long obfuscation before the
//     first byte and the transfer. Zero keeps the client's own timeout (60
//     seconds by default, which a large export exceeds); a positive value
//     replaces it; a negative value removes it, leaving the context as the only
//     limit.
type SnapshotExportOptions struct {
	IncludeDevices []string
	ExcludeDevices []string
	ObfuscationKey string
	ObfuscateNames bool
	Only           string
	Timeout        time.Duration
}

// String redacts the obfuscation key so an options value is safe to log.
func (o SnapshotExportOptions) String() string {
	key := ""
	if o.ObfuscationKey != "" {
		key = "<redacted>"
	}
	return fmt.Sprintf("SnapshotExportOptions{include=%d exclude=%d obfuscationKey=%q obfuscateNames=%t only=%q timeout=%s}",
		len(o.IncludeDevices), len(o.ExcludeDevices), key, o.ObfuscateNames, o.Only, o.Timeout)
}

// GoString redacts the key for %#v as well.
func (o SnapshotExportOptions) GoString() string { return o.String() }
