package domain

import (
	"encoding/json"
)

// ChangeSetCheckResult is a check attached to a change set, with its result
// on one snapshot (Forward's ChangeSetCheckResult: the check's fields
// unwrapped, plus status and execution fields once it has run).
type ChangeSetCheckResult struct {
	ID                  Identifier      `json:"id"`
	Name                string          `json:"name,omitempty"`
	Definition          json.RawMessage `json:"definition,omitempty"`
	DefinedAt           string          `json:"definedAt,omitempty"`
	Status              string          `json:"status,omitempty"`
	ExecutedAt          string          `json:"executedAt,omitempty"`
	ExecutionDurationMS *int64          `json:"executionDurationMillis,omitempty"`
	// NumViolations is nil when Forward sent none (the check did not fail or
	// has not run), not "zero violations".
	NumViolations *int64     `json:"numViolations,omitempty"`
	CreatedByID   Identifier `json:"createdById,omitempty"`
	CreatedAt     string     `json:"createdAt,omitempty"`
	CreatedBy     string     `json:"createdBy,omitempty"`
	UpdatedByID   Identifier `json:"updatedById,omitempty"`
	UpdatedAt     string     `json:"updatedAt,omitempty"`
	UpdatedBy     string     `json:"updatedBy,omitempty"`
	NQEResultKey  string     `json:"nqeResultKey,omitempty"`
}

// CommandValidationOptions places the editor cursor Forward uses for its
// suggestions. Both are 1-based; zero means 1.
type CommandValidationOptions struct {
	CursorLine   int
	CursorColumn int
}

// CommandError.ErrorType values (Forward's changeset.ErrorType): INCOMPLETE is a command that stops before it is complete, INVALID one the
// grammar does not accept, UNSUPPORTED one Forward cannot model (see CommandError.Applied).
const (
	CommandErrorIncomplete  = "INCOMPLETE"
	CommandErrorInvalid     = "INVALID"
	CommandErrorUnsupported = "UNSUPPORTED"
)

// CommandError is one invalid or incomplete command line (Forward's
// changeset.CommandError). Lines and columns are 1-based.
type CommandError struct {
	LineNumber        int             `json:"lineNumber"`
	StartColumnNumber int             `json:"startColumnNumber,omitempty"`
	EndColumnNumber   int             `json:"endColumnNumber,omitempty"`
	ErrorType         string          `json:"errorType"`
	ErrorMsg          string          `json:"errorMsg"`
	ExpectedArgs      json.RawMessage `json:"expectedArgs,omitempty"`
	// Applied is only sent with ErrorType UNSUPPORTED: true means Forward kept the line in the predicted config text even though Predict
	// does not model it. ErrorType is INCOMPLETE, INVALID or UNSUPPORTED.
	Applied *bool `json:"applied,omitempty"`
}

// CommandValidation is Forward's ChangeSetValidationResponse. CommandErrors is
// empty when Forward found nothing wrong -- which is not proof the commands
// are right: the validator only knows the commands in its grammar.
type CommandValidation struct {
	CommandErrors []CommandError    `json:"commandErrors"`
	Suggestions   []json.RawMessage `json:"suggestions,omitempty"`
	TextMarks     []json.RawMessage `json:"textMarks,omitempty"`
}
