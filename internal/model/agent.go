package model

import (
	"time"
)

type StartUserSubscribeInput struct {
	UserID        string
	Region        string
	DriverType    string
	SubscribeTime time.Duration

	RequestID string
}

type RemoveUserInput struct {
	UserID     string
	DriverType string

	RequestID string
}
