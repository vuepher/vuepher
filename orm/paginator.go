package orm

import (
	"encoding/json"
	"net/url"
	"strconv"

	"gorm.io/gorm"
)

// Pager is the part of the request the paginator needs. *vuepher.Context
// satisfies it, which is what makes `Paginate(ctx, 15)` work, but any object
// with the same method does too — useful in tests and in jobs that have no HTTP
// request.
type Pager interface {
	// QueryInt reads an integer query parameter with a fallback.
	QueryInt(key string, def int) int
}

// PageParam is the query parameter that selects a page.
const PageParam = "page"

// DefaultPerPage is used when a caller passes a non-positive page size.
const DefaultPerPage = 15

// Paginator is the paginated result shape (DB-08). Its JSON is deliberately
// final: `data`, `meta` and `links` are exactly what the Vue pagination
// component in @vuepher/vue3 consumes, so a paginated prop needs no massaging on
// the frontend.
//
//	{"data": [...],
//	 "meta":  {"current_page":1,"per_page":15,"total":42,"last_page":3,
//	           "from":1,"to":15,"has_more":true},
//	 "links": {"first":"/posts?page=1","prev":"","next":"/posts?page=2",
//	           "last":"/posts?page=3","path":"/posts"}}
type Paginator[T any] struct {
	Data  []T       `json:"data"`
	Meta  PageMeta  `json:"meta"`
	Links PageLinks `json:"links"`
}

// PageMeta describes where the current page sits in the result set.
type PageMeta struct {
	CurrentPage int   `json:"current_page"`
	PerPage     int   `json:"per_page"`
	Total       int64 `json:"total"`
	LastPage    int   `json:"last_page"`

	// From and To are the 1-based row numbers on this page, 0 when empty, so
	// the UI can render "showing 1–15 of 42".
	From int `json:"from"`
	To   int `json:"to"`

	// HasMore is redundant with CurrentPage < LastPage but saves the frontend
	// from having to model the edge cases.
	HasMore bool `json:"has_more"`
}

// PageLinks holds the URLs for navigation. An unavailable direction is an empty
// string rather than null, so the frontend can bind it directly to a link's href
// and check `v-if="links.prev"`.
type PageLinks struct {
	First string `json:"first"`
	Prev  string `json:"prev"`
	Next  string `json:"next"`
	Last  string `json:"last"`
	Path  string `json:"path"`
}

// Paginate runs a count and a page query, returning the rows together with the
// metadata the frontend needs (DB-08).
//
//	posts, err := models.Post{}.Query().Latest().Paginate(ctx, 15)
//
// Existing query parameters (filters, search, sort) are preserved in the
// generated links, so paging does not silently drop a filter.
func (q *Builder[T]) Paginate(pager Pager, perPage int) (*Paginator[T], error) {
	if q.err != nil {
		return nil, q.err
	}
	if perPage <= 0 {
		perPage = DefaultPerPage
	}

	page := 1
	if pager != nil {
		page = pager.QueryInt(PageParam, 1)
	}
	if page < 1 {
		page = 1
	}

	// The count must ignore any limit or offset left on the chain, otherwise it
	// would count only the current page.
	base := q.stmt.Session(&gorm.Session{})
	var total int64
	if err := base.Session(&gorm.Session{}).Limit(-1).Offset(-1).Count(&total).Error; err != nil {
		return nil, q.wrap("Paginate", err)
	}

	lastPage := int((total + int64(perPage) - 1) / int64(perPage))
	if lastPage < 1 {
		lastPage = 1
	}
	// Ask for a page past the end and you get the last page, rather than an
	// empty list with a plausible-looking page number.
	if page > lastPage {
		page = lastPage
	}

	offset := (page - 1) * perPage
	var items []T
	if err := base.Session(&gorm.Session{}).Limit(perPage).Offset(offset).Find(&items).Error; err != nil {
		return nil, q.wrap("Paginate", err)
	}
	if items == nil {
		items = []T{}
	}

	from, to := 0, 0
	if len(items) > 0 {
		from = offset + 1
		to = offset + len(items)
	}

	baseURL := pagerBaseURL(pager)
	links := PageLinks{
		First: pageURL(baseURL, 1),
		Last:  pageURL(baseURL, lastPage),
		Path:  pathOnly(baseURL),
	}
	if page > 1 {
		links.Prev = pageURL(baseURL, page-1)
	}
	if page < lastPage {
		links.Next = pageURL(baseURL, page+1)
	}

	return &Paginator[T]{
		Data: items,
		Meta: PageMeta{
			CurrentPage: page,
			PerPage:     perPage,
			Total:       total,
			LastPage:    lastPage,
			From:        from,
			To:          to,
			HasMore:     page < lastPage,
		},
		Links: links,
	}, nil
}

// SimplePaginate fetches one page without running a count. Cheaper on large
// tables where an exact total is not worth the cost; LastPage and Total are left
// at zero and HasMore is derived from whether a full page came back.
//
// TODO(phase-2): expose SimplePaginate through the frontend paginator component,
// which currently assumes an exact total.
func (q *Builder[T]) SimplePaginate(pager Pager, perPage int) (*Paginator[T], error) {
	if q.err != nil {
		return nil, q.err
	}
	if perPage <= 0 {
		perPage = DefaultPerPage
	}
	page := 1
	if pager != nil {
		page = pager.QueryInt(PageParam, 1)
	}
	if page < 1 {
		page = 1
	}

	// Fetch one extra row to learn whether a next page exists.
	var items []T
	offset := (page - 1) * perPage
	if err := q.stmt.Session(&gorm.Session{}).Limit(perPage + 1).Offset(offset).Find(&items).Error; err != nil {
		return nil, q.wrap("SimplePaginate", err)
	}
	hasMore := len(items) > perPage
	if hasMore {
		items = items[:perPage]
	}
	if items == nil {
		items = []T{}
	}

	baseURL := pagerBaseURL(pager)
	links := PageLinks{First: pageURL(baseURL, 1), Path: pathOnly(baseURL)}
	if page > 1 {
		links.Prev = pageURL(baseURL, page-1)
	}
	if hasMore {
		links.Next = pageURL(baseURL, page+1)
	}

	from, to := 0, 0
	if len(items) > 0 {
		from = offset + 1
		to = offset + len(items)
	}

	return &Paginator[T]{
		Data: items,
		Meta: PageMeta{
			CurrentPage: page,
			PerPage:     perPage,
			From:        from,
			To:          to,
			HasMore:     hasMore,
		},
		Links: links,
	}, nil
}

// Map transforms the data while preserving pagination metadata. Handy for
// turning models into response DTOs just before rendering.
func (p *Paginator[T]) Map(fn func(T) any) *mapResult[T] {
	out := make([]any, 0, len(p.Data))
	for _, item := range p.Data {
		out = append(out, fn(item))
	}
	return &mapResult[T]{Paginator: p, Items: out}
}

// mapResult keeps the metadata of the original paginator alongside mapped items.
// It serialises with the same shape, so the frontend component does not care.
type mapResult[T any] struct {
	*Paginator[T]
	Items []any
}

// MarshalJSON emits data, meta and links with the mapped items.
func (m *mapResult[T]) MarshalJSON() ([]byte, error) {
	return marshalPaginated(m.Items, m.Meta, m.Links)
}

// pagerBaseURL extracts the current URL from the pager so that generated links
// keep the existing filters.
func pagerBaseURL(pager Pager) string {
	type urler interface{ URL() string }
	if u, ok := pager.(urler); ok {
		if base := u.URL(); base != "" {
			return base
		}
	}
	type pather interface{ Path() string }
	if p, ok := pager.(pather); ok {
		if base := p.Path(); base != "" {
			return base
		}
	}
	return "/"
}

// pageURL sets the page parameter on a URL, preserving everything else.
func pageURL(base string, page int) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	query := parsed.Query()
	query.Set(PageParam, strconv.Itoa(page))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// marshalPaginated serialises a paginated payload with arbitrary data, so the
// mapped result keeps the exact shape the frontend component expects.
func marshalPaginated(data any, meta PageMeta, links PageLinks) ([]byte, error) {
	return json.Marshal(struct {
		Data  any       `json:"data"`
		Meta  PageMeta  `json:"meta"`
		Links PageLinks `json:"links"`
	}{Data: data, Meta: meta, Links: links})
}

// pathOnly returns just the path of a URL.
func pathOnly(base string) string {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Path == "" {
		return "/"
	}
	return parsed.Path
}
