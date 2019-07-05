// Copyright 2017 Canonical Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package registry_test

import (
	"testing"

	"github.com/CanonicalLtd/go-dqlite/internal/bindings"
	"github.com/stretchr/testify/assert"
)

// Add a new leader connection to the registry.
func TestRegistry_ConnLeaderAdd(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	conn := newConn()
	registry.ConnLeaderAdd("test.db", conn)
	assert.Equal(t, []*bindings.Conn{conn}, registry.ConnLeaders("test.db"))
	assert.Equal(t, "test.db", registry.ConnLeaderFilename(conn))
}

// Adding the same connection twice results in panic.
func TestRegistry_ConnLeaderAddTwice(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	conn := newConn()
	registry.ConnLeaderAdd("foo.db", conn)
	f := func() { registry.ConnLeaderAdd("bar.db", conn) }
	assert.PanicsWithValue(t, "connection is already registered with serial 1", f)
}

// Delete a leader from to the registry.
func TestRegistry_ConnLeaderDel(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	conn := newConn()
	registry.ConnLeaderAdd("test.db", conn)
	registry.ConnLeaderDel(conn)
	assert.Equal(t, []*bindings.Conn{}, registry.ConnLeaders("test.db"))
}

// Delete a non registered leader causes a panic.
func TestRegistry_ConnLeaderDelNotRegistered(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	conn := newConn()
	f := func() { registry.ConnLeaderDel(conn) }
	assert.PanicsWithValue(t, "connection is not registered", f)
}

// ConnLeaderFilename panics if passed a non-registered registry.
func TestRegistry_ConnLeaderFilenameNonRegisteredConn(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	conn := newConn()
	f := func() { registry.ConnLeaderFilename(conn) }
	assert.PanicsWithValue(t, "no database for the given connection", f)
}

// Add a new follower connection to the registry.
func TestRegistry_ConnFollowerAdd(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	conn := newConn()
	registry.ConnFollowerAdd("test.db", conn)
	assert.Equal(t, []string{"test.db"}, registry.ConnFollowerFilenames())
}

// ConnFollowerAdd panics if the given filename already has a registered follower
// registry.
func TestRegistry_ConnFollowerAddAlreadyRegistered(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	conn := newConn()
	registry.ConnFollowerAdd("test.db", conn)
	f := func() { registry.ConnFollowerAdd("test.db", conn) }
	assert.PanicsWithValue(t, "connection is already registered with serial 1", f)
}

// Remove a follower connection from the registry.
func TestRegistry_ConnFollowerDel(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	conn := newConn()
	registry.ConnFollowerAdd("test.db", conn)
	registry.ConnFollowerDel("test.db")
	assert.Equal(t, []string{}, registry.ConnFollowerFilenames())
}

// ConnFollowerDel panics if the given filename is not associated with a registered
// follower registry.
func TestRegistry_ConnFollowerDelAlreadyRegistered(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	f := func() { registry.ConnFollowerDel("test.db") }
	assert.PanicsWithValue(t, "follower connection for 'test.db' is not registered", f)
}

// ConnFollower panics if no follower with the given filename is registered.
func TestRegistry_ConnFollowerNotRegistered(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	f := func() { registry.ConnFollower("test.db") }
	assert.PanicsWithValue(t, "no follower connection for 'test.db'", f)
}

// Serial tracks added/removed connections with unique identifiers.
func TestRegistry_Serial(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	conn1 := newConn()
	conn2 := newConn()

	defer conn1.Close()
	defer conn2.Close()

	registry.ConnLeaderAdd("test.db", conn1)
	registry.ConnFollowerAdd("test.db", conn2)
	assert.Equal(t, uint64(1), registry.ConnSerial(conn1))
	assert.Equal(t, uint64(2), registry.ConnSerial(conn2))
}

// Serial panics if the given connection is not registered.
func TestRegistry_SerialNotRegistered(t *testing.T) {
	registry, cleanup := newRegistry(t)
	defer cleanup()

	f := func() { registry.ConnSerial(newConn()) }
	assert.PanicsWithValue(t, "connection is not registered", f)
}

// Create a new sqlite connection against a memory database.
func newConn() *bindings.Conn {
	conn, err := bindings.Open("test.db", "test")
	if err != nil {
		panic(err)
	}

	err = conn.Exec("PRAGMA synchronous=OFF; PRAGMA journal_mode=wal")
	if err != nil {
		panic(err)
	}

	err = conn.WalReplicationFollower()
	if err != nil {
		panic(err)
	}

	return conn
}
