package rest

// #include <stdlib.h>
import "C"

// cgoProbe is a cgo file in the adapter: the walk cannot see into it, so it is refused.
func cgoProbe() { _ = C.size_t(0) }
