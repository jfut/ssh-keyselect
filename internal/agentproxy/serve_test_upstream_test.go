// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"net"

	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/protocol"
)

// serveTestUpstream provides the small agent protocol surface shared by socket and named-pipe integration tests.
func serveTestUpstream(listener net.Listener, identities []identity.Identity, done chan<- struct{}) {
	defer close(done)
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer func() { _ = conn.Close() }()
			request, err := protocol.ReadFrame(conn)
			if err != nil || len(request) == 0 {
				return
			}
			var response []byte
			switch request[0] {
			case protocol.RequestIdentities:
				response, _ = protocol.MarshalIdentities(identities)
			case protocol.SignRequest:
				response = []byte{protocol.SignResponse, 0, 0, 0, 3, 's', 'i', 'g'}
			default:
				response = []byte{protocol.Failure}
			}
			_ = protocol.WriteFrame(conn, response)
		}()
	}
}
