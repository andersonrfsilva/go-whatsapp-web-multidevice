package rest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/gofiber/fiber/v3"
)

// withSignConfig sets the Chatwoot signature globals for the duration of a
// single subtest and restores their prior values on cleanup, so sibling tests
// always observe the package defaults (sign disabled, "\n\n" delimiter).
func withSignConfig(t *testing.T, signMsg bool, delimiter string) {
	t.Helper()
	prevSign := config.ChatwootSignMsg
	prevDelim := config.ChatwootSignDelimiter
	config.ChatwootSignMsg = signMsg
	config.ChatwootSignDelimiter = delimiter
	t.Cleanup(func() {
		config.ChatwootSignMsg = prevSign
		config.ChatwootSignDelimiter = prevDelim
	})
}

func withChatwootWebhookSecret(t *testing.T, secret string) {
	t.Helper()
	prevSecret := config.ChatwootWebhookSecret
	config.ChatwootWebhookSecret = secret
	t.Cleanup(func() {
		config.ChatwootWebhookSecret = prevSecret
	})
}

type chatwootRouteTestRepo struct {
	domainChatStorage.IChatStorageRepository
	link  *domainChatStorage.ChatwootMessageLink
	count int
}

func (r *chatwootRouteTestRepo) CountChatwootDeviceConfigs() (int, error) {
	return r.count, nil
}

// GetLatestChatwootMessageLinkByConversation mirrors the real repo's matching
// rules (account scope, legacy-zero wildcard, config scope) so a handler that
// passed the wrong scope would get nil here instead of silently succeeding.
func (r *chatwootRouteTestRepo) GetLatestChatwootMessageLinkByConversation(conversationID, accountID int, allowLegacyZero bool, configID int64) (*domainChatStorage.ChatwootMessageLink, error) {
	if r.link == nil || r.link.ChatwootConversationID != conversationID {
		return nil, nil
	}
	if r.link.ChatwootAccountID != accountID && !(allowLegacyZero && r.link.ChatwootAccountID == 0) {
		return nil, nil
	}
	if configID != 0 && r.link.ChatwootConfigID != configID {
		return nil, nil
	}
	cloned := *r.link
	return &cloned, nil
}

func TestComposeOutgoingText(t *testing.T) {
	tests := []struct {
		name string
		// signMsg/delimiter drive the CHATWOOT_SIGN_* config globals for the case.
		signMsg   bool
		delimiter string
		content   string
		sender    string
		want      string
	}{
		{
			// Core reason this function exists: GFM/Chatwoot markdown is
			// rewritten to WhatsApp's syntax before delivery — bold (**bold**->
			// *bold*), italic (*it*->_it_), and strike (~~s~~->~s~) all translate.
			name:      "markdown translated chatwoot to whatsapp",
			signMsg:   false,
			delimiter: "\n\n",
			content:   "**bold** *it* ~~s~~",
			sender:    "",
			want:      "*bold* _it_ ~s~",
		},
		{
			// Empty content has no body, so there is nothing to send.
			name:      "empty content returns empty",
			signMsg:   false,
			delimiter: "\n\n",
			content:   "",
			sender:    "",
			want:      "",
		},
		{
			// Sign disabled: never attach a signature, even when the agent name
			// is present — the body must pass through (markdown-translated) only.
			name:      "sign disabled ignores sender name",
			signMsg:   false,
			delimiter: "\n\n",
			content:   "hello",
			sender:    "Agent Smith",
			want:      "hello",
		},
		{
			// Sign enabled with a name: prefix "*<name>*" + delimiter + body.
			// Default delimiter "\n\n" carries literal backslash-n escapes that
			// must be expanded to real newlines.
			name:      "sign enabled default delimiter expands escapes",
			signMsg:   true,
			delimiter: "\n\n",
			content:   "hello",
			sender:    "Agent Smith",
			want:      "*Agent Smith*\n\nhello",
		},
		{
			// Same path but the delimiter is the literal two-character escape
			// sequence backslash-n-backslash-n; composeOutgoingText must expand
			// each "\n" into a real newline rune in the output.
			name:      "sign enabled literal backslash-n delimiter expanded",
			signMsg:   true,
			delimiter: "\\n\\n",
			content:   "hello",
			sender:    "Agent Smith",
			want:      "*Agent Smith*\n\nhello",
		},
		{
			// Signing still translates the body markdown before prefixing the
			// signature, so both transforms compose in the signed path.
			name:      "sign enabled also translates body markdown",
			signMsg:   true,
			delimiter: " | ",
			content:   "**bold**",
			sender:    "Bob",
			want:      "*Bob* | *bold*",
		},
		{
			// Empty sender name => no signature; the body alone is returned.
			name:      "sign enabled empty sender name no signature",
			signMsg:   true,
			delimiter: "\n\n",
			content:   "hello",
			sender:    "",
			want:      "hello",
		},
		{
			// Whitespace-only sender name is trimmed to empty => no signature.
			name:      "sign enabled whitespace sender name no signature",
			signMsg:   true,
			delimiter: "\n\n",
			content:   "hello",
			sender:    "   \t  ",
			want:      "hello",
		},
		{
			// Sign enabled but no body: the guard text != "" prevents emitting a
			// lone signature with no message underneath.
			name:      "sign enabled empty content no lone signature",
			signMsg:   true,
			delimiter: "\n\n",
			content:   "",
			sender:    "Agent Smith",
			want:      "",
		},
		{
			// A surrounding sender name is trimmed before being wrapped, so the
			// signature uses the cleaned name, not the padded original.
			name:      "sign enabled sender name trimmed before wrapping",
			signMsg:   true,
			delimiter: "\n\n",
			content:   "hello",
			sender:    "  Agent Smith  ",
			want:      "*Agent Smith*\n\nhello",
		},
		{
			// Plain text with no markdown delimiters is passed through unchanged
			// (the markdown translator short-circuits when no * ~ are present).
			name:      "plain text no markdown unchanged",
			signMsg:   false,
			delimiter: "\n\n",
			content:   "just plain text",
			sender:    "",
			want:      "just plain text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSignConfig(t, tt.signMsg, tt.delimiter)

			payload := chatwoot.WebhookPayload{
				Content: tt.content,
				Sender:  chatwoot.Contact{Name: tt.sender},
			}

			got := composeOutgoingText(payload)
			if got != tt.want {
				t.Errorf("composeOutgoingText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestComposeOutgoingTextDefaultsRestored asserts the config globals are left at
// their package defaults after the table-driven cases run, guarding against a
// save/restore leak that would make unrelated package tests flaky.
func TestComposeOutgoingTextDefaultsRestored(t *testing.T) {
	if config.ChatwootSignMsg != false {
		t.Errorf("ChatwootSignMsg leaked: got %v, want false", config.ChatwootSignMsg)
	}
	if config.ChatwootSignDelimiter != "\n\n" {
		t.Errorf("ChatwootSignDelimiter leaked: got %q, want %q", config.ChatwootSignDelimiter, "\n\n")
	}
}

func TestIsEchoOfForwardedMessage(t *testing.T) {
	tests := []struct {
		name     string
		sourceID string
		want     bool
	}{
		{"forwarded message", "WAID:3EB0ABC123", true},
		{"agent reply, no source_id", "", false},
		{"unrelated source_id", "12345", false},
		{"prefix only as substring elsewhere", "X-WAID:3EB0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := chatwoot.WebhookPayload{SourceID: tt.sourceID}
			if got := isEchoOfForwardedMessage(payload); got != tt.want {
				t.Errorf("isEchoOfForwardedMessage(source_id=%q) = %v, want %v", tt.sourceID, got, tt.want)
			}
		})
	}
}

func TestChatwootSendFailureContent(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "with error",
			err:  errors.New("device offline"),
			want: "Message was not sent to WhatsApp.\n\nError: device offline",
		},
		{
			name: "nil error",
			err:  nil,
			want: "Message was not sent to WhatsApp.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chatwootSendFailureContent(tt.err); got != tt.want {
				t.Fatalf("chatwootSendFailureContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChatwootPayloadDeleted(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want bool
	}{
		{name: "bool true", in: map[string]any{"deleted": true}, want: true},
		{name: "bool false", in: map[string]any{"deleted": false}, want: false},
		{name: "string true", in: map[string]any{"deleted": "true"}, want: true},
		{name: "missing", in: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := chatwoot.WebhookPayload{ContentAttributes: tt.in}
			if got := chatwootPayloadDeleted(payload); got != tt.want {
				t.Fatalf("chatwootPayloadDeleted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChatwootWebhookRejectsInvalidSecret(t *testing.T) {
	withChatwootWebhookSecret(t, "expected-secret")

	app := fiber.New()
	handler := &ChatwootHandler{}
	app.Post("/chatwoot/webhook", handler.HandleWebhook)

	req := httptest.NewRequest(http.MethodPost, "/chatwoot/webhook", strings.NewReader(`{"event":"message_created"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Chatwoot-Webhook-Secret", "wrong-secret")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestResolveChatwootWebhookRoutePrefersConversationLink(t *testing.T) {
	handler := &ChatwootHandler{
		ChatStorageRepo: &chatwootRouteTestRepo{
			link: &domainChatStorage.ChatwootMessageLink{
				DeviceID:               "device-b@s.whatsapp.net",
				WhatsAppMessageID:      "wa-1",
				WhatsAppChatJID:        "628222222222@s.whatsapp.net",
				ChatwootConversationID: 202,
			},
		},
	}

	route := handler.resolveChatwootWebhookRoute(chatwoot.WebhookPayload{
		Conversation: chatwoot.ConversationWebhook{
			ID: 202,
			Meta: chatwoot.ConversationMeta{
				Sender: chatwoot.Contact{
					PhoneNumber: "+628999999999",
					CustomAttributes: map[string]any{
						"gowa_whatsapp_jid": "628999999999@s.whatsapp.net",
					},
				},
			},
		},
	}, nil)

	if route.DeviceID != "device-b@s.whatsapp.net" {
		t.Fatalf("DeviceID = %q, want device-b@s.whatsapp.net", route.DeviceID)
	}
	if route.Destination != "628222222222@s.whatsapp.net" {
		t.Fatalf("Destination = %q, want linked WhatsApp chat", route.Destination)
	}
}

func TestResolveSendDestination(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantDest    string
		wantIsGroup bool
	}{
		// Plain @s.whatsapp.net JIDs are reduced to the bare phone number.
		{name: "private jid stripped to phone", in: "628123456789@s.whatsapp.net", wantDest: "628123456789", wantIsGroup: false},
		// A "+"-prefixed phone is cleaned and passes through with no @ suffix.
		{name: "plus phone cleaned", in: "+628123456789", wantDest: "628123456789", wantIsGroup: false},
		// A bare phone number is unchanged.
		{name: "bare phone unchanged", in: "628123456789", wantDest: "628123456789", wantIsGroup: false},
		// Groups keep the full @g.us JID and report isGroup=true.
		{name: "group jid preserved", in: "120363000000@g.us", wantDest: "120363000000@g.us", wantIsGroup: true},
		// Regression: privacy-masked @lid JIDs must be preserved verbatim so the
		// send layer's LID resolution can run. Stripping the suffix here misroutes
		// the reply into @s.whatsapp.net and breaks delivery to @lid contacts.
		{name: "lid jid preserved verbatim", in: "1234567890123@lid", wantDest: "1234567890123@lid", wantIsGroup: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDest, gotIsGroup := resolveSendDestination(tt.in)
			if gotDest != tt.wantDest || gotIsGroup != tt.wantIsGroup {
				t.Fatalf("resolveSendDestination(%q) = (%q, %v), want (%q, %v)", tt.in, gotDest, gotIsGroup, tt.wantDest, tt.wantIsGroup)
			}
		})
	}
}

func TestChatwootLinkChatJID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "phone gets whatsapp suffix", in: "+628123456789", want: "628123456789@s.whatsapp.net"},
		{name: "private jid stays jid", in: "628123456789@s.whatsapp.net", want: "628123456789@s.whatsapp.net"},
		{name: "group jid stays jid", in: "120363@g.us", want: "120363@g.us"},
		{name: "lid jid stays jid", in: "abc@lid", want: "abc@lid"},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chatwootLinkChatJID(tt.in); got != tt.want {
				t.Fatalf("chatwootLinkChatJID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestChatwootInReplyToID(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]any
		want  int
	}{
		{name: "json number decodes as float64", attrs: map[string]any{"in_reply_to": float64(123)}, want: 123},
		{name: "int is accepted", attrs: map[string]any{"in_reply_to": 456}, want: 456},
		{name: "numeric string is accepted", attrs: map[string]any{"in_reply_to": " 789 "}, want: 789},
		{name: "non-numeric string yields zero", attrs: map[string]any{"in_reply_to": "abc"}, want: 0},
		{name: "missing key yields zero", attrs: map[string]any{}, want: 0},
		{name: "nil attrs yields zero", attrs: nil, want: 0},
		{name: "nil value yields zero", attrs: map[string]any{"in_reply_to": nil}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chatwootInReplyToID(tt.attrs); got != tt.want {
				t.Fatalf("chatwootInReplyToID(%v) = %d, want %d", tt.attrs, got, tt.want)
			}
		})
	}
}

type chatwootReplyLinkTestRepo struct {
	domainChatStorage.IChatStorageRepository
	links map[int]*domainChatStorage.ChatwootMessageLink
	err   error
}

func (r *chatwootReplyLinkTestRepo) GetChatwootMessageLinkByChatwootID(deviceID string, chatwootMessageID int) (*domainChatStorage.ChatwootMessageLink, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.links[chatwootMessageID], nil
}

func TestResolveReplyMessageID(t *testing.T) {
	repo := &chatwootReplyLinkTestRepo{
		links: map[int]*domainChatStorage.ChatwootMessageLink{
			42: {WhatsAppMessageID: "3EB0AAAA"},
			43: {WhatsAppMessageID: ""},
		},
	}
	handler := &ChatwootHandler{ChatStorageRepo: repo}

	payloadWithReply := func(v any) chatwoot.WebhookPayload {
		return chatwoot.WebhookPayload{ContentAttributes: map[string]any{"in_reply_to": v}}
	}

	t.Run("linked quote resolves to whatsapp message id", func(t *testing.T) {
		got := handler.resolveReplyMessageID("device-1", payloadWithReply(float64(42)))
		if got == nil || *got != "3EB0AAAA" {
			t.Fatalf("expected 3EB0AAAA, got %v", got)
		}
	})

	t.Run("unknown quote yields nil", func(t *testing.T) {
		if got := handler.resolveReplyMessageID("device-1", payloadWithReply(float64(99))); got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
	})

	t.Run("link without whatsapp id yields nil", func(t *testing.T) {
		if got := handler.resolveReplyMessageID("device-1", payloadWithReply(float64(43))); got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
	})

	t.Run("no quote yields nil", func(t *testing.T) {
		if got := handler.resolveReplyMessageID("device-1", chatwoot.WebhookPayload{}); got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
	})

	t.Run("empty device id yields nil", func(t *testing.T) {
		if got := handler.resolveReplyMessageID("", payloadWithReply(float64(42))); got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
	})

	t.Run("repo error yields nil", func(t *testing.T) {
		errHandler := &ChatwootHandler{ChatStorageRepo: &chatwootReplyLinkTestRepo{err: errors.New("boom")}}
		if got := errHandler.resolveReplyMessageID("device-1", payloadWithReply(float64(42))); got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
	})

	t.Run("nil repo yields nil", func(t *testing.T) {
		nilHandler := &ChatwootHandler{}
		if got := nilHandler.resolveReplyMessageID("device-1", payloadWithReply(float64(42))); got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
	})
}

// withResolveQuotedSourceIDFn overrides the resolveQuotedSourceIDFn seam for
// the duration of a subtest and restores the original on cleanup.
func withResolveQuotedSourceIDFn(t *testing.T, fn func(conversationID, messageID int) (string, error)) {
	t.Helper()
	prev := resolveQuotedSourceIDFn
	resolveQuotedSourceIDFn = fn
	t.Cleanup(func() {
		resolveQuotedSourceIDFn = prev
	})
}

func TestResolveReplyMessageIDFallsBackToSourceID(t *testing.T) {
	payloadWithReply := func(v any) chatwoot.WebhookPayload {
		return chatwoot.WebhookPayload{
			ContentAttributes: map[string]any{"in_reply_to": v},
			Conversation:      chatwoot.ConversationWebhook{ID: 202},
		}
	}

	t.Run("no local link falls back to source_id WAID prefix", func(t *testing.T) {
		withResolveQuotedSourceIDFn(t, func(conversationID, messageID int) (string, error) {
			if conversationID != 202 || messageID != 42 {
				t.Fatalf("unexpected args conversationID=%d messageID=%d", conversationID, messageID)
			}
			return "WAID:ABC123", nil
		})

		// No ChatStorageRepo => the local-link path is skipped entirely, so
		// resolution must come from the fallback.
		handler := &ChatwootHandler{}
		got := handler.resolveReplyMessageID("device-1", payloadWithReply(float64(42)))
		if got == nil || *got != "ABC123" {
			t.Fatalf("expected ABC123, got %v", got)
		}
	})

	t.Run("local link miss falls back to source_id", func(t *testing.T) {
		withResolveQuotedSourceIDFn(t, func(conversationID, messageID int) (string, error) {
			return "WAID:XYZ999", nil
		})

		repo := &chatwootReplyLinkTestRepo{links: map[int]*domainChatStorage.ChatwootMessageLink{}}
		handler := &ChatwootHandler{ChatStorageRepo: repo}
		got := handler.resolveReplyMessageID("device-1", payloadWithReply(float64(42)))
		if got == nil || *got != "XYZ999" {
			t.Fatalf("expected XYZ999, got %v", got)
		}
	})

	t.Run("local link hit takes priority over fallback", func(t *testing.T) {
		fallbackCalled := false
		withResolveQuotedSourceIDFn(t, func(conversationID, messageID int) (string, error) {
			fallbackCalled = true
			return "WAID:SHOULD_NOT_BE_USED", nil
		})

		repo := &chatwootReplyLinkTestRepo{links: map[int]*domainChatStorage.ChatwootMessageLink{
			42: {WhatsAppMessageID: "3EB0AAAA"},
		}}
		handler := &ChatwootHandler{ChatStorageRepo: repo}
		got := handler.resolveReplyMessageID("device-1", payloadWithReply(float64(42)))
		if got == nil || *got != "3EB0AAAA" {
			t.Fatalf("expected 3EB0AAAA, got %v", got)
		}
		if fallbackCalled {
			t.Fatalf("fallback should not be called when a local link is found")
		}
	})

	t.Run("source_id without WAID prefix yields nil", func(t *testing.T) {
		withResolveQuotedSourceIDFn(t, func(conversationID, messageID int) (string, error) {
			return "12345", nil
		})
		handler := &ChatwootHandler{}
		if got := handler.resolveReplyMessageID("device-1", payloadWithReply(float64(42))); got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
	})

	t.Run("empty source_id yields nil", func(t *testing.T) {
		withResolveQuotedSourceIDFn(t, func(conversationID, messageID int) (string, error) {
			return "", nil
		})
		handler := &ChatwootHandler{}
		if got := handler.resolveReplyMessageID("device-1", payloadWithReply(float64(42))); got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
	})

	t.Run("bare WAID prefix with empty id yields nil", func(t *testing.T) {
		withResolveQuotedSourceIDFn(t, func(conversationID, messageID int) (string, error) {
			return "WAID:", nil
		})
		handler := &ChatwootHandler{}
		if got := handler.resolveReplyMessageID("device-1", payloadWithReply(float64(42))); got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
	})

	t.Run("fallback error yields nil", func(t *testing.T) {
		withResolveQuotedSourceIDFn(t, func(conversationID, messageID int) (string, error) {
			return "", errors.New("boom")
		})
		handler := &ChatwootHandler{}
		if got := handler.resolveReplyMessageID("device-1", payloadWithReply(float64(42))); got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
	})
}
