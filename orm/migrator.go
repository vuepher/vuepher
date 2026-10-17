package orm

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
)

// Migration is one versioned schema change (DB-05). Migrations are Go code
// rather than raw SQL so they are type-checked and can use the driver-agnostic
// migrator; a migration that really needs SQL can still call Raw.
//
//	type CreatePostsTable struct{}
//
//	func (CreatePostsTable) Up(db *orm.DB) error {
//		return db.Schema().Create(&Post{})
//	}
//
//	func (CreatePostsTable) Down(db *orm.DB) error {
//		return db.Schema().Drop(&Post{})
//	}
//
// `vuepher make:migration create_posts_table` writes this file with the name
// registered.
type Migration interface {
	// Up applies the change.
	Up(*DB) error

	// Down reverses it. A migration that cannot be reversed should return a
	// clear error rather than silently doing nothing.
	Down(*DB) error
}

// MigrationRecord is the bookkeeping row for an applied migration.
type MigrationRecord struct {
	ID        uint64 `gorm:"primaryKey"`
	Migration string `gorm:"uniqueIndex;size:255"`
	Batch     int    `gorm:"index"`
	AppliedAt time.Time
}

// TableName keeps the bookkeeping table out of the way of application tables.
func (MigrationRecord) TableName() string { return "migrations" }

// MigrationStatus is one row of `vuepher migrate:status`.
type MigrationStatus struct {
	Name      string    `json:"name"`
	Applied   bool      `json:"applied"`
	Batch     int       `json:"batch"`
	AppliedAt time.Time `json:"applied_at"`
	Runnable  bool      `json:"runnable"`
}

// MigrationReport summarises a migrate or rollback run.
type MigrationReport struct {
	Applied    []string      `json:"applied"`
	RolledBack []string      `json:"rolled_back"`
	Batch      int           `json:"batch"`
	Duration   time.Duration `json:"duration"`
}

// Migrator runs and rolls back migrations in registration order (DB-05).
type Migrator struct {
	db         *DB
	order      []string
	migrations map[string]Migration
	log        func(string, ...any)
}

// NewMigrator builds a migrator for a connection.
func NewMigrator(db *DB) *Migrator {
	return &Migrator{
		db:         db,
		migrations: map[string]Migration{},
	}
}

// SetLogger installs a progress callback, so the CLI can print each step.
func (m *Migrator) SetLogger(fn func(string, ...any)) *Migrator {
	m.log = fn
	return m
}

// Register adds a migration under its timestamped name. Names are expected to
// sort chronologically, which is what the generator produces
// (2026_10_16_120000_create_posts_table).
func (m *Migrator) Register(name string, migration Migration) *Migrator {
	if _, exists := m.migrations[name]; exists {
		panic(fmt.Sprintf("vuepher/orm: migration %q is registered twice", name))
	}
	m.migrations[name] = migration
	m.order = append(m.order, name)
	sort.Strings(m.order)
	return m
}

// Count reports how many migrations are registered.
func (m *Migrator) Count() int { return len(m.order) }

func (m *Migrator) printf(format string, args ...any) {
	if m.log != nil {
		m.log(format, args...)
	}
}

// ensureTable creates the bookkeeping table when it is missing.
func (m *Migrator) ensureTable() error {
	if m.db == nil || m.db.DB == nil {
		return ErrNoConnection
	}
	return m.db.raw().AutoMigrate(&MigrationRecord{})
}

// applied returns the applied migrations keyed by name.
func (m *Migrator) applied(ctx context.Context) (map[string]MigrationRecord, error) {
	var records []MigrationRecord
	if err := m.db.raw().WithContext(ctx).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("orm: could not read the migrations table: %w", err)
	}
	out := make(map[string]MigrationRecord, len(records))
	for _, record := range records {
		out[record.Migration] = record
	}
	return out, nil
}

// Run applies every pending migration in one batch (DB-05).
func (m *Migrator) Run(ctx context.Context) (*MigrationReport, error) {
	if err := m.ensureTable(); err != nil {
		return nil, err
	}
	applied, err := m.applied(ctx)
	if err != nil {
		return nil, err
	}

	var lastBatch int
	for _, record := range applied {
		if record.Batch > lastBatch {
			lastBatch = record.Batch
		}
	}

	report := &MigrationReport{Batch: lastBatch + 1}
	started := time.Now()

	for _, name := range m.order {
		if _, done := applied[name]; done {
			continue
		}
		migration := m.migrations[name]
		m.printf("migrating  %s", name)
		if err := migration.Up(m.db); err != nil {
			return report, fmt.Errorf("orm: migration %s failed: %w", name, err)
		}
		record := MigrationRecord{Migration: name, Batch: report.Batch, AppliedAt: time.Now().UTC()}
		if err := m.db.raw().WithContext(ctx).Create(&record).Error; err != nil {
			return report, fmt.Errorf("orm: migration %s applied but could not be recorded: %w", name, err)
		}
		report.Applied = append(report.Applied, name)
	}
	report.Duration = time.Since(started)
	return report, nil
}

// Rollback reverses the last `steps` batches. steps below 1 means one batch
// (DB-05).
func (m *Migrator) Rollback(ctx context.Context, steps int) (*MigrationReport, error) {
	if err := m.ensureTable(); err != nil {
		return nil, err
	}
	if steps < 1 {
		steps = 1
	}
	applied, err := m.applied(ctx)
	if err != nil {
		return nil, err
	}
	if len(applied) == 0 {
		return &MigrationReport{}, nil
	}

	// Collect the batches to undo, newest first.
	batches := map[int]bool{}
	var allBatches []int
	for _, record := range applied {
		if !batches[record.Batch] {
			batches[record.Batch] = true
			allBatches = append(allBatches, record.Batch)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(allBatches)))
	if steps > len(allBatches) {
		steps = len(allBatches)
	}
	target := map[int]bool{}
	for i := 0; i < steps; i++ {
		target[allBatches[i]] = true
	}

	report := &MigrationReport{Batch: allBatches[0]}
	started := time.Now()

	// Reverse chronological order within the batch.
	names := make([]string, 0, len(applied))
	for name := range applied {
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	for _, name := range names {
		record := applied[name]
		if !target[record.Batch] {
			continue
		}
		migration, known := m.migrations[name]
		if !known {
			return report, fmt.Errorf(
				"orm: migration %s is recorded as applied but is not registered; "+
					"restore the file or delete its row from the migrations table to continue",
				name,
			)
		}
		m.printf("rolling back %s", name)
		if err := migration.Down(m.db); err != nil {
			return report, fmt.Errorf("orm: rollback of %s failed: %w", name, err)
		}
		if err := m.db.raw().WithContext(ctx).
			Where("migration = ?", name).
			Delete(&MigrationRecord{}).Error; err != nil {
			return report, fmt.Errorf("orm: %s was rolled back but its record could not be removed: %w", name, err)
		}
		report.RolledBack = append(report.RolledBack, name)
	}
	report.Duration = time.Since(started)
	return report, nil
}

// Fresh rolls every batch back and migrates from scratch. Destructive by design;
// the CLI asks for confirmation first (DB-05).
//
// TODO(phase-1): add a `--drop-tables` mode that drops application tables when
// their migration file has gone missing.
func (m *Migrator) Fresh(ctx context.Context) (*MigrationReport, error) {
	if err := m.ensureTable(); err != nil {
		return nil, err
	}
	applied, err := m.applied(ctx)
	if err != nil {
		return nil, err
	}
	if len(applied) > 0 {
		batches := map[int]bool{}
		for _, record := range applied {
			batches[record.Batch] = true
		}
		if _, err := m.Rollback(ctx, len(batches)); err != nil {
			return nil, err
		}
	}
	return m.Run(ctx)
}

// Status reports each migration's state, for `vuepher migrate:status` (DB-05).
func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	if err := m.ensureTable(); err != nil {
		return nil, err
	}
	applied, err := m.applied(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MigrationStatus, 0, len(m.order))
	for _, name := range m.order {
		status := MigrationStatus{Name: name, Runnable: true}
		if record, done := applied[name]; done {
			status.Applied = true
			status.Batch = record.Batch
			status.AppliedAt = record.AppliedAt
		}
		out = append(out, status)
	}
	// Recorded migrations with no file are surfaced so they are not silently
	// forgotten.
	for name, record := range applied {
		if _, known := m.migrations[name]; known {
			continue
		}
		out = append(out, MigrationStatus{
			Name:      name + " (missing file)",
			Applied:   true,
			Batch:     record.Batch,
			AppliedAt: record.AppliedAt,
			Runnable:  false,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Schema helpers
// ---------------------------------------------------------------------------

// Schema is a small, driver-agnostic schema helper for use inside migrations.
// It exists so a migration does not have to reach for *gorm.DB for the common
// cases, while still being able to (DB-10).
type Schema struct {
	db *DB
}

// Schema returns the schema helper for this connection.
func (db *DB) Schema() Schema { return Schema{db: db} }

// Create makes the table for a model, adding any missing columns when the table
// already exists. This mirrors Laravel's Schema::create plus the relaxed
// behaviour people actually rely on during development.
func (s Schema) Create(model any) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.db.raw().AutoMigrate(model)
}

// Drop removes the table for a model.
func (s Schema) Drop(model any) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.db.raw().Migrator().DropTable(model)
}

// Rename renames a table.
func (s Schema) Rename(oldName, newName string) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.db.raw().Migrator().RenameTable(oldName, newName)
}

// HasTable reports whether a table exists.
func (s Schema) HasTable(name string) bool {
	if s.check() != nil {
		return false
	}
	return s.db.raw().Migrator().HasTable(name)
}

// AddColumn adds a column to an existing table.
func (s Schema) AddColumn(model any, field string) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.db.raw().Migrator().AddColumn(model, field)
}

// DropColumn removes a column.
func (s Schema) DropColumn(model any, field string) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.db.raw().Migrator().DropColumn(model, field)
}

// Exec runs raw SQL, the escape hatch for statements no migrator can express.
func (s Schema) Exec(sql string, values ...any) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.db.raw().Exec(sql, values...).Error
}

// Table returns a *gorm.DB scoped to a table name, for raw column manipulation.
func (s Schema) Table(name string) *gorm.DB {
	if s.check() != nil {
		return nil
	}
	return s.db.raw().Table(name)
}

func (s Schema) check() error {
	if s.db == nil || s.db.DB == nil {
		return ErrNoConnection
	}
	return nil
}

// ErrNotReversible is returned by a Down implementation that cannot undo its Up.
var ErrNotReversible = errors.New(
	"orm: this migration cannot be reversed; restore from a backup or write a compensating migration",
)
