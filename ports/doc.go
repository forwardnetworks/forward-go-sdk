// Package ports holds the contracts between the SDK's layers: one interface per Forward API family (what a Client field
// sends), ports.Session (the bound network and principal a use case may know of), the values that carry a transport type,
// and the domain values those contracts speak, re-exported so an adapter imports this package and nothing else.
//
// A port may grow in any release: a method may be added to an interface without a major version. A type that implements a
// port to fake it in a test should embed the interface, so a method added later does not break the fake's build.
package ports
