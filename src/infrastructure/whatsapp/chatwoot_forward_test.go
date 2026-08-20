package whatsapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
)

func TestChatwootMessageTypeFromPayload(t *testing.T) {
	tests := []struct {
		name     string
		isFromMe any
		expected string
	}{
		{
			name:     "incoming message",
			isFromMe: false,
			expected: "incoming",
		},
		{
			name:     "outgoing message",
			isFromMe: true,
			expected: "outgoing",
		},
		{
			name:     "missing field defaults to incoming",
			isFromMe: nil,
			expected: "incoming",
		},
		{
			name:     "non-bool field defaults to incoming",
			isFromMe: "true",
			expected: "incoming",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{}
			if tc.isFromMe != nil {
				payload["is_from_me"] = tc.isFromMe
			}

			if got := chatwootMessageTypeFromPayload(payload); got != tc.expected {
				t.Fatalf("expected %s, got %s", tc.expected, got)
			}
		})
	}
}

func TestBuildChatwootForwardMessageLink(t *testing.T) {
	data := map[string]any{
		"id":         "wa-live-1",
		"chat_id":    "628123456789@s.whatsapp.net",
		"is_from_me": false,
	}

	link := buildChatwootForwardMessageLink(
		"device-a@s.whatsapp.net",
		data,
		chatwoot.MessageOptions{SourceID: "WAID:wa-live-1"},
		&chatwootSyncResult{MessageID: 123, ConversationID: 456, InboxID: 789},
	)

	if link == nil {
		t.Fatal("expected chatwoot message link")
	}
	if link.DeviceID != "device-a@s.whatsapp.net" {
		t.Fatalf("DeviceID = %q", link.DeviceID)
	}
	if link.WhatsAppMessageID != "wa-live-1" || link.SourceID != "WAID:wa-live-1" {
		t.Fatalf("unexpected source mapping: %+v", link)
	}
	if link.ChatwootMessageID != 123 || link.ChatwootConversationID != 456 || link.ChatwootInboxID != 789 {
		t.Fatalf("unexpected chatwoot ids: %+v", link)
	}
	if link.Direction != "incoming" {
		t.Fatalf("Direction = %q, want incoming", link.Direction)
	}
}

func TestExtractReceiptMessageIDs(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want []string
	}{
		{name: "string slice", in: []string{"a", "b"}, want: []string{"a", "b"}},
		{name: "any slice", in: []any{"a", "b", 3}, want: []string{"a", "b"}},
		{name: "single string", in: "a", want: []string{"a"}},
		{name: "empty", in: nil, want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractReceiptMessageIDs(map[string]any{"ids": tc.in})
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d (%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ids = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestDeleteTargetMessageID(t *testing.T) {
	cases := []struct {
		event string
		data  map[string]any
		want  string
	}{
		{"message.revoked", map[string]any{"revoked_message_id": "wa-1"}, "wa-1"},
		{"message.deleted", map[string]any{"deleted_message_id": "wa-2"}, "wa-2"},
		{"message.deleted", map[string]any{"id": "wa-current"}, "wa-current"},
		{"message", map[string]any{"id": "wa-current"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.event, func(t *testing.T) {
			if got := deleteTargetMessageID(tc.event, tc.data); got != tc.want {
				t.Fatalf("deleteTargetMessageID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShouldForwardEventToChatwoot(t *testing.T) {
	// message and message.reaction are always forwarded; edits and deletes are
	// gated by their config toggles. Save/restore the globals so other tests in
	// the package see the defaults.
	origEdits := config.ChatwootForwardEdits
	origDeletes := config.ChatwootForwardDeletes
	origRead := config.ChatwootMessageRead
	origMsgDelete := config.ChatwootMessageDelete
	defer func() {
		config.ChatwootForwardEdits = origEdits
		config.ChatwootForwardDeletes = origDeletes
		config.ChatwootMessageRead = origRead
		config.ChatwootMessageDelete = origMsgDelete
	}()

	t.Run("always-on and unrelated events", func(t *testing.T) {
		config.ChatwootMessageRead = false
		cases := map[string]bool{
			"message":          true,
			"message.reaction": true,
			"message.ack":      false,
			"chat_presence":    false,
		}
		for event, want := range cases {
			if got := shouldForwardEventToChatwoot(event); got != want {
				t.Errorf("shouldForwardEventToChatwoot(%q) = %v, want %v", event, got, want)
			}
		}
	})

	t.Run("read receipts follow toggle", func(t *testing.T) {
		config.ChatwootMessageRead = true
		if !shouldForwardEventToChatwoot("message.ack") {
			t.Fatal("message.ack should forward when ChatwootMessageRead is enabled")
		}
		config.ChatwootMessageRead = false
		if shouldForwardEventToChatwoot("message.ack") {
			t.Fatal("message.ack should not forward when ChatwootMessageRead is disabled")
		}
	})

	t.Run("edits and deletes follow their toggles", func(t *testing.T) {
		config.ChatwootMessageDelete = false
		config.ChatwootForwardEdits = true
		config.ChatwootForwardDeletes = true
		for _, e := range []string{"message.edited", "message.revoked", "message.deleted"} {
			if !shouldForwardEventToChatwoot(e) {
				t.Errorf("%q should forward when toggles enabled", e)
			}
		}

		config.ChatwootForwardEdits = false
		config.ChatwootForwardDeletes = false
		for _, e := range []string{"message.edited", "message.revoked", "message.deleted"} {
			if shouldForwardEventToChatwoot(e) {
				t.Errorf("%q should NOT forward when toggles disabled", e)
			}
		}
	})

	t.Run("deletes forward when only ChatwootMessageDelete is enabled", func(t *testing.T) {
		// The hard-delete feature must reach the forwarder even when tombstone-note
		// forwarding (ChatwootForwardDeletes) is off, so the two are independent.
		// Edits remain gated solely on ChatwootForwardEdits.
		config.ChatwootForwardEdits = false
		config.ChatwootForwardDeletes = false
		config.ChatwootMessageDelete = true
		for _, e := range []string{"message.revoked", "message.deleted"} {
			if !shouldForwardEventToChatwoot(e) {
				t.Errorf("%q should forward when ChatwootMessageDelete is enabled", e)
			}
		}
		if shouldForwardEventToChatwoot("message.edited") {
			t.Error("message.edited should NOT forward on ChatwootMessageDelete alone")
		}
	})
}

func TestIsRetryableChatwootForwardEvent(t *testing.T) {
	// Base messages and their sub-events carry a unique WhatsApp id and are
	// retry-eligible; read receipts are best-effort and must not be queued.
	for _, e := range []string{"message", "message.edited", "message.revoked", "message.deleted", "message.reaction"} {
		if !isRetryableChatwootForwardEvent(e) {
			t.Errorf("%q should be retry-eligible", e)
		}
	}
	for _, e := range []string{"message.ack", "chat_presence", ""} {
		if isRetryableChatwootForwardEvent(e) {
			t.Errorf("%q should NOT be retry-eligible", e)
		}
	}
}

func TestIsEventWhitelistedForChatwoot(t *testing.T) {
	originalEvents := config.WhatsappWebhookEvents
	defer func() { config.WhatsappWebhookEvents = originalEvents }()

	t.Run("empty whitelist allows all", func(t *testing.T) {
		config.WhatsappWebhookEvents = nil
		if !isEventWhitelistedForChatwoot("message.reaction") {
			t.Fatal("expected message.reaction to be allowed when whitelist is empty")
		}
	})

	t.Run("explicit reaction whitelist allowed", func(t *testing.T) {
		config.WhatsappWebhookEvents = []string{"message.reaction"}
		if !isEventWhitelistedForChatwoot("message.reaction") {
			t.Fatal("expected message.reaction to be allowed when explicitly whitelisted")
		}
	})

	t.Run("message whitelist also allows reactions for chatwoot", func(t *testing.T) {
		config.WhatsappWebhookEvents = []string{"message"}
		if !isEventWhitelistedForChatwoot("message.reaction") {
			t.Fatal("expected message.reaction to be allowed for Chatwoot when message is whitelisted")
		}
	})

	t.Run("unrelated whitelist blocks reaction", func(t *testing.T) {
		config.WhatsappWebhookEvents = []string{"message.ack"}
		if isEventWhitelistedForChatwoot("message.reaction") {
			t.Fatal("expected message.reaction to be blocked for Chatwoot when not covered by whitelist")
		}
	})
}

func TestExtractMediaPath(t *testing.T) {
	// extractMediaPath handles the two shapes buildAutoDownloadPayload emits
	// for image/video/document fields. The map branch is the regression-prone
	// one: prior to its introduction, captioned images silently dropped their
	// attachment because the string assertion failed and the loop moved on.
	tests := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "plain string path (audio/sticker shape)",
			in:   "/tmp/whatsapp/audio-123.oga",
			want: "/tmp/whatsapp/audio-123.oga",
		},
		{
			name: "map with path key (captioned image/video/document shape)",
			in:   map[string]any{"path": "/tmp/whatsapp/img-1.jpg", "caption": "look at this"},
			want: "/tmp/whatsapp/img-1.jpg",
		},
		{
			name: "map with only url (auto-download disabled) yields empty",
			in:   map[string]any{"url": "https://wa.example/blob.jpg", "caption": "x"},
			want: "",
		},
		{
			name: "nil yields empty",
			in:   nil,
			want: "",
		},
		{
			name: "unrelated type yields empty",
			in:   42,
			want: "",
		},
		{
			name: "empty string returns empty (caller treats as no-op)",
			in:   "",
			want: "",
		},
		{
			name: "map with non-string path yields empty",
			in:   map[string]any{"path": 12345},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractMediaPath(tc.in); got != tc.want {
				t.Fatalf("extractMediaPath(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestChatwootIdentifierForJID(t *testing.T) {
	// chatwootIdentifierForJID guards the boundary into the Chatwoot REST
	// client. The client picks its identifier-vs-phone search path by
	// checking HasSuffix(identifier, "@lid"); stripping that suffix here
	// would silently downgrade @lid contacts to phone-normalized garbage.
	// Each case below pins a specific JID server / suffix so a regression
	// (e.g. someone reintroducing ExtractPhoneFromJID at the call site)
	// surfaces as a focused test failure rather than a downstream search miss.
	tests := []struct {
		name string
		jid  string
		want string
	}{
		{
			name: "ordinary phone JID is stripped to digits",
			jid:  "628123456789@s.whatsapp.net",
			want: "628123456789",
		},
		{
			name: "lid JID is preserved end-to-end so chatwoot client takes identifier path",
			jid:  "1234567890abcd@lid",
			want: "1234567890abcd@lid",
		},
		{
			name: "raw digits without @ pass through untouched",
			jid:  "628123456789",
			want: "628123456789",
		},
		{
			name: "empty string yields empty",
			jid:  "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatwootIdentifierForJID(tc.jid); got != tc.want {
				t.Fatalf("chatwootIdentifierForJID(%q) = %q, want %q", tc.jid, got, tc.want)
			}
		})
	}
}

func TestBuildReactionChatwootContent(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]any
		fromName string
		preview  string
		expected string
	}{
		{
			name: "reaction quotes resolved message preview",
			payload: map[string]any{
				"reaction":           "👍",
				"reacted_message_id": "wamid-123",
			},
			fromName: "Alice",
			preview:  "bom dia, tudo certo?",
			expected: "Alice reacted 👍 to “bom dia, tudo certo?”",
		},
		{
			name: "reaction removal quotes resolved message preview",
			payload: map[string]any{
				"reaction":           "",
				"reacted_message_id": "wamid-123",
			},
			fromName: "Alice",
			preview:  "bom dia, tudo certo?",
			expected: "Alice removed a reaction from “bom dia, tudo certo?”",
		},
		{
			name: "reaction with sender name and target id",
			payload: map[string]any{
				"reaction":           "👍",
				"reacted_message_id": "wamid-123",
			},
			fromName: "Alice",
			expected: "Alice reacted 👍 to message wamid-123",
		},
		{
			name: "reaction falls back to phone",
			payload: map[string]any{
				"reaction":           "🔥",
				"reacted_message_id": "wamid-456",
				"from":               "628123456789@s.whatsapp.net",
			},
			fromName: "",
			expected: "628123456789 reacted 🔥 to message wamid-456",
		},
		{
			name: "reaction removal",
			payload: map[string]any{
				"reaction":           "",
				"reacted_message_id": "wamid-789",
			},
			fromName: "Bob",
			expected: "Bob removed a reaction from message wamid-789",
		},
		{
			name: "reaction falls back to sender jid when pushname missing",
			payload: map[string]any{
				"reaction":           "😂",
				"reacted_message_id": "wamid-999",
				"from":               "628777000111@s.whatsapp.net",
			},
			fromName: "",
			expected: "628777000111 reacted 😂 to message wamid-999",
		},
		{
			name: "missing target id still produces readable text",
			payload: map[string]any{
				"reaction": "❤️",
			},
			fromName: "Carol",
			expected: "Carol reacted ❤️",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildReactionChatwootContent(tc.payload, tc.fromName, tc.preview); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

type reactedPreviewTestRepo struct {
	chatstorage.IChatStorageRepository
	byDevice map[string]*chatstorage.Message
	byID     map[string]*chatstorage.Message
}

func (r *reactedPreviewTestRepo) GetMessageByIDAndDevice(deviceID, id string) (*chatstorage.Message, error) {
	return r.byDevice[deviceID+"|"+id], nil
}

func (r *reactedPreviewTestRepo) GetMessageByID(id string) (*chatstorage.Message, error) {
	return r.byID[id], nil
}

func TestReactedMessagePreview(t *testing.T) {
	repo := &reactedPreviewTestRepo{
		byDevice: map[string]*chatstorage.Message{
			"device-1|wamid-text":  {Content: "  olá,\ncomo vai?  "},
			"device-1|wamid-image": {MediaType: "image"},
			"device-1|wamid-doc":   {MediaType: "document", Filename: "contrato.pdf"},
			"device-1|wamid-long":  {Content: strings.Repeat("a", 100)},
		},
		byID: map[string]*chatstorage.Message{
			"wamid-global": {Content: "mensagem global"},
		},
	}

	tests := []struct {
		name      string
		deviceID  string
		messageID string
		expected  string
	}{
		{
			name:      "text message collapses whitespace",
			deviceID:  "device-1",
			messageID: "wamid-text",
			expected:  "olá, como vai?",
		},
		{
			name:      "media without caption falls back to media type",
			deviceID:  "device-1",
			messageID: "wamid-image",
			expected:  "(image)",
		},
		{
			name:      "document includes filename",
			deviceID:  "device-1",
			messageID: "wamid-doc",
			expected:  "(document: contrato.pdf)",
		},
		{
			name:      "long content is truncated with ellipsis",
			deviceID:  "device-1",
			messageID: "wamid-long",
			expected:  strings.Repeat("a", 80) + "…",
		},
		{
			name:      "empty device id falls back to global lookup",
			deviceID:  "",
			messageID: "wamid-global",
			expected:  "mensagem global",
		},
		{
			name:      "unknown message yields empty preview",
			deviceID:  "device-1",
			messageID: "wamid-missing",
			expected:  "",
		},
		{
			name:      "empty message id yields empty preview",
			deviceID:  "device-1",
			messageID: "",
			expected:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := reactedMessagePreview(tc.deviceID, repo, tc.messageID); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}

	t.Run("nil repo yields empty preview", func(t *testing.T) {
		if got := reactedMessagePreview("device-1", nil, "wamid-text"); got != "" {
			t.Fatalf("expected empty preview, got %q", got)
		}
	})
}

type replyAttrsTestRepo struct {
	chatstorage.IChatStorageRepository
	links map[string]*chatstorage.ChatwootMessageLink
}

func (r *replyAttrsTestRepo) GetChatwootMessageLinkByWhatsAppID(deviceID, waMessageID string) (*chatstorage.ChatwootMessageLink, error) {
	return r.links[deviceID+"|"+waMessageID], nil
}

func TestBuildChatwootReplyAttributes(t *testing.T) {
	repo := &replyAttrsTestRepo{
		links: map[string]*chatstorage.ChatwootMessageLink{
			"device-1|wamid-linked":    {ChatwootMessageID: 77},
			"device-1|wamid-zero-link": {ChatwootMessageID: 0},
		},
	}

	t.Run("linked reply sets native in_reply_to", func(t *testing.T) {
		attrs, linked := buildChatwootReplyAttributes("device-1", repo, "wamid-linked")
		if !linked {
			t.Fatal("expected linked=true")
		}
		if attrs["in_reply_to"] != 77 {
			t.Fatalf("expected in_reply_to=77, got %v", attrs["in_reply_to"])
		}
		if attrs["in_reply_to_external_id"] != "WAID:wamid-linked" {
			t.Fatalf("unexpected external id %v", attrs["in_reply_to_external_id"])
		}
	})

	t.Run("unlinked reply keeps only external id", func(t *testing.T) {
		attrs, linked := buildChatwootReplyAttributes("device-1", repo, "wamid-unknown")
		if linked {
			t.Fatal("expected linked=false")
		}
		if _, ok := attrs["in_reply_to"]; ok {
			t.Fatal("in_reply_to should not be set without a link")
		}
		if attrs["in_reply_to_external_id"] != "WAID:wamid-unknown" {
			t.Fatalf("unexpected external id %v", attrs["in_reply_to_external_id"])
		}
	})

	t.Run("link with zero chatwoot id is not linked", func(t *testing.T) {
		if _, linked := buildChatwootReplyAttributes("device-1", repo, "wamid-zero-link"); linked {
			t.Fatal("expected linked=false for zero chatwoot message id")
		}
	})

	t.Run("nil repo keeps only external id", func(t *testing.T) {
		attrs, linked := buildChatwootReplyAttributes("device-1", nil, "wamid-linked")
		if linked {
			t.Fatal("expected linked=false with nil repo")
		}
		if attrs["in_reply_to_external_id"] != "WAID:wamid-linked" {
			t.Fatalf("unexpected external id %v", attrs["in_reply_to_external_id"])
		}
	})

	t.Run("empty device id keeps only external id", func(t *testing.T) {
		if _, linked := buildChatwootReplyAttributes("", repo, "wamid-linked"); linked {
			t.Fatal("expected linked=false with empty device id")
		}
	})
}

func TestOneLineExcerpt(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "collapses newlines and spaces", in: "  olá,\n  como   vai? ", max: 80, want: "olá, como vai?"},
		{name: "truncates with ellipsis", in: strings.Repeat("b", 90), max: 80, want: strings.Repeat("b", 80) + "…"},
		{name: "short string unchanged", in: "oi", max: 80, want: "oi"},
		{name: "empty string", in: "", max: 80, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := oneLineExcerpt(tt.in, tt.max); got != tt.want {
				t.Fatalf("oneLineExcerpt(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSyncPayloadToChatwoot_ReactionEchoStampsSourceID is the regression test
// for the echo race: an outgoing reaction note must carry the reaction's own
// WAID as source_id so isEchoOfForwardedMessage (chatwoot.go) can drop the
// webhook Chatwoot fires back for it, independent of the timing-sensitive
// IsMessageSentByUs cache. Without the fix, source_id is empty on this path
// and the synthetic "X reacted 👍 to ..." note gets sent back to WhatsApp.
func TestSyncPayloadToChatwoot_ReactionEchoStampsSourceID(t *testing.T) {
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/search":
			// Echo the query term back as the contact's phone number so the
			// client's own normalization always matches, whatever it computed.
			q := r.URL.Query().Get("q")
			writeTestJSON(t, w, http.StatusOK, map[string]any{
				"payload": []map[string]any{
					{"id": 7, "name": "Alice", "phone_number": q},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/7/conversations":
			writeTestJSON(t, w, http.StatusOK, map[string]any{
				"payload": []map[string]any{
					{"id": 42, "inbox_id": 1, "status": "open"},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations/42/messages":
			if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
				t.Fatalf("decode message-create body: %v", err)
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"id": 999})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	cw := &chatwoot.Client{
		BaseURL:    server.URL,
		APIToken:   "t",
		AccountID:  1,
		InboxID:    1,
		HTTPClient: server.Client(),
	}
	origGetClient := getChatwootClientFn
	getChatwootClientFn = func() *chatwoot.Client { return cw }
	defer func() { getChatwootClientFn = origGetClient }()

	payload := map[string]any{"payload": map[string]any{
		"id":                 "REACTION-ID-1",
		"reacted_message_id": "ORIGINAL-ID-1",
		"reaction":           "👍",
		"from":               "628123456789@s.whatsapp.net",
		"chat_id":            "628123456789@s.whatsapp.net",
		"from_name":          "Alice",
		"is_from_me":         true,
	}}

	if err := syncPayloadToChatwoot(context.Background(), payload, "message.reaction", "", nil); err != nil {
		t.Fatalf("syncPayloadToChatwoot: %v", err)
	}

	if capturedBody == nil {
		t.Fatal("message-create endpoint was never called")
	}

	sourceID, _ := capturedBody["source_id"].(string)
	if sourceID != "WAID:REACTION-ID-1" {
		t.Errorf("source_id = %q, want %q", sourceID, "WAID:REACTION-ID-1")
	}
	if sourceID == "WAID:ORIGINAL-ID-1" {
		t.Fatal("source_id must be stamped from the reaction's own id, not reacted_message_id")
	}

	messageType, _ := capturedBody["message_type"].(string)
	if messageType != "outgoing" {
		t.Errorf("message_type = %q, want outgoing", messageType)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode json response: %v", err)
	}
}
