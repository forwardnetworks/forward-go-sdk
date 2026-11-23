package usecases

// ---- generic tree helpers ------------------------------------------------------------------------------------------------------

// ---- the serializer: sorted keys, indent 2, JSON-required escapes only -----------------------------------------------------------

// ---- assemble ------------------------------------------------------------------------------------------------------------------

func lessKey(x, y []string) bool {
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return len(x) < len(y)
}
