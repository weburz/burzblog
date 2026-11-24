package store

import (
	"sync"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"modernc.org/sqlite"
)

// A collator owns scratch buffers. Each concurrent SQLite comparison borrows
// its own, and IDs break ties between equivalent titles during pagination.
var titleCollators = sync.Pool{New: func() any { return collate.New(language.Und, collate.IgnoreCase, collate.Numeric) }}

func init() {
	sqlite.MustRegisterCollationUtf8("burzblog_title", func(a, b string) int {
		c := titleCollators.Get().(*collate.Collator)
		order := c.CompareString(a, b)
		titleCollators.Put(c)
		return order
	})
}
