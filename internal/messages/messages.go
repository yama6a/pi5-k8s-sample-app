// Package messages holds the messaging contract of all three binaries: exchange and routing key names,
// and the JSON wire types. The names must match the rabbitmq-topology values in offgrid's sample charts.
package messages

// Exchange names, one per RabbitMQ exchange type.
const (
	ExchangeCreateUserCommand = "create-user-command" // direct: one command queue, consumed by the manager
	ExchangeUserEvents        = "user-events"         // topic: each queue binds the keys it wants
	ExchangeUserAuditLogger   = "user-audit-logger"   // fanout: every bound queue gets every message
)

// Routing keys on ExchangeUserEvents. No queue binds RoutingKeyUserDeleted, so the broker drops it,
// which shows topic routing.
const (
	RoutingKeyUserCreated = "users.created"
	RoutingKeyUserDeleted = "users.deleted"
)

// Audit actions carried in AuditLog.Action.
const (
	ActionUserCreated = "user.created"
	ActionUserDeleted = "user.deleted"
)

// CreateUserCommand is published by signup to ExchangeCreateUserCommand and consumed by the manager.
type CreateUserCommand struct {
	UUID      string `json:"uuid"`
	Timestamp string `json:"timestamp"` // RFC3339
}

// UserEvent is published by the manager to ExchangeUserEvents. The routing key says what happened.
type UserEvent struct {
	UUID      string `json:"uuid"`
	Timestamp string `json:"timestamp"` // RFC3339
}

// AuditLog is broadcast by the manager to ExchangeUserAuditLogger on every create and delete.
// Its JSON tags must match the AuditEvent schema in api/openapi.yaml.
type AuditLog struct {
	Service   string `json:"service"` // the emitting workload (WORKLOAD_NAME)
	Action    string `json:"action"`  // ActionUserCreated or ActionUserDeleted
	UUID      string `json:"uuid"`
	Timestamp string `json:"timestamp"` // RFC3339
}

// SubscriptionQueue returns <workload>.<exchange>, the queue name the rabbitmq-topology chart generates.
func SubscriptionQueue(workload, exchange string) string {
	return workload + "." + exchange
}
