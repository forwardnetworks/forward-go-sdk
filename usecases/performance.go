package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/domain/perfpayload"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"time"
)

func (s *Performance) Upload(
	ctx context.Context,
	networkID string,
	collectorUsername string,
	collectorPassword string,
	payload []byte,
) (*ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, err
	}
	return s.UploadWithIdentity(ctx, networkID, domain.CollectorIdentity{Username: collectorUsername, AuthorizationKey: collectorPassword}, payload)
}

// UploadPayload is UploadPayloadWithIdentity for a collector username and password.
func (s *Performance) UploadPayload(ctx context.Context, networkID, collectorUsername, collectorPassword string, payload perfpayload.Payload) (*ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, err
	}
	return s.UploadPayloadWithIdentity(ctx, networkID, domain.CollectorIdentity{Username: collectorUsername, AuthorizationKey: collectorPassword}, payload)
}

// UploadPayloadWithIdentity validates and encodes a typed payload (package perfpayload) and uploads it with
// UploadWithIdentity. Nothing is sent when the payload is one Forward would drop part of (CPU or memory above 1, a
// device's entries out of time order); the error wraps one of perfpayload's errors. A zero payload.Time is set to now;
// otherwise set it to the moment you send. With a RetryPolicy a retried request re-sends the same bytes, so a retry wait
// (up to its MaxDelay) shifts every stored time by that wait. To skip devices that already have data, filter the payload first with perfpayload.Watermarks.
func (s *Performance) UploadPayloadWithIdentity(ctx context.Context, networkID string, identity domain.CollectorIdentity, payload perfpayload.Payload) (*ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, err
	}
	if payload.Time.IsZero() {
		payload.Time = time.Now()
	}
	body, err := payload.Encode()
	if err != nil {
		return nil, err
	}
	return s.UploadWithIdentity(ctx, networkID, identity, body)
}
