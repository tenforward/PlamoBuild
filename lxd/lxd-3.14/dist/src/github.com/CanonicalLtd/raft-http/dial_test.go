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

package rafthttp_test

import (
	"crypto/tls"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CanonicalLtd/raft-http"
	"github.com/mpvl/subtest"
)

func TestDial(t *testing.T) {
	cases := []struct {
		name  string
		dial  rafthttp.Dial
		start func(*httptest.Server)
	}{
		{"tcp", rafthttp.NewDialTCP(), (*httptest.Server).Start},
		{"tls", rafthttp.NewDialTLS(newTLSConfig()), (*httptest.Server).StartTLS},
	}
	for _, c := range cases {
		subtest.Run(t, c.name, func(t *testing.T) {
			server := httptest.NewUnstartedServer(nil)
			c.start(server)
			defer server.Close()
			addr := server.Listener.Addr().String()

			// Dialing a listening port should return a
			// new network connection.
			conn, err := c.dial(addr, 250*time.Millisecond)
			if err != nil {
				t.Errorf("dial returned error: %v", err)
			}
			if conn == nil {
				t.Errorf("dial returned a nil connection")
			}
		})
	}
}

func TestDial_TimeoutError(t *testing.T) {
	cases := map[string]rafthttp.Dial{
		"tcp": rafthttp.NewDialTCP(),
		"tls": rafthttp.NewDialTLS(&tls.Config{}),
	}
	for name, dial := range cases {
		subtest.Run(t, name, func(t *testing.T) {
			// Dialing a non-listening port should return
			// a timeout error.
			_, err := dial("localhost:0", time.Microsecond)

			if err == nil {
				t.Fatal("dial function did not return an error")
			}
			if err, ok := err.(net.Error); ok {
				if !err.Timeout() {
					t.Errorf("dial returned non-timeout error: %v", err)
				}
			} else {
				t.Errorf("dial returned non-network error: %v", err)
			}
		})
	}
}

// Return a TLS client configuration that does not verify the server
// key, for use in tests.
func newTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true}
}
