package chat

import (
	"testing"

	filesmanager "github.com/chhongzh/atri-bot/internal/files"
	"github.com/cloudwego/eino/schema"
)

func TestLegacyInputPartWrapsAudioBase64AsDataURL(t *testing.T) {
	attachment := filesmanager.Attachment{
		Kind:     "audio",
		MIMEType: "audio/ogg",
		Base64:   "aGVsbG8=",
	}

	part := legacyInputPart(attachment)

	if part.Type != schema.ChatMessagePartTypeAudioURL || part.AudioURL == nil {
		t.Fatalf("part type = %+v, want audio URL", part)
	}
	if got := part.AudioURL.URL; got != "data:audio/ogg;base64,aGVsbG8=" {
		t.Fatalf("audio URL = %q, want inline data URL", got)
	}
	if got := part.AudioURL.MIMEType; got != "opus" {
		t.Fatalf("audio format = %q, want opus", got)
	}
}
