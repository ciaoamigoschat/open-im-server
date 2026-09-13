package msg

import (
	"encoding/json"
	"testing"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

func TestIsAllowedNonFriendCallSignal(t *testing.T) {
	tests := []struct {
		name        string
		description string
		payload     any
		want        bool
	}{
		{
			name:        "valid voice invite",
			description: voiceCallDescription,
			payload: map[string]any{
				"callId": "call-1", "sentAt": int64(1), "type": "invite", "sdp": "v=0\\r\\n",
			},
			want: true,
		},
		{
			name:        "valid ice candidate",
			description: voiceCallDescription,
			payload: map[string]any{
				"callId": "call-1", "sentAt": int64(1), "type": "ice",
				"candidate": map[string]any{"candidate": "candidate:1", "sdpMLineIndex": 0},
			},
			want: true,
		},
		{
			name:        "voice invite without sdp",
			description: voiceCallDescription,
			payload:     map[string]any{"callId": "call-1", "sentAt": int64(1), "type": "invite"},
		},
		{
			name:        "unknown field cannot smuggle text",
			description: voiceCallDescription,
			payload: map[string]any{
				"callId": "call-1", "sentAt": int64(1), "type": "end", "message": "hidden text",
			},
		},
		{
			name:        "valid anonymous request",
			description: anonymousCallRequestDescription,
			payload: map[string]any{
				"callId": "call-2", "sentAt": int64(2), "type": "request",
				"caller": map[string]any{
					"gender": "Donna", "age": "32", "zone": "Milano", "description": "Due chiacchiere?",
					"mode": "VOICE", "duration": "30 minuti",
				},
			},
			want: true,
		},
		{
			name:        "unknown custom protocol",
			description: "ciaoamigos.chat-message.v1",
			payload:     map[string]any{"callId": "call-3", "sentAt": int64(3), "type": "end"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := customMessage(t, tt.description, tt.payload)
			if got := isAllowedNonFriendCallSignal(message); got != tt.want {
				t.Fatalf("isAllowedNonFriendCallSignal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAllowedNonFriendCallSignalRejectsNonCustomAndMalformedEnvelope(t *testing.T) {
	if isAllowedNonFriendCallSignal(&sdkws.MsgData{ContentType: constant.Text, Content: []byte(`{}`)}) {
		t.Fatal("text messages must not bypass friend verification")
	}
	if isAllowedNonFriendCallSignal(&sdkws.MsgData{ContentType: constant.Custom, Content: []byte(`{"data":`)}) {
		t.Fatal("malformed custom messages must not bypass friend verification")
	}
}

func customMessage(t *testing.T, description string, payload any) *sdkws.MsgData {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(customCallEnvelope{Data: string(data), Description: description})
	if err != nil {
		t.Fatal(err)
	}
	return &sdkws.MsgData{ContentType: constant.Custom, Content: content}
}
