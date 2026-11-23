package domain

import (
	"errors"
)

// ErrPollBudget is returned by Poller.Wait when PollOptions.Budget ran out before the operation finished.
var ErrPollBudget = errors.New("forward: poll budget exhausted before the operation finished")
