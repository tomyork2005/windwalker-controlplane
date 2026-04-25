package service

import "errors"

var (
	ErrTrialAlreadyUsed     = errors.New("trial already used")
	ErrNoTrialPlanAvailable = errors.New("no trial plan available")
)
