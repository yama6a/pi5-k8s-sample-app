// Package messages is the single source of truth for the messaging contract shared by all three
// binaries (manager, signup, auditor): the topology names and the JSON wire structs. Keeping the
// exchange/routing-key names as compile-time constants (rather than env) means they can't be
// misconfigured per-pod — the only per-deployment values are the broker connection + WORKLOAD_NAME.
// These names MUST match what the raspi-cluster rabbitmq-topology values declare (see 11_messaging.md).
package messages

// Exchange names. One of each RabbitMQ exchange type, to showcase the three messaging patterns:
//   - ExchangeCreateUserCommand: direct  — command topic (N publishers : 1 consumer = the manager).
//   - ExchangeUserEvents:        topic   — domain events, routed by key (consumers filter per key).
//   - ExchangeUserAuditLogger:   fanout  — audit broadcast (every bound queue gets every message).
const (
	ExchangeCreateUserCommand = "create-user-command"
	ExchangeUserEvents        = "user-events"
	ExchangeUserAuditLogger   = "user-audit-logger"
)

// Routing keys published to the topic exchange ExchangeUserEvents. A subscriber only receives the
// keys its queue is bound to — e.g. signup binds RoutingKeyUserCreated only, so RoutingKeyUserDeleted
// reaches no one (deliberate: it demonstrates topic routing dropping unrouted keys).
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

// UserEvent is published by the manager to ExchangeUserEvents; the routing key (users.created /
// users.deleted) says what happened, the payload says to whom.
type UserEvent struct {
	UUID      string `json:"uuid"`
	Timestamp string `json:"timestamp"` // RFC3339
}

// AuditLog is broadcast by the manager to ExchangeUserAuditLogger on every create/delete.
type AuditLog struct {
	Service   string `json:"service"` // the emitting workload (WORKLOAD_NAME)
	Action    string `json:"action"`  // ActionUserCreated | ActionUserDeleted
	UUID      string `json:"uuid"`
	Timestamp string `json:"timestamp"` // RFC3339
}

// SubscriptionQueue returns the queue name a workload consumes for an event subscription. It mirrors
// the rabbitmq-topology library's auto-naming (`<user>.<exchange>`, with user == WORKLOAD_NAME), so a
// consumer derives its own queue name instead of it being passed in as env.
func SubscriptionQueue(workload, exchange string) string {
	return workload + "." + exchange
}
