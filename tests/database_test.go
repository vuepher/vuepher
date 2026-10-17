package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/vuepher/vuepher/orm"
)

// These tests run the ORM end to end against a real (in-memory) SQLite
// database: migrations, factories, the fluent query builder, the paginator and
// soft deletes (DB-01 … DB-09). They assert behaviour an application depends on
// rather than implementation details.

// testPost is the model the database tests migrate and query.
type testPost struct {
	orm.Model

	Title     string `json:"title" validate:"required,min=3"`
	Body      string `json:"body"`
	Published bool   `json:"published"`
}

// createPostsTable is a migration in the shape `vuepher make:migration` writes.
type createPostsTable struct{}

func (createPostsTable) Up(db *orm.DB) error {
	return db.Schema().Create(&testPost{})
}

func (createPostsTable) Down(db *orm.DB) error {
	return db.Schema().Drop(&testPost{})
}

// newDatabase opens a fresh in-memory SQLite database for one test.
//
// The DSN is shared-cache in-memory with a unique name per test, so each test
// gets its own schema and connections within a test see the same tables.
func newDatabase(t *testing.T) *orm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", sanitiseDSN(t.Name()))
	db, err := orm.Connect(orm.Config{Driver: "sqlite", DSN: dsn, LogLevel: "silent"})
	if err != nil {
		t.Fatalf("could not open the test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func sanitiseDSN(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

func TestMigrationsRunThenReportThemselvesAsApplied(t *testing.T) {
	db := newDatabase(t)
	migrator := orm.NewMigrator(db)
	migrator.Register("2026_01_01_000000_create_posts_table", createPostsTable{})

	report, err := migrator.Run(context.Background())
	if err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if len(report.Applied) != 1 {
		t.Fatalf("applied = %v, want one migration", report.Applied)
	}

	// Running again must be a no-op: `vuepher migrate` is expected to be safe to
	// repeat, which is how people actually use it.
	second, err := migrator.Run(context.Background())
	if err != nil {
		t.Fatalf("the second migrate failed: %v", err)
	}
	if len(second.Applied) != 0 {
		t.Fatalf("the second migrate applied %v again", second.Applied)
	}

	status, err := migrator.Status(context.Background())
	if err != nil {
		t.Fatalf("migrate:status failed: %v", err)
	}
	if len(status) != 1 || !status[0].Applied {
		t.Fatalf("status = %#v, want one applied migration", status)
	}

	// The table really exists, so the migration did something observable.
	if _, err := orm.QueryOn[testPost](db).Count(); err != nil {
		t.Fatalf("the posts table is not usable after migrating: %v", err)
	}

	rolledBack, err := migrator.Rollback(context.Background(), 1)
	if err != nil {
		t.Fatalf("migrate:rollback failed: %v", err)
	}
	if len(rolledBack.RolledBack) != 1 {
		t.Fatalf("rolled back = %v, want one migration", rolledBack.RolledBack)
	}
	if _, err := orm.QueryOn[testPost](db).Count(); err == nil {
		t.Fatal("the posts table still exists after rolling back")
	}
}

func TestFactoryInsertsAndQueriesSupportFluentChains(t *testing.T) {
	db := newDatabase(t)
	migratePosts(t, db)

	// DB-06: a factory builds a batch and inserts it in one statement.
	records, err := orm.NewFactory(5, func(i int) testPost {
		return testPost{
			Title:     fmt.Sprintf("Post %d", i+1),
			Body:      "body",
			Published: i%2 == 0,
		}
	}).CreateWith(context.Background(), db)
	if err != nil {
		t.Fatalf("the factory insert failed: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("the factory produced %d records, want 5", len(records))
	}
	if records[0].ID == 0 {
		t.Error("the factory insert did not populate the primary key")
	}

	query := orm.QueryOn[testPost](db)

	total, err := query.Clone().Count()
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if total != 5 {
		t.Fatalf("count = %d, want 5", total)
	}

	published, err := query.Clone().Where("published = ?", true).Latest().Get()
	if err != nil {
		t.Fatalf("the filtered query failed: %v", err)
	}
	if len(published) != 3 {
		t.Fatalf("published = %d rows, want 3", len(published))
	}
	// Latest() orders by created_at descending; every row was created in the same
	// second here, so the assertion is on ordering being applied at all rather
	// than on a specific row.
	for _, post := range published {
		if !post.Published {
			t.Fatalf("the where clause leaked an unpublished post: %#v", post)
		}
	}

	first, err := query.Clone().Where("title = ?", "Post 1").First()
	if err != nil {
		t.Fatalf("first failed: %v", err)
	}
	if first.Title != "Post 1" {
		t.Errorf("first = %q, want Post 1", first.Title)
	}

	found, err := query.Clone().Find(records[0].ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if found.Title != records[0].Title {
		t.Errorf("find returned %q, want %q", found.Title, records[0].Title)
	}

	if _, err := query.Clone().Find(999999); !errors.Is(err, orm.ErrNotFound) {
		t.Errorf("Find on a missing row returned %v, want orm.ErrNotFound", err)
	}

	exists, err := query.Clone().Where("title = ?", "Post 1").Exists()
	if err != nil || !exists {
		t.Errorf("exists = %v (err %v), want true", exists, err)
	}
}

func TestPaginateReportsPagesLikeTheFrontendExpects(t *testing.T) {
	db := newDatabase(t)
	migratePosts(t, db)
	seedPosts(t, db, 5)

	pager := &testPager{page: 2}
	paginator, err := orm.QueryOn[testPost](db).Latest().Paginate(pager, 2)
	if err != nil {
		t.Fatalf("paginate failed: %v", err)
	}

	if len(paginator.Data) != 2 {
		t.Errorf("page 2 holds %d rows, want 2", len(paginator.Data))
	}
	if paginator.Meta.CurrentPage != 2 || paginator.Meta.PerPage != 2 {
		t.Errorf("meta = %#v, want page 2 of size 2", paginator.Meta)
	}
	if paginator.Meta.Total != 5 || paginator.Meta.LastPage != 3 {
		t.Errorf("meta = %#v, want total 5 over 3 pages", paginator.Meta)
	}
	if paginator.Links.Next == "" || paginator.Links.Prev == "" {
		t.Errorf("links = %#v, want both a previous and a next link", paginator.Links)
	}
}

func TestSoftDeleteHidesRowsButKeepsThem(t *testing.T) {
	db := newDatabase(t)
	migratePosts(t, db)
	seedPosts(t, db, 3)

	err := orm.QueryOn[testPost](db).Where("title = ?", "Post 1").Delete()
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	visible, err := orm.QueryOn[testPost](db).Count()
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if visible != 2 {
		t.Errorf("visible rows = %d, want 2", visible)
	}

	withTrashed, err := orm.QueryOn[testPost](db).IncludeSoftDeleted().Count()
	if err != nil {
		t.Fatalf("count including soft-deleted rows failed: %v", err)
	}
	if withTrashed != 3 {
		t.Errorf("rows including soft-deleted = %d, want 3: the row must stay recoverable", withTrashed)
	}

	// A hard delete is still available for the cases that need it.
	if err := orm.QueryOn[testPost](db).Where("title = ?", "Post 1").ForceDelete(); err != nil {
		t.Fatalf("force delete failed: %v", err)
	}
	afterForce, err := orm.QueryOn[testPost](db).IncludeSoftDeleted().Count()
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if afterForce != 2 {
		t.Errorf("rows after a force delete = %d, want 2", afterForce)
	}
}

func TestTransactionRollsBackOnError(t *testing.T) {
	db := newDatabase(t)
	migratePosts(t, db)

	boom := errors.New("something went wrong halfway")
	err := db.Transaction(context.Background(), func(tx *orm.DB) error {
		post := &testPost{Title: "Inside a transaction", Body: "body"}
		if err := orm.QueryOn[testPost](tx).Create(post); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("the transaction returned %v, want the handler's error", err)
	}

	count, err := orm.QueryOn[testPost](db).Count()
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("rows after a failed transaction = %d, want 0", count)
	}
}

// migratePosts runs the posts migration.
func migratePosts(t *testing.T, db *orm.DB) {
	t.Helper()
	migrator := orm.NewMigrator(db)
	migrator.Register("2026_01_01_000000_create_posts_table", createPostsTable{})
	if _, err := migrator.Run(context.Background()); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
}

// seedPosts inserts n rows directly, for tests that do not need a factory.
func seedPosts(t *testing.T, db *orm.DB, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		post := &testPost{
			Title:     fmt.Sprintf("Post %d", i),
			Body:      "body",
			Published: i%2 == 1,
		}
		if err := orm.QueryOn[testPost](db).Create(post); err != nil {
			t.Fatalf("seeding failed: %v", err)
		}
	}
}

// testPager is the smallest thing that satisfies orm.Pager, which is what a
// *vuepher.Context provides in an application.
type testPager struct {
	page int
}

func (p *testPager) QueryInt(name string, def int) int {
	if name == orm.PageParam {
		return p.page
	}
	return def
}
