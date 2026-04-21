package sqlitestore_test

import (
	"testing"

	"github.com/anttti/j/internal/store"
	"github.com/anttti/j/internal/store/sqlitestore"
	"github.com/anttti/j/internal/store/storetest"
)

func TestSqliteConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		s, err := sqlitestore.Open(":memory:")
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return s
	})
}
