package domain

// CheckDiffProcessing is the DiffType of a row Forward has not finished computing (NQE check diffs only; GET
// /api/diffs/{a}/{b}/checks?type=NQE answers at once with PROCESSING rows).
const CheckDiffProcessing = "PROCESSING"
