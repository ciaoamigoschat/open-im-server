// Copyright © 2023 OpenIM. All rights reserved.
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

package msggateway

import (
	"context"
	"sync"
	"testing"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

type pushTestLongConnServer struct {
	LongConnServer
	clients []*Client
	online  bool
}

func (s *pushTestLongConnServer) GetUserAllCons(string) ([]*Client, bool) {
	return s.clients, s.online
}

type pushTestClientConn struct {
	writes int
}

func (*pushTestClientConn) ReadMessage() ([]byte, error) {
	return nil, nil
}

func (c *pushTestClientConn) WriteMessage([]byte) error {
	c.writes++
	return nil
}

func (*pushTestClientConn) Close() error {
	return nil
}

func TestPushToUserBackgroundPushEligibility(t *testing.T) {
	tests := []struct {
		name            string
		platformID      int
		isBackground    bool
		online          bool
		wantWrites      int
		wantOfflinePush bool
	}{
		{name: "Android foreground online", platformID: constant.AndroidPlatformID, online: true, wantWrites: 1},
		{name: "Android background online", platformID: constant.AndroidPlatformID, isBackground: true, online: true, wantOfflinePush: true},
		{name: "Android offline", platformID: constant.AndroidPlatformID, wantOfflinePush: true},
		{name: "iOS foreground online", platformID: constant.IOSPlatformID, online: true, wantWrites: 1},
		{name: "iOS background online", platformID: constant.IOSPlatformID, isBackground: true, online: true, wantOfflinePush: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &pushTestClientConn{}
			longConnServer := &pushTestLongConnServer{online: tt.online}
			if tt.online {
				longConnServer.clients = []*Client{{
					w:            new(sync.Mutex),
					conn:         conn,
					PlatformID:   tt.platformID,
					IsBackground: tt.isBackground,
					Encoder:      NewJsonEncoder(),
				}}
			}
			server := NewServer(longConnServer, nil, nil)
			result := server.pushToUser(context.Background(), "recipient", &sdkws.MsgData{
				SendID:      "sender",
				RecvID:      "recipient",
				SessionType: constant.SingleChatType,
			})

			if conn.writes != tt.wantWrites {
				t.Fatalf("WebSocket writes = %d, want %d", conn.writes, tt.wantWrites)
			}
			if got := !result.OnlinePush; got != tt.wantOfflinePush {
				t.Fatalf("offline push eligibility = %t, want %t", got, tt.wantOfflinePush)
			}
		})
	}
}
