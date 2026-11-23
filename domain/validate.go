package domain

import (
	"context"
	"errors"
	"strings"
	"time"
)

// RequireNetworkID returns a network ID trimmed of space, or the error a call gets when none is given. It is the one copy of
// that rule: the HTTP adapter's path builders and the use cases' argument checks both reach it.
func RequireNetworkID(networkID string) (string, error) {
	if networkID = strings.TrimSpace(networkID); networkID == "" {
		return "", errors.New("forward: network ID is required")
	}
	return networkID, nil
}

// RequireDeviceRef checks a network ID and a modeled device name, returning both trimmed, before anything addresses that
// device.
func RequireDeviceRef(networkID, deviceName string) (string, string, error) {
	networkID, err := RequireNetworkID(networkID)
	if err != nil {
		return "", "", err
	}
	if deviceName = strings.TrimSpace(deviceName); deviceName == "" {
		return "", "", errors.New("forward: device name is required")
	}
	return networkID, deviceName, nil
}

// RequireDecoratorName refuses an empty decorator name before anything addresses it.
func RequireDecoratorName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("forward: a decorator name is required")
	}
	return nil
}

// WaitInterval waits for interval, or returns ctx's error as soon as ctx is done: the pause between two polls.
func WaitInterval(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// NilServiceError is what a method of a nil service returns where that method is nil-safe, so a zero Client fails with a
// reason instead of a panic. service names the family as a person reads it ("admin", "collector tasks").
func NilServiceError(service string) error {
	return errors.New("forward: " + service + " service is nil")
}
