package messages

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateUserCommandJSON(t *testing.T) {
	t.Parallel()

	b, err := json.Marshal(CreateUserCommand{UUID: "u-1", Timestamp: "2026-07-10T12:00:00Z"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"uuid":"u-1","timestamp":"2026-07-10T12:00:00Z"}`, string(b))
}

func TestAuditLogJSON(t *testing.T) {
	t.Parallel()

	b, err := json.Marshal(AuditLog{Service: "sample-user-manager", Action: ActionUserCreated, UUID: "u-1", Timestamp: "2026-07-10T12:00:00Z"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"service":"sample-user-manager","action":"user.created","uuid":"u-1","timestamp":"2026-07-10T12:00:00Z"}`, string(b))
}

func TestSubscriptionQueue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "sample-user-signup.user-events", SubscriptionQueue("sample-user-signup", ExchangeUserEvents))
	assert.Equal(t, "sample-audit-logger.user-audit-logger", SubscriptionQueue("sample-audit-logger", ExchangeUserAuditLogger))
}
