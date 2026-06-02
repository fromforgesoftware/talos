// Package fields holds the logical field-name constants used to build
// search queries. Each repository maps these camelCase names to its
// snake_case DB columns in a fieldMapping, so query filters never carry
// raw column strings.
package fields

const (
	ID           = "id"
	Timestamp    = "timestamp"
	RealmID      = "realmId"
	ActorID      = "actorId"
	ActorType    = "actorType"
	ResourceType = "resourceType"
	ResourceID   = "resourceId"
	Action       = "action"
	RequestID    = "requestId"
)
