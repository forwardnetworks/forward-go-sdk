package domain

// NQEQueryAssist is an AI-generated NQE query and its validation state.
type NQEQueryAssist struct {
	ID         Identifier `json:"id"`
	Query      string     `json:"query"`
	ValidQuery bool       `json:"validQuery"`
}

// NQESummaryAssist is an AI-generated summary of an NQE query.
type NQESummaryAssist struct {
	ID      Identifier `json:"id"`
	Summary string     `json:"summary"`
}

// NQEDocMessage is prior user or assistant context for a documentation assist.
type NQEDocMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NQEDocReference identifies documentation used in an answer.
type NQEDocReference struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// NQEDocAssist is an AI-generated answer grounded in NQE documentation.
type NQEDocAssist struct {
	ID         Identifier        `json:"id"`
	Answer     string            `json:"answer"`
	References []NQEDocReference `json:"references"`
}

// NQEDocAssistRequest asks an NQE documentation question.
type NQEDocAssistRequest struct {
	Question         string          `json:"question"`
	PreviousMessages []NQEDocMessage `json:"previousMessages,omitempty"`
}

// PredictOverviewAssist contains generated change-set metadata.
type PredictOverviewAssist struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}
