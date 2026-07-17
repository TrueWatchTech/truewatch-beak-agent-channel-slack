package beakslack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	platform "github.com/TrueWatchTech/truewatch-beak-agent-channel-slack/internal/slack"
	"github.com/TrueWatchTech/truewatch-beak-agent-channel-slack/sdk"
	"github.com/TrueWatchTech/truewatch-beak-agent-channel-slack/state"
)

const (
	ID       = "beak-agent-slack"
	Platform = "slack"
)

var ErrCredentialLogin = errors.New("slack connector uses credential login; create channel account from CredentialSchema")

type Connector struct{}

func NewConnector() sdk.Connector {
	return Connector{}
}

var _ sdk.Connector = Connector{}

// EventResult is returned by the inbound event handler.
type EventResult struct {
	Type        string              `json:"type"`
	Ignored     bool                `json:"ignored,omitempty"`
	Reason      string              `json:"reason,omitempty"`
	SessionUUID string              `json:"session_uuid,omitempty"`
	MessageUUID string              `json:"message_uuid,omitempty"`
	Inbound     *sdk.InboundMessage `json:"inbound,omitempty"`
}

func (Connector) Metadata() sdk.ConnectorMetadata {
	return sdk.ConnectorMetadata{
		ID:          ID,
		Platform:    Platform,
		Label:       "Slack",
		Description: "Connect Slack bot accounts to Beak Channel Gateway",
		Capabilities: sdk.Capabilities{
			LoginModes:          []string{sdk.LoginModeCredential},
			Text:                true,
			Media:               false,
			GroupChat:           true,
			DirectChat:          true,
			Stream:              false,
			Webhook:             true,
			WebhookRegistration: sdk.WebhookRegistrationManual,
			BlockStreaming:      false,
			AckModes:            []string{"reaction"},
		},
	}
}

func (Connector) CredentialSchema(context.Context) sdk.CredentialSchema {
	return sdk.CredentialSchema{
		Type:       "object",
		LoginModes: []string{sdk.LoginModeCredential},
		Properties: map[string]sdk.CredentialField{
			"bot_token": {
				Type:        "string",
				Title:       "Bot Token",
				Description: "Bot user OAuth token from your Slack app (xoxb- prefix). Used for all Web API calls and auth.test validation.",
				Secret:      true,
			},
			"signing_secret": {
				Type:        "string",
				Title:       "Signing Secret",
				Description: "Signing secret from your Slack app's Basic Information page. Used to verify HMAC-SHA256 signatures on inbound Events API webhook requests.",
				Secret:      true,
			},
		},
		Required: []string{
			"bot_token",
			"signing_secret",
		},
		AdditionalProperties: false,
	}
}

func (Connector) ValidateCredential(ctx context.Context, req sdk.CredentialValidationRequest) (*sdk.CredentialValidationResult, error) {
	credential := cloneMap(req.Credential)
	stateMap := cloneMap(req.State)

	client := platform.NewClient("", credentialStrings(credential))
	client.HTTPClient = req.Runtime.HTTPClient

	info, err := client.Validate(ctx)
	if err != nil {
		return &sdk.CredentialValidationResult{
			Valid:       false,
			AccountKey:  firstString(credential["account_id"], credential["bot_id"]),
			DisplayName: firstString(credential["display_name"], credential["account_id"]),
			Credential:  credential,
			State:       stateMap,
			Metadata:    map[string]any{"platform": Platform},
			Error:       err.Error(),
		}, nil
	}

	credential["account_id"] = info.AccountID
	credential["bot_id"] = info.BotID
	// Persist bot identity to state so inbound ownership / self-echo checks have
	// it without re-calling auth.test. Never write tokens back to credential.
	if strings.TrimSpace(info.TeamID) != "" {
		stateMap["team_id"] = info.TeamID
	}
	if strings.TrimSpace(info.BotID) != "" {
		stateMap["bot_id"] = info.BotID
	}
	if strings.TrimSpace(info.BotUserID) != "" {
		credential["bot_user_id"] = info.BotUserID
		stateMap["bot_user_id"] = info.BotUserID
	}
	// Standardized nested identity (in addition to the flat keys above, which
	// self-echo detection still reads) so generic conformance/host tooling can
	// find the bot's identity at a single well-known path.
	if id := firstString(info.BotUserID, info.BotID, info.AccountID); id != "" {
		stateMap["bot_identity"] = map[string]any{
			"id":           id,
			"id_type":      slackBotIdentityType(id, info),
			"display_name": firstString(info.BotName, info.DisplayName),
		}
	}
	if identities := slackBotIdentities(info); len(identities) > 0 {
		stateMap["bot_identities"] = identities
	}
	return &sdk.CredentialValidationResult{
		Valid:       true,
		AccountKey:  info.AccountID,
		DisplayName: firstString(info.DisplayName, info.BotName, info.AccountID),
		Credential:  credential,
		State:       stateMap,
		Metadata: map[string]any{
			"platform": Platform,
			"bot_id":   info.BotID,
		},
	}, nil
}

func (Connector) StartLogin(context.Context, sdk.LoginStartRequest) (*sdk.LoginChallenge, error) {
	return nil, ErrCredentialLogin
}

func (Connector) PollLogin(context.Context, sdk.LoginPollRequest) (*sdk.LoginStatus, error) {
	return nil, ErrCredentialLogin
}

func (c Connector) Start(ctx context.Context, runtime sdk.Runtime) error {
	if runtime.Gateway == nil {
		return fmt.Errorf("%s connector requires sdk.Runtime.Gateway", Platform)
	}
	if _, err := runtime.Gateway.EnsureChannel(ctx, sdk.EnsureChannelRequest{
		WorkspaceUUID: runtime.WorkspaceUUID,
		Platform:      Platform,
		Name:          "Slack",
		Config:        map[string]any{"bridge": ID},
	}); err != nil {
		return err
	}

	store := newConnectorStateStore(runtime.AccountStore)
	for _, account := range runtimeAccountCandidates(runtime) {
		store.seed(account)
		accountUUID := accountKey(account)
		if accountUUID == "" {
			return fmt.Errorf("%s account_uuid or account_id is required", Platform)
		}
		sessionUUID, err := runtime.Gateway.EnsureChannelLinkSession(ctx, sdk.EnsureChannelLinkSessionRequest{
			WorkspaceUUID:       runtime.WorkspaceUUID,
			Platform:            Platform,
			AccountUUID:         accountUUID,
			AgentParticipantID:  runtime.Gateway.AgentParticipantID(),
			BridgeParticipantID: runtime.Gateway.BridgeParticipantID(Platform),
		})
		if err != nil {
			return err
		}
		st, err := store.LoadAccount(ctx, accountUUID)
		if err != nil {
			return err
		}
		if st.ChannelLinkSession != sessionUUID {
			st.ChannelLinkSession = sessionUUID
			if err := store.SaveAccount(ctx, st); err != nil {
				return err
			}
		}
	}
	return nil
}

func (Connector) Send(ctx context.Context, runtime sdk.Runtime, req sdk.OutboundMessage) (*sdk.SendResult, error) {
	account, err := selectRuntimeAccount(runtime, req.AccountUUID)
	if err != nil {
		return nil, err
	}
	accountUUID := accountKey(account)
	if accountUUID == "" {
		return nil, fmt.Errorf("%s outbound account is required", Platform)
	}
	if strings.TrimSpace(req.ChatID) == "" {
		return nil, fmt.Errorf("%s outbound chat_id is required", Platform)
	}
	if strings.TrimSpace(req.Text) == "" {
		return nil, fmt.Errorf("%s outbound text is required", Platform)
	}

	store := newConnectorStateStore(runtime.AccountStore)
	store.seed(account)
	st, err := store.LoadAccount(ctx, accountUUID)
	if err != nil {
		return nil, err
	}

	client := platform.NewClient("", credentialStrings(account.Credential))
	client.HTTPClient = runtime.HTTPClient
	messageID, err := client.SendText(ctx, req.ChatID, slackOutboundThreadTS(req), req.Text, req.Format, req.Mentions, req.MentionAll)
	if err != nil {
		return nil, err
	}
	if err := store.SaveAccount(ctx, st); err != nil {
		return nil, err
	}
	return &sdk.SendResult{
		Platform:    Platform,
		AccountUUID: accountUUID,
		MessageID:   messageID,
	}, nil
}

func (Connector) Acknowledge(ctx context.Context, runtime sdk.Runtime, req sdk.OutboundAck) (*sdk.AckResult, error) {
	account, err := selectRuntimeAccount(runtime, req.AccountUUID)
	if err != nil {
		return nil, err
	}
	accountUUID := accountKey(account)
	result := &sdk.AckResult{
		Platform:    Platform,
		AccountUUID: accountUUID,
		Mode:        "reaction",
		Status:      "skipped",
	}
	if mode := slackUnsupportedAckMode(req); mode != "" {
		result.Mode = mode
		result.Status = "unsupported"
		result.Raw = map[string]any{"reason": "unsupported_ack_mode"}
		return result, nil
	}
	if !slackAckWantsReaction(req) {
		return result, nil
	}
	chatID := slackAckChatID(req)
	if chatID == "" {
		result.Raw = map[string]any{"reason": "missing_chat_id"}
		return result, nil
	}
	messageID := slackAckTargetMessageID(req)
	if messageID == "" {
		result.Raw = map[string]any{"reason": "missing_target_message_id"}
		return result, nil
	}
	emojiName := slackAckEmojiName(req)
	client := platform.NewClient("", credentialStrings(account.Credential))
	client.HTTPClient = runtime.HTTPClient
	if err := client.AddReaction(ctx, chatID, messageID, emojiName); err != nil {
		return nil, err
	}
	result.Status = "sent"
	result.ReactionID = chatID + ":" + messageID + ":" + emojiName
	result.Raw = map[string]any{
		"channel":     chatID,
		"message_ts":  messageID,
		"emoji_name":  emojiName,
		"reaction_id": result.ReactionID,
	}
	return result, nil
}

func (Connector) Stop(ctx context.Context, account sdk.ChannelAccount) error {
	_ = ctx
	_ = account
	return nil
}

// HandleWebhookRequest implements the sdk webhook entry point. It reads the raw
// body, answers the Slack url_verification challenge (which carries no valid
// signature) before verifying anything, verifies the HMAC signature for all
// event_callback deliveries, then turns the event into a Beak session/message.
func (c Connector) HandleWebhookRequest(ctx context.Context, runtime sdk.Runtime, account sdk.ChannelAccount, req *http.Request) (*sdk.WebhookResponse, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("%s read webhook body: %w", Platform, err)
	}

	var envelope platform.EventEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%s decode webhook envelope: %w", Platform, err)
	}

	// url_verification is the initial Slack handshake; it has no signature and
	// must be answered before signature verification.
	if envelope.Type == "url_verification" {
		return jsonWebhookResponse(map[string]string{"challenge": envelope.Challenge})
	}

	if err := platform.VerifyWebhookSignature(
		stringValue(account.Credential["signing_secret"]),
		req.Header.Get("X-Slack-Request-Timestamp"),
		req.Header.Get("X-Slack-Signature"),
		body,
		time.Now().UTC(),
	); err != nil {
		return nil, err
	}

	processCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	go func() {
		defer cancel()
		if _, err := c.HandleWebhook(processCtx, runtime, account, body); err != nil {
			logRuntimeError(runtime, "slack webhook processing failed: %v", err)
		}
	}()
	// Slack requires a 2xx within 3s; the body is ignored for events.
	return &sdk.WebhookResponse{StatusCode: http.StatusOK}, nil
}

// HandleWebhook parses an already-verified Slack Events API body and runs the
// inbound flow. It is separated from HandleWebhookRequest so tests can drive
// the inbound logic directly and inspect the resulting EventResult.
func (c Connector) HandleWebhook(ctx context.Context, runtime sdk.Runtime, account sdk.ChannelAccount, body []byte) (*EventResult, error) {
	var envelope platform.EventEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%s decode webhook envelope: %w", Platform, err)
	}
	if envelope.Type == "url_verification" {
		return &EventResult{Type: "url_verification"}, nil
	}
	if envelope.Type != "event_callback" {
		return &EventResult{Type: envelope.Type, Ignored: true, Reason: "unsupported_event_type"}, nil
	}

	var event platform.InnerEvent
	if len(envelope.Event) > 0 {
		if err := json.Unmarshal(envelope.Event, &event); err != nil {
			return nil, fmt.Errorf("%s decode inner event: %w", Platform, err)
		}
	}
	return c.processMessageEvent(ctx, runtime, account, &envelope, &event)
}

// processMessageEvent implements the inbound standard flow: ownership check →
// self-echo filter → text-only filter → chat identity normalization → mention
// detection → dedupe → EnsureChatSession + CreateMessage + state persistence.
func (c Connector) processMessageEvent(ctx context.Context, runtime sdk.Runtime, account sdk.ChannelAccount, envelope *platform.EventEnvelope, event *platform.InnerEvent) (*EventResult, error) {
	accountUUID := accountKey(account)
	if accountUUID == "" {
		return nil, fmt.Errorf("%s account_uuid or account_id is required", Platform)
	}

	store := newConnectorStateStore(runtime.AccountStore)
	store.seed(account)
	st, err := store.LoadAccount(ctx, accountUUID)
	if err != nil {
		return nil, err
	}

	// Resolve bot identity from the most authoritative source available. team_id
	// is the bare workspace id (account_id is the composite team:user key, so it
	// is not used for ownership comparison).
	teamID := firstString(st.TeamID, account.State["team_id"])
	botID := firstString(st.BotID, account.Credential["bot_id"], account.State["bot_id"])
	botUserID := firstString(st.BotUserID, account.Credential["bot_user_id"], account.State["bot_user_id"])
	apiAppID := firstString(account.Credential["api_app_id"], account.State["api_app_id"])

	// 1. Ownership: reject events for a different workspace sharing this endpoint.
	if teamID != "" && envelope.TeamID != "" && envelope.TeamID != teamID {
		return &EventResult{Type: event.Type, Ignored: true, Reason: "team_mismatch"}, nil
	}

	if event.Type != "message" && event.Type != "app_mention" {
		return &EventResult{Type: event.Type, Ignored: true, Reason: "unsupported_event_type"}, nil
	}

	// 2. Self-echo: drop the bot's own messages.
	if event.Subtype == "bot_message" ||
		(botID != "" && event.BotID == botID) ||
		(botUserID != "" && event.User == botUserID) ||
		(apiAppID != "" && event.AppID == apiAppID) {
		return &EventResult{Type: event.Type, Ignored: true, Reason: "self_echo"}, nil
	}

	// 3. Normalize chat identity.
	chatType := sdk.ChatTypeGroup
	if event.ChannelType == "im" {
		chatType = sdk.ChatTypeDirect
	}
	chatID := strings.TrimSpace(event.Channel)
	senderID := strings.TrimSpace(event.User)
	text := strings.TrimSpace(event.Text)

	// 4. Mention detection. <!channel> and <!here> are Slack's literal
	// broadcast-mention tokens; they address the whole channel, not this bot,
	// so they must never flip mentionedMe on their own.
	mentions := slackMentions(event.Text)
	mentionedMe := event.Type == "app_mention" || slackMentionsContain(mentions, botUserID) || slackMentionsContain(mentions, botID)
	mentionAll := slackTextHasBroadcastMention(event.Text)

	// 5. Text-only filter (skip non-text / incomplete events).
	if chatType == "" || chatID == "" || senderID == "" || text == "" {
		return &EventResult{Type: event.Type, Ignored: true, Reason: "unsupported_message_type"}, nil
	}

	// 6. Dedupe.
	messageID := firstString(event.TS, event.ClientMsgID, event.EventTS, envelope.EventID)
	dedupeKey := accountUUID + ":message:" + chatID + ":" + messageID
	stateKey := Platform + ":" + chatType + ":" + chatID
	if _, ok := st.InboundSeen[dedupeKey]; ok {
		return &EventResult{Type: event.Type, Ignored: true, Reason: "duplicate", SessionUUID: st.PeerSessions[stateKey]}, nil
	}

	chatDisplayName, chatAvatarURL, chatIdentity, senderDisplayName := slackInboundDisplayFields(ctx, runtime, account, chatType, chatID, senderID)
	referenced := slackReferencedMessage(ctx, runtime, account, event, chatType, chatID)

	inbound := sdk.InboundMessage{
		WorkspaceUUID:     runtime.WorkspaceUUID,
		Platform:          Platform,
		AccountUUID:       accountUUID,
		ChannelUUID:       runtime.Channel.UUID,
		ChatType:          chatType,
		ChatID:            chatID,
		ThreadID:          event.ThreadTS,
		ChatDisplayName:   chatDisplayName,
		ChatAvatarURL:     chatAvatarURL,
		ChatIdentity:      chatIdentity,
		SenderID:          senderID,
		SenderDisplayName: senderDisplayName,
		MessageID:         event.TS,
		Text:              text,
		ReferencedMessage: referenced,
		DedupeKey:         dedupeKey,
		Mentions:          mentions,
		MentionedMe:       mentionedMe,
		MentionAll:        mentionAll,
		Raw: map[string]any{
			"event_id":       envelope.EventID,
			"event_ts":       event.EventTS,
			"channel":        chatID,
			"thread_ts":      event.ThreadTS,
			"parent_user_id": event.ParentUserID,
			"ts":             event.TS,
			"user":           senderID,
			"subtype":        event.Subtype,
			"api_app_id":     envelope.APIAppID,
		},
	}

	// 7a. Ensure the chat session (identity includes account uuid → two bots in
	// the same group never share a session).
	sessionUUID, err := runtime.Gateway.EnsureChatSession(ctx, sdk.EnsureChatSessionRequest{
		WorkspaceUUID:       runtime.WorkspaceUUID,
		Platform:            Platform,
		AccountUUID:         accountUUID,
		ChatType:            chatType,
		ChatID:              chatID,
		ThreadID:            inbound.ThreadID,
		ChatDisplayName:     inbound.ChatDisplayName,
		ChatAvatarURL:       inbound.ChatAvatarURL,
		ChatIdentity:        inbound.ChatIdentity,
		SenderID:            senderID,
		AgentParticipantID:  runtime.Gateway.AgentParticipantID(),
		BridgeParticipantID: runtime.Gateway.BridgeParticipantID(Platform),
		Metadata: map[string]any{
			"slack_chat_type": chatType,
			"slack_chat_id":   chatID,
			"slack_thread_ts": event.ThreadTS,
		},
	})
	if err != nil {
		return nil, err
	}

	// 7b. Create the Beak message.
	messageUUID, err := runtime.Gateway.CreateMessage(ctx, sdk.CreateMessageRequest{
		WorkspaceUUID: runtime.WorkspaceUUID,
		SessionUUID:   sessionUUID,
		SenderID:      sdk.IMPersonParticipantID(Platform, chatType, chatID, senderID),
		Content:       text,
		DedupeKey:     dedupeKey,
		Metadata: map[string]any{
			"source":          Platform,
			"slack_chat_type": chatType,
			"slack_chat_id":   chatID,
			"inbound_message": inbound,
		},
	})
	if err != nil {
		return nil, err
	}

	// 7c. Persist session mapping and dedupe marker.
	st.PeerSessions[stateKey] = sessionUUID
	st.InboundSeen[dedupeKey] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.SaveAccount(ctx, st); err != nil {
		return nil, err
	}

	return &EventResult{
		Type:        event.Type,
		SessionUUID: sessionUUID,
		MessageUUID: messageUUID,
		Inbound:     &inbound,
	}, nil
}

// jsonWebhookResponse builds a 200 JSON webhook response.
func jsonWebhookResponse(value any) (*sdk.WebhookResponse, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &sdk.WebhookResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"content-type": "application/json; charset=utf-8"},
		Body:       data,
	}, nil
}

// connectorStateStore adapts sdk.AccountStore to typed state.AccountState.
type connectorStateStore struct {
	mu           sync.Mutex
	accounts     map[string]*state.AccountState
	accountStore sdk.AccountStore
}

func newConnectorStateStore(accountStore sdk.AccountStore) *connectorStateStore {
	return &connectorStateStore{
		accounts:     make(map[string]*state.AccountState),
		accountStore: accountStore,
	}
}

func (s *connectorStateStore) seed(account sdk.ChannelAccount) {
	accountID := accountKey(account)
	if accountID == "" {
		return
	}
	// Registration only; LoadAccount is the single point that populates the
	// cache (rehydrating from the AccountStore when present), so seed must not
	// pre-insert an empty state that would shadow persisted state.
	_ = accountID
}

func (s *connectorStateStore) LoadAccount(ctx context.Context, accountID string) (*state.AccountState, error) {
	s.mu.Lock()
	if st, ok := s.accounts[accountID]; ok {
		s.mu.Unlock()
		return st, nil
	}
	accountStore := s.accountStore
	s.mu.Unlock()

	st := &state.AccountState{AccountID: accountID}
	if accountStore != nil {
		raw, err := accountStore.LoadChannelAccountState(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			if data, err := json.Marshal(raw); err == nil {
				_ = json.Unmarshal(data, st)
			}
			st.AccountID = accountID
		}
	}
	st.EnsureMaps()

	s.mu.Lock()
	s.accounts[accountID] = st
	s.mu.Unlock()
	return st, nil
}

func (s *connectorStateStore) SaveAccount(ctx context.Context, account *state.AccountState) error {
	if err := state.TouchAccount(account); err != nil {
		return err
	}
	s.mu.Lock()
	s.accounts[account.AccountID] = account
	accountStore := s.accountStore
	s.mu.Unlock()
	if accountStore != nil {
		data, err := json.Marshal(account)
		if err != nil {
			return err
		}
		var persisted map[string]any
		if err := json.Unmarshal(data, &persisted); err != nil {
			return err
		}
		return accountStore.SaveChannelAccountState(ctx, account.AccountID, persisted)
	}
	return nil
}

var slackUserMentionPattern = regexp.MustCompile(`<@([^>|]+)(?:\|([^>]+))?>`)

func slackBotIdentityType(id string, info *platform.BotInfo) string {
	switch strings.TrimSpace(id) {
	case strings.TrimSpace(info.BotUserID):
		return "user_id"
	case strings.TrimSpace(info.BotID):
		return "bot_id"
	default:
		return "account_id"
	}
}

func slackBotIdentities(info *platform.BotInfo) []map[string]any {
	if info == nil {
		return nil
	}
	displayName := firstString(info.BotName, info.DisplayName)
	var identities []map[string]any
	add := func(id, idType string) {
		if strings.TrimSpace(id) == "" {
			return
		}
		item := map[string]any{
			"id":      strings.TrimSpace(id),
			"id_type": idType,
		}
		if displayName != "" {
			item["display_name"] = displayName
		}
		identities = append(identities, item)
	}
	add(info.BotUserID, "user_id")
	add(info.BotID, "bot_id")
	return identities
}

func slackMentions(text string) []sdk.MentionIdentity {
	matches := slackUserMentionPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	mentions := make([]sdk.MentionIdentity, 0, len(matches))
	for _, match := range matches {
		id := strings.TrimSpace(match[1])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		displayName := ""
		if len(match) > 2 {
			displayName = strings.TrimSpace(match[2])
		}
		mentions = append(mentions, sdk.MentionIdentity{
			ID:          id,
			IDType:      "user_id",
			DisplayName: displayName,
		})
	}
	return mentions
}

func slackMentionsContain(mentions []sdk.MentionIdentity, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, mention := range mentions {
		if strings.TrimSpace(mention.ID) == id {
			return true
		}
	}
	return false
}

func slackTextHasBroadcastMention(text string) bool {
	return strings.Contains(text, "<!channel>") ||
		strings.Contains(text, "<!here>") ||
		strings.Contains(text, "<!everyone>")
}

func slackInboundDisplayFields(ctx context.Context, runtime sdk.Runtime, account sdk.ChannelAccount, chatType, chatID, senderID string) (string, string, sdk.ChatIdentity, string) {
	chatIdentity := sdk.ChatIdentity{
		ID:     chatID,
		IDType: "channel_id",
		Type:   chatType,
	}
	client := platform.NewClient("", credentialStrings(account.Credential))
	client.HTTPClient = runtime.HTTPClient
	client.RequestTimeout = 2 * time.Second

	var senderDisplayName string
	var senderAvatarURL string
	if sender, err := client.UserInfo(ctx, senderID); err == nil && sender != nil {
		senderDisplayName = firstString(sender.DisplayName, sender.RealName, sender.Name, sender.ID)
		senderAvatarURL = sender.AvatarURL
	}

	if chatType == sdk.ChatTypeDirect {
		chatDisplayName := senderDisplayName
		chatAvatarURL := senderAvatarURL
		chatIdentity.DisplayName = chatDisplayName
		chatIdentity.AvatarURL = chatAvatarURL
		return chatDisplayName, chatAvatarURL, chatIdentity, senderDisplayName
	}

	chatDisplayName := ""
	if channel, err := client.ConversationInfo(ctx, chatID); err == nil && channel != nil {
		chatDisplayName = firstString(channel.Name, channel.ID)
		chatIdentity.DisplayName = chatDisplayName
	}
	return chatDisplayName, "", chatIdentity, senderDisplayName
}

func slackReferencedMessage(ctx context.Context, runtime sdk.Runtime, account sdk.ChannelAccount, event *platform.InnerEvent, chatType, chatID string) *sdk.ReferencedMessage {
	if event == nil {
		return nil
	}
	threadTS := strings.TrimSpace(event.ThreadTS)
	messageTS := strings.TrimSpace(event.TS)
	if threadTS == "" || messageTS == "" || threadTS == messageTS {
		return nil
	}
	ref := &sdk.ReferencedMessage{
		Platform:    Platform,
		MessageID:   threadTS,
		ChatType:    chatType,
		ChatID:      chatID,
		ThreadID:    threadTS,
		RootID:      threadTS,
		SenderID:    strings.TrimSpace(event.ParentUserID),
		MessageType: "text",
		Raw: map[string]any{
			"thread_ts":      threadTS,
			"parent_user_id": strings.TrimSpace(event.ParentUserID),
		},
	}

	client := platform.NewClient("", credentialStrings(account.Credential))
	client.HTTPClient = runtime.HTTPClient
	client.RequestTimeout = 2 * time.Second
	parent, err := client.ThreadParent(ctx, chatID, threadTS)
	if err != nil {
		ref.Raw["fetch_error"] = err.Error()
		return ref
	}
	if parent == nil {
		return ref
	}
	ref.MessageID = firstString(parent.TS, ref.MessageID)
	ref.ThreadID = firstString(parent.ThreadTS, ref.ThreadID)
	ref.RootID = firstString(parent.ThreadTS, ref.RootID)
	ref.SenderID = firstString(parent.User, parent.BotID, ref.SenderID)
	ref.SenderDisplayName = strings.TrimSpace(parent.Username)
	if subtype := strings.TrimSpace(parent.Subtype); subtype != "" {
		ref.MessageType = subtype
	}
	ref.Text = strings.TrimSpace(parent.Text)
	ref.CreatedAt = strings.TrimSpace(parent.TS)
	ref.Raw["fetched"] = true
	ref.Raw["bot_id"] = strings.TrimSpace(parent.BotID)
	ref.Raw["subtype"] = strings.TrimSpace(parent.Subtype)

	if ref.SenderDisplayName == "" && strings.TrimSpace(parent.User) != "" {
		if sender, err := client.UserInfo(ctx, parent.User); err == nil && sender != nil {
			ref.SenderDisplayName = firstString(sender.DisplayName, sender.RealName, sender.Name, sender.ID)
		}
	}
	return ref
}

func slackAckWantsReaction(req sdk.OutboundAck) bool {
	action := strings.ToLower(strings.TrimSpace(firstString(req.Action, req.Raw["action"])))
	return action == "" || action == "start" || action == "processing"
}

func slackUnsupportedAckMode(req sdk.OutboundAck) string {
	mode := strings.ToLower(strings.TrimSpace(firstString(req.Mode, req.Raw["mode"])))
	if mode == "" || mode == "auto" || mode == "reaction" {
		return ""
	}
	return mode
}

func slackAckChatID(req sdk.OutboundAck) string {
	return firstString(req.ChatID, req.Raw["chat_id"], req.Raw["channel"], req.Raw["slack_channel_id"])
}

func slackAckTargetMessageID(req sdk.OutboundAck) string {
	return firstString(req.TargetMessageID, req.Raw["target_message_id"], req.Raw["message_id"], req.Raw["slack_message_ts"], req.Raw["ts"])
}

func slackOutboundThreadTS(req sdk.OutboundMessage) string {
	return firstString(req.ThreadID, req.Raw["thread_ts"], req.Raw["thread_id"], req.Raw["slack_thread_ts"])
}

func slackAckEmojiName(req sdk.OutboundAck) string {
	value := strings.Trim(strings.TrimSpace(firstString(req.Emoji, req.Raw["emoji"], req.Raw["emoji_name"])), ":")
	switch strings.ToLower(value) {
	case "", "eyes", "processing":
		return "eyes"
	case "thinking", "think":
		return "thinking_face"
	case "typing":
		return "keyboard"
	case "ok", "done", "check", "checkmark", "check_mark":
		return "white_check_mark"
	case "wait", "one_second", "one-second", "onesecond":
		return "hourglass_flowing_sand"
	default:
		return value
	}
}

func runtimeAccountCandidates(runtime sdk.Runtime) []sdk.ChannelAccount {
	seen := make(map[string]bool)
	var out []sdk.ChannelAccount
	add := func(account sdk.ChannelAccount) {
		key := accountKey(account)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, account)
	}
	add(runtime.Account)
	for _, account := range runtime.Accounts {
		add(account)
	}
	return out
}

func selectRuntimeAccount(runtime sdk.Runtime, accountUUID string) (sdk.ChannelAccount, error) {
	accountUUID = strings.TrimSpace(accountUUID)
	candidates := runtimeAccountCandidates(runtime)
	if accountUUID != "" {
		for _, account := range candidates {
			if accountMatches(account, accountUUID) {
				return account, nil
			}
		}
		return sdk.ChannelAccount{}, fmt.Errorf("%s account %s not found in runtime", Platform, accountUUID)
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return sdk.ChannelAccount{}, fmt.Errorf("%s outbound account is required", Platform)
	}
	return sdk.ChannelAccount{}, fmt.Errorf("%s outbound account is ambiguous; account_uuid is required", Platform)
}

func accountMatches(account sdk.ChannelAccount, accountID string) bool {
	return strings.TrimSpace(account.UUID) == accountID ||
		strings.TrimSpace(stringValue(account.Credential["account_id"])) == accountID ||
		strings.TrimSpace(stringValue(account.Credential["bot_id"])) == accountID
}

func accountKey(account sdk.ChannelAccount) string {
	return firstString(account.UUID, account.Credential["account_id"], account.Credential["bot_id"])
}

func credentialStrings(credential map[string]any) map[string]string {
	out := make(map[string]string, len(credential))
	for key, value := range credential {
		out[key] = stringValue(value)
	}
	return out
}

func cloneMap(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func firstString(values ...any) string {
	for _, value := range values {
		if s := strings.TrimSpace(stringValue(value)); s != "" {
			return s
		}
	}
	return ""
}

func logRuntimeError(runtime sdk.Runtime, format string, args ...any) {
	if runtime.Logger != nil {
		runtime.Logger.Printf(format, args...)
	}
}
