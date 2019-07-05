package transaction_test

import (
	"fmt"
	"testing"

	"github.com/CanonicalLtd/go-dqlite/internal/bindings"
	"github.com/CanonicalLtd/go-dqlite/internal/logging"
	"github.com/CanonicalLtd/go-dqlite/internal/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTxn_String(t *testing.T) {
	txn, cleanup := newFollowerTxn(t, 0)
	defer cleanup()
	assert.False(t, txn.IsLeader())
	assert.Equal(t, "0 pending as follower", txn.String())

	txn, cleanup = newLeaderTxn(t, 1)
	defer cleanup()
	assert.True(t, txn.IsLeader())
	assert.Equal(t, "1 pending as leader", txn.String())

	txn.Zombie()
	assert.Equal(t, "1 pending as leader (zombie)", txn.String())
}

// Marking a transaction as leader twice results in a panic.
func TestTxn_LeaderTwice(t *testing.T) {
	txn, cleanup := newLeaderTxn(t, 1)
	defer cleanup()

	assert.PanicsWithValue(t, "transaction is already marked as leader", txn.Leader)
}

// Putting a transaction in dry-run mode twice results in a panic.
func TestTxn_DryRunTwice(t *testing.T) {
	txn, cleanup := newFollowerTxn(t, 1)
	defer cleanup()

	txn.DryRun()

	assert.PanicsWithValue(t, "transaction is already in dry-run mode", txn.DryRun)
}

// The transaction connection is exposed by the Conn() method.
func TestTxn_Conn(t *testing.T) {
	txn, cleanup := newFollowerTxn(t, 123)
	defer cleanup()

	assert.NotNil(t, txn.Conn())
}

// The transaction ID is exposed by the ID() method.
func TestTxn_ID(t *testing.T) {
	txn, cleanup := newFollowerTxn(t, 123)
	defer cleanup()

	assert.Equal(t, uint64(123), txn.ID())
}

// The transaction state is exposed by the Sate() method.
func TestTxn_State(t *testing.T) {
	txn, cleanup := newFollowerTxn(t, 123)
	defer cleanup()

	assert.Equal(t, transaction.Pending, txn.State())
}

/*
// A follower transaction replicates the given commit frames command.
func TestTxn_Frames_Follower_Commit(t *testing.T) {
	txn, cleanup := newFollowerTxn(t, 123)
	defer cleanup()

	err := txn.Frames(true, newCreateTable())
	require.NoError(t, err)

	assert.Equal(t, transaction.Written, txn.State())
}

// A follower transaction replicates the given non-commit frames command.
func TestTxn_Frames_Follower_Non_Commit(t *testing.T) {
	txn, cleanup := newFollowerTxn(t, 0)
	defer cleanup()

	err := txn.Frames(true, newCreateTable())
	require.NoError(t, err)

	// Create a new txn with the same connection, since the one above is done.
	txn = transaction.New(txn.Conn(), 1)

	err = txn.Frames(true, newInsertN())
	require.NoError(t, err)

	assert.Equal(t, transaction.Writing, txn.State())
}

// A follower transaction executes an undo command.
func TestTxn_Undo_Follower_Non_Commit(t *testing.T) {
	txn, cleanup := newFollowerTxn(t, 0)
	defer cleanup()

	err := txn.Frames(true, newCreateTable())
	require.NoError(t, err)

	// Create a new txn with the same connection, since the one above is done.
	txn = transaction.New(txn.Conn(), 1)

	err = txn.Frames(true, newInsertN())
	require.NoError(t, err)

	err = txn.Undo()
	require.NoError(t, err)

	assert.Equal(t, transaction.Undone, txn.State())
}
*/

// Mark a leader transaction as zombie.
func TestTxn_Zombie(t *testing.T) {
	txn, cleanup := newLeaderTxn(t, 0)
	defer cleanup()

	txn.Zombie()

	assert.True(t, txn.IsZombie())
}

// Marking a follower transaction as zombie results in a panic.
func TestTxn_Zombie_Follower(t *testing.T) {
	txn, cleanup := newFollowerTxn(t, 0)
	defer cleanup()

	assert.PanicsWithValue(t, "follower transactions can't be marked as zombie", txn.Zombie)
}

// Marking a transaction as zombie twice results in a panic.
func TestTxn_Zombie_Twice(t *testing.T) {
	txn, cleanup := newLeaderTxn(t, 0)
	defer cleanup()

	txn.Zombie()

	assert.PanicsWithValue(t, "transaction is already marked as zombie", txn.Zombie)
}

// Calling IsZombie() on a follower connection results in a panic.
func TestTxn_IsZombie_Follower(t *testing.T) {
	txn, cleanup := newFollowerTxn(t, 0)
	defer cleanup()

	assert.PanicsWithValue(t, "follower transactions can't be zombie", func() { txn.IsZombie() })
}

/*
// Resurrect a zombie transaction.
func TestTxn_Resurrect(t *testing.T) {
	// First apply a CREATE TABLE frames command to both a leader and a
	// follower transaction. This is needed in order for the recover of the
	// INSERT frames command below to work.
	leader, cleanup := newLeaderTxn(t, 0)
	defer cleanup()

	follower, cleanup := newFollowerTxn(t, 0)
	defer cleanup()

	err := leader.Frames(true, newCreateTable())
	require.NoError(t, err)

	err = follower.Frames(true, newCreateTable())
	require.NoError(t, err)

	// Then apply an INSERT frames command to the leader connection, using
	// a new transaction ID.
	leader = transaction.New(leader.Conn(), 1)
	leader.Leader()

	err = leader.Frames(true, newInsertN())
	require.NoError(t, err)

	// Now mark the transaction as zombie and resurrect it using the
	// follower connection.
	leader.Zombie()

	follower, err = leader.Resurrect(follower.Conn())
	require.NoError(t, err)
	assert.Equal(t, leader.ID(), follower.ID())
	assert.Equal(t, transaction.Writing, follower.State())
}

// Resurrect panics if some pre-conditions are not met.
func TestTxn_Resurrect_Panic(t *testing.T) {
	leader := transaction.New(&sqlite3.SQLiteConn{}, 1)

	f := func() { leader.Resurrect(&sqlite3.SQLiteConn{}) }
	assert.PanicsWithValue(t, "attempt to resurrect follower transaction", f)

	leader.Leader()
	assert.PanicsWithValue(t, "attempt to resurrect non-zombie transaction", f)

	leader.Zombie()
	leader.Undo()
	assert.PanicsWithValue(t, "attempt to resurrect a transaction not in pending or writing state", f)
}

// Performing an invalid state transition results in an error
func TestTxn_InvalidTransition(t *testing.T) {
	txn, cleanup := newFollowerTxn(t, 1)
	defer cleanup()

	err := txn.Frames(true, newCreateTable())
	require.NoError(t, err)

	f := func() { txn.Undo() }
	assert.PanicsWithValue(t, "invalid written -> undone transition", f)
}
*/

func newFollowerTxn(t *testing.T, id uint64) (*transaction.Txn, func()) {
	t.Helper()

	vfs, name := newVfs(t)

	conn, err := bindings.Open("test.db", name)
	require.NoError(t, err)

	err = conn.Exec("PRAGMA synchronous=off; PRAGMA journal_mode=wal")
	require.NoError(t, err)

	err = conn.WalReplicationFollower()
	require.NoError(t, err)

	txn := transaction.New(conn, id)

	cleanup := func() {
		require.NoError(t, vfs.Close())
	}

	return txn, cleanup
}

func newLeaderTxn(t *testing.T, id uint64) (*transaction.Txn, func()) {
	t.Helper()

	vfs, name := newVfs(t)

	conn, err := bindings.Open("test.db", name)
	require.NoError(t, err)

	err = conn.Exec("PRAGMA synchronous=off; PRAGMA journal_mode=wal")
	require.NoError(t, err)

	methods := &testWalReplicationMethods{}
	replication, err := bindings.NewWalReplication("test", methods)
	require.NoError(t, err)

	err = conn.WalReplicationLeader("test")
	require.NoError(t, err)

	txn := transaction.New(conn, id)
	txn.Leader()

	cleanup := func() {
		require.NoError(t, replication.Close())
		require.NoError(t, vfs.Close())
	}

	return txn, cleanup
}

// Create a new in-memory VFS.
func newVfs(t *testing.T) (*bindings.Vfs, string) {
	t.Helper()

	name := fmt.Sprintf("test-%d", serial)

	logger := bindings.NewLogger(logging.Test(t))

	vfs, err := bindings.NewVfs(name, logger)
	require.NoError(t, err)

	serial++

	return vfs, name
}

var serial int

type testWalReplicationMethods struct {
}

func (r *testWalReplicationMethods) Begin(*bindings.Conn) int {
	return 0
}

func (r *testWalReplicationMethods) Abort(*bindings.Conn) int {
	return 0
}

func (r *testWalReplicationMethods) Frames(*bindings.Conn, bindings.WalReplicationFrameList) int {
	return 0
}

func (r *testWalReplicationMethods) Undo(*bindings.Conn) int {
	return 0
}

func (r *testWalReplicationMethods) End(*bindings.Conn) int {
	return 0
}
