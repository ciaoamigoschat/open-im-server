package msg

import (
	"testing"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

func TestModerationText(t *testing.T) {
	msg := &sdkws.MsgData{ContentType: constant.Text, Content: []byte(`{"content":"testo da controllare"}`)}
	if got := moderationText(msg); got != "testo da controllare" {
		t.Fatalf("moderationText = %q", got)
	}
	msg.ContentType = constant.Picture
	if got := moderationText(msg); got != "" {
		t.Fatalf("picture must not be treated as text: %q", got)
	}
}
