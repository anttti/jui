package memstore_test

import (
	"testing"

	"github.com/anttti/j/internal/store"
	"github.com/anttti/j/internal/store/memstore"
	"github.com/anttti/j/internal/store/storetest"
)

func TestMemstoreConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		return memstore.New()
	})
}
