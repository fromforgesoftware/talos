// Package db holds Talos's Postgres repositories: gorm entities that
// implement the domain interfaces directly and read/write through the kit
// query DSL.
package db

import (
	"context"
	"errors"
	"time"

	apierrors "github.com/fromforgesoftware/go-kit/errors"
	"github.com/fromforgesoftware/go-kit/persistence/gormdb"
	"github.com/fromforgesoftware/go-kit/persistence/postgres"
	"github.com/fromforgesoftware/go-kit/resource"
	"github.com/fromforgesoftware/go-kit/search"
	"github.com/fromforgesoftware/go-kit/slicesx"
	"gorm.io/gorm"

	"github.com/fromforgesoftware/talos/internal/app"
	"github.com/fromforgesoftware/talos/internal/domain"
	"github.com/fromforgesoftware/talos/internal/fields"
)

var auditEventFieldMapping = map[string]string{
	fields.ID:           "id",
	fields.Timestamp:    "timestamp",
	fields.RealmID:      "realm_id",
	fields.ActorID:      "actor_id",
	"actor":             "actor_id",
	fields.ActorType:    "actor_type",
	fields.ResourceType: "resource_type",
	fields.ResourceID:   "resource_id",
	fields.Action:       "action",
	fields.RequestID:    "request_id",
}

type auditEventEntity struct {
	EID          string         `gorm:"column:id;type:uuid;default:uuid_generate_v4();primaryKey"`
	ETimestamp   time.Time      `gorm:"column:timestamp;type:timestamptz;default:now();primaryKey"`
	ERealmID     *string        `gorm:"column:realm_id;type:uuid"`
	EActorID     string         `gorm:"column:actor_id"`
	EActorType   string         `gorm:"column:actor_type"`
	EResourceTyp string         `gorm:"column:resource_type"`
	EResourceID  string         `gorm:"column:resource_id"`
	EAction      string         `gorm:"column:action"`
	ESummary     string         `gorm:"column:summary"`
	EChanges     map[string]any `gorm:"column:changes;type:jsonb;serializer:json"`
	EMetadata    map[string]any `gorm:"column:metadata;type:jsonb;serializer:json"`
	EIP          string         `gorm:"column:ip"`
	ERequestID   string         `gorm:"column:request_id"`
}

func (e *auditEventEntity) TableName() string    { return "talos.audit_event" }
func (e *auditEventEntity) Type() resource.Type  { return domain.ResourceTypeAuditEvent }
func (e *auditEventEntity) ID() string           { return e.EID }
func (e *auditEventEntity) LID() string          { return "" }
func (e *auditEventEntity) CreatedAt() time.Time { return e.ETimestamp }
func (e *auditEventEntity) UpdatedAt() time.Time { return e.ETimestamp }
func (e *auditEventEntity) DeletedAt() *time.Time {
	return nil
}
func (e *auditEventEntity) Timestamp() time.Time     { return e.ETimestamp }
func (e *auditEventEntity) ActorID() string          { return e.EActorID }
func (e *auditEventEntity) ActorType() string        { return e.EActorType }
func (e *auditEventEntity) ResourceType() string     { return e.EResourceTyp }
func (e *auditEventEntity) ResourceID() string       { return e.EResourceID }
func (e *auditEventEntity) Action() string           { return e.EAction }
func (e *auditEventEntity) Summary() string          { return e.ESummary }
func (e *auditEventEntity) Changes() map[string]any  { return e.EChanges }
func (e *auditEventEntity) Metadata() map[string]any { return e.EMetadata }
func (e *auditEventEntity) IP() string               { return e.EIP }
func (e *auditEventEntity) RequestID() string        { return e.ERequestID }

func (e *auditEventEntity) RealmID() string {
	if e.ERealmID == nil {
		return ""
	}
	return *e.ERealmID
}

func auditEventToEntity(ev domain.AuditEvent) *auditEventEntity {
	e := &auditEventEntity{
		EID:          ev.ID(),
		ETimestamp:   ev.Timestamp(),
		EActorID:     ev.ActorID(),
		EActorType:   ev.ActorType(),
		EResourceTyp: ev.ResourceType(),
		EResourceID:  ev.ResourceID(),
		EAction:      ev.Action(),
		ESummary:     ev.Summary(),
		EChanges:     ev.Changes(),
		EMetadata:    ev.Metadata(),
		EIP:          ev.IP(),
		ERequestID:   ev.RequestID(),
	}
	if rid := ev.RealmID(); rid != "" {
		e.ERealmID = &rid
	}
	return e
}

type auditEventRepo struct {
	*postgres.Repo
}

func NewAuditEventRepository(db *gormdb.DBClient) (*auditEventRepo, error) {
	r, err := postgres.NewRepo(db, auditEventFieldMapping)
	if err != nil {
		return nil, err
	}
	return &auditEventRepo{Repo: r}, nil
}

// Create appends one event. id and timestamp fall back to the table defaults
// (uuid_generate_v4 / now()) when the caller left them zero, and the inserted
// row — with its server-assigned values — is returned.
func (r *auditEventRepo) Create(ctx context.Context, ev domain.AuditEvent) (domain.AuditEvent, error) {
	e := auditEventToEntity(ev)
	tx := r.DB.WithContext(ctx)
	if e.EID == "" {
		tx = tx.Omit("id")
	}
	if e.ETimestamp.IsZero() {
		tx = tx.Omit("timestamp")
	}
	if err := tx.Create(e).Error; err != nil {
		return nil, postgres.NewErrUnknown(err)
	}
	return e, nil
}

func (r *auditEventRepo) Get(ctx context.Context, opts ...search.Option) (domain.AuditEvent, error) {
	s := search.New(opts...)
	var e auditEventEntity
	if err := r.QueryApply(ctx, s.Query()).First(&e).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apierrors.NotFound("audit-event", "")
		}
		return nil, postgres.NewErrUnknown(err)
	}
	return &e, nil
}

func (r *auditEventRepo) Query(ctx context.Context, f app.AuditEventFilter) (resource.ListResponse[domain.AuditEvent], error) {
	return r.List(ctx, f.QueryOptions()...)
}

func (r *auditEventRepo) List(ctx context.Context, opts ...search.Option) (resource.ListResponse[domain.AuditEvent], error) {
	q := search.New(opts...).Query()
	var found []*auditEventEntity
	if err := r.QueryApply(ctx, q).Find(&found).Error; err != nil {
		return nil, postgres.NewErrUnknown(err)
	}
	var total int64
	if err := r.CountApply(ctx, new(auditEventEntity), q).Count(&total).Error; err != nil {
		return nil, postgres.NewErrUnknown(err)
	}
	out := slicesx.Map(found, func(e *auditEventEntity) domain.AuditEvent { return e })
	return resource.NewListResponse(out, int(total)), nil
}
