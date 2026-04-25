package service

import "errors"

var (
	ErrTrialAlreadyUsed     = errors.New("trial already used")
	ErrNoTrialPlanForRegion = errors.New("no trial plan for region")
)
