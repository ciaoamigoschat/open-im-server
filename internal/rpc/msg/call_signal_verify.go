package msg

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

const (
	anonymousCallRequestDescription = "ciaoamigos.anonymous-call-request.v1"
	voiceCallDescription            = "ciaoamigos.voice-call.v1"
	maxCallPayloadBytes             = 128 * 1024
)

type customCallEnvelope struct {
	Data        string `json:"data"`
	Description string `json:"description"`
	Extension   string `json:"extension"`
}

type voiceCallPayload struct {
	CallID       string          `json:"callId"`
	SentAt       int64           `json:"sentAt"`
	Type         string          `json:"type"`
	CallerName   string          `json:"callerName,omitempty"`
	CallerAvatar string          `json:"callerAvatar,omitempty"`
	SDP          string          `json:"sdp,omitempty"`
	Candidate    *voiceCandidate `json:"candidate,omitempty"`
	Reason       string          `json:"reason,omitempty"`
}

type voiceCandidate struct {
	Candidate     string  `json:"candidate"`
	SDPMLineIndex *int32  `json:"sdpMLineIndex,omitempty"`
	SDPMid        *string `json:"sdpMid,omitempty"`
}

type anonymousCallPayload struct {
	CallID string          `json:"callId"`
	SentAt int64           `json:"sentAt"`
	Type   string          `json:"type"`
	Caller anonymousCaller `json:"caller"`
}

type anonymousCaller struct {
	Gender      string `json:"gender"`
	Age         string `json:"age"`
	Zone        string `json:"zone"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
	Duration    string `json:"duration"`
}

func isAllowedNonFriendCallSignal(message *sdkws.MsgData) bool {
	if message == nil || message.ContentType != constant.Custom || len(message.Content) > maxCallPayloadBytes {
		return false
	}
	var envelope customCallEnvelope
	if !decodeStrictJSON(message.Content, &envelope) || envelope.Extension != "" || len(envelope.Data) > maxCallPayloadBytes {
		return false
	}
	switch envelope.Description {
	case voiceCallDescription:
		return validVoiceCallPayload([]byte(envelope.Data))
	case anonymousCallRequestDescription:
		return validAnonymousCallPayload([]byte(envelope.Data))
	default:
		return false
	}
}

func validVoiceCallPayload(data []byte) bool {
	var payload voiceCallPayload
	if !decodeStrictJSON(data, &payload) || !validCallMetadata(payload.CallID, payload.SentAt) {
		return false
	}
	if len(payload.CallerName) > 120 || len(payload.CallerAvatar) > 2048 || len(payload.Reason) > 240 {
		return false
	}
	switch payload.Type {
	case "invite", "answer":
		return nonBlankWithin(payload.SDP, maxCallPayloadBytes) && payload.Candidate == nil
	case "ice":
		return payload.SDP == "" && validVoiceCandidate(payload.Candidate)
	case "reject", "end", "busy":
		return payload.SDP == "" && payload.Candidate == nil
	default:
		return false
	}
}

func validVoiceCandidate(candidate *voiceCandidate) bool {
	return candidate != nil && nonBlankWithin(candidate.Candidate, 16*1024) &&
		(candidate.SDPMid == nil || len(*candidate.SDPMid) <= 64)
}

func validAnonymousCallPayload(data []byte) bool {
	var payload anonymousCallPayload
	if !decodeStrictJSON(data, &payload) || !validCallMetadata(payload.CallID, payload.SentAt) {
		return false
	}
	switch payload.Type {
	case "request", "accepted", "passed", "cancelled":
	default:
		return false
	}
	caller := payload.Caller
	return nonBlankWithin(caller.Gender, 40) && nonBlankWithin(caller.Age, 20) &&
		nonBlankWithin(caller.Zone, 120) && len(caller.Description) <= 500 &&
		caller.Mode == "VOICE" && validAnonymousDuration(caller.Duration)
}

func validAnonymousDuration(duration string) bool {
	switch duration {
	case "30 minuti", "1 ora", "8 ore", "24 ore":
		return true
	default:
		return false
	}
}

func validCallMetadata(callID string, sentAt int64) bool {
	return nonBlankWithin(callID, 128) && sentAt > 0
}

func nonBlankWithin(value string, max int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= max
}

func decodeStrictJSON(data []byte, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}
