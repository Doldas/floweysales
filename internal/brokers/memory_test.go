package brokers_test

import (
	"testing"

	"floweysales/internal/brokers"
	"floweysales/internal/brokers/brokerstest"
)

func TestMemoryStoreKeepsTheSharedLedgerRules(t *testing.T) {
	brokerstest.Run(t, brokers.NewMemoryStore(), 1, 2, "m-")
}
