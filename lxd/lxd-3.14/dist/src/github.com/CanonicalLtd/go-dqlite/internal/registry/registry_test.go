package registry_test

import (
	"testing"

	"github.com/CanonicalLtd/go-dqlite/internal/bindings"
	"github.com/CanonicalLtd/go-dqlite/internal/logging"
	"github.com/CanonicalLtd/go-dqlite/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Dump returns a string with the contents of the registry.
func TestRegistry_Dump(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	conn1 := newConn()
	conn2 := newConn()
	registry.ConnLeaderAdd("foo.db", conn1)
	registry.ConnFollowerAdd("foo.db", conn2)
	registry.TxnLeaderAdd(conn1, 0)
	dump := registry.Dump()
	assert.Equal(t,
		"leaders:\n-> 1: foo.db\nfollowers:\n-> 2: foo.db\ntransactions:\n-> 0 pending as leader\n",
		dump)
}

func newRegistry(t *testing.T) (*registry.Registry, func()) {
	t.Helper()

	logger := bindings.NewLogger(logging.Test(t))

	vfs, err := bindings.NewVfs("test", logger)
	require.NoError(t, err)

	registry := registry.New(vfs)

	cleanup := func() {
		registry.ConnSerialReset()
		vfs.Close()
		logger.Close()
	}

	return registry, cleanup
}
