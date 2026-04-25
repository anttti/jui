package storetest_test

import (
	"testing"

	"github.com/anttti/j/internal/store"
	"github.com/anttti/j/internal/store/memstore"
	"github.com/anttti/j/internal/store/storetest"
)

// TestRun_DriveSuiteAgainstMemstore exercises every helper inside the
// storetest package itself. The suite is already used by memstore and
// sqlitestore via their own *_test.go files, but those measure coverage
// on a per-package basis so this package would otherwise show 0%.
func TestRun_DriveSuiteAgainstMemstore(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		return memstore.New()
	})
}
