package forward

// Ptr returns a pointer to value.
//
// This SDK uses pointer fields wherever "not stated" and "stated as zero" are
// different requests, which leaves callers needing an addressable copy of a
// literal. Without a helper every such field costs a named local.
func Ptr[T any](value T) *T { return &value }
