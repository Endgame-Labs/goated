package telegram

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"goated/internal/gateway"
	"goated/internal/util"
)

// OffsetStore persists the last processed Telegram update ID so restarts
// don't replay old messages.
type OffsetStore interface {
	GetMeta(key string) string
	SetMeta(key, value string) error
}

type Connector struct {
	bot   *tgbotapi.BotAPI
	store OffsetStore

	httpClient *http.Client

	attachmentsRootRel string
	attachmentsRootAbs string

	attachmentMaxBytes  int64
	attachmentTotalMax  int64
	attachmentRetention time.Duration
	sweepEvery          time.Duration

	allowedChatIDs map[int64]struct{}
	botUsername    string
	botUserID      int64
}

type RunMode string

const (
	RunModePolling RunMode = "polling"
	RunModeWebhook RunMode = "webhook"
)

type WebhookOptions struct {
	PublicURL  string
	ListenAddr string
	Path       string
}

func NewConnector(token string, allowedChatIDs []int64, store OffsetStore, attachmentCfg AttachmentConfig) (*Connector, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("init telegram bot: %w", err)
	}

	rootPath := strings.TrimSpace(attachmentCfg.RootPath)
	if rootPath == "" {
		rootPath = defaultAttachmentsRootRel
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("resolve attachments root: %w", err)
	}
	if err := os.MkdirAll(rootAbs, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir attachments root: %w", err)
	}
	rootRel := filepath.ToSlash(rootPath)
	if filepath.IsAbs(rootPath) {
		if cwd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(cwd, rootPath); err == nil {
				rootRel = filepath.ToSlash(rel)
			}
		}
	}
	maxBytes := attachmentCfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultAttachmentMaxBytes
	}
	maxTotalBytes := attachmentCfg.MaxTotalBytes
	if maxTotalBytes <= 0 {
		maxTotalBytes = defaultAttachmentTotalMax
	}

	allowed := make(map[int64]struct{}, len(allowedChatIDs))
	for _, id := range allowedChatIDs {
		allowed[id] = struct{}{}
	}

	return &Connector{
		bot:                 bot,
		store:               store,
		httpClient:          &http.Client{Timeout: 2 * time.Minute},
		attachmentsRootRel:  rootRel,
		attachmentsRootAbs:  rootAbs,
		attachmentMaxBytes:  maxBytes,
		attachmentTotalMax:  maxTotalBytes,
		attachmentRetention: defaultAttachmentRetention,
		sweepEvery:          defaultAttachmentSweepEvery,
		allowedChatIDs:      allowed,
		botUsername:         bot.Self.UserName,
		botUserID:           bot.Self.ID,
	}, nil
}

func (c *Connector) Run(ctx context.Context, handler gateway.Handler, mode RunMode, webhookOpts WebhookOptions) error {
	go c.runAttachmentSweeper(ctx)

	switch mode {
	case RunModePolling:
		return c.runPolling(ctx, handler)
	case RunModeWebhook:
		return c.runWebhook(ctx, handler, webhookOpts)
	default:
		return fmt.Errorf("unsupported telegram mode %q", mode)
	}
}

const metaKeyTelegramOffset = "telegram_update_offset"

func (c *Connector) loadOffset() int {
	if c.store == nil {
		return 0
	}
	raw := c.store.GetMeta(metaKeyTelegramOffset)
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return v
}

func (c *Connector) saveOffset(offset int) {
	if c.store == nil {
		return
	}
	_ = c.store.SetMeta(metaKeyTelegramOffset, strconv.Itoa(offset))
}

func (c *Connector) runPolling(ctx context.Context, handler gateway.Handler) error {
	if _, err := c.bot.Request(tgbotapi.DeleteWebhookConfig{DropPendingUpdates: false}); err != nil {
		return fmt.Errorf("delete webhook before polling: %w", err)
	}

	offset := c.loadOffset()
	u := tgbotapi.NewUpdate(offset)
	u.Timeout = 30
	updates := c.bot.GetUpdatesChan(u)
	defer c.bot.StopReceivingUpdates()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case update := <-updates:
			// Persist offset so restarts don't replay old messages
			c.saveOffset(update.UpdateID + 1)

			if err := c.processUpdate(ctx, handler, update); err != nil {
				chatID := "unknown"
				if update.Message != nil {
					chatID = strconv.FormatInt(update.Message.Chat.ID, 10)
				}
				if chatID != "unknown" {
					_ = c.SendMessage(ctx, chatID, "Error: "+err.Error())
				}
			}
		}
	}
}

func (c *Connector) runWebhook(ctx context.Context, handler gateway.Handler, opts WebhookOptions) error {
	if strings.TrimSpace(opts.PublicURL) == "" {
		return fmt.Errorf("webhook mode requires public URL")
	}
	if strings.TrimSpace(opts.ListenAddr) == "" {
		opts.ListenAddr = ":8080"
	}
	if strings.TrimSpace(opts.Path) == "" {
		opts.Path = "/telegram/webhook"
	}

	webhook, err := tgbotapi.NewWebhook(strings.TrimRight(opts.PublicURL, "/") + opts.Path)
	if err != nil {
		return fmt.Errorf("build webhook config: %w", err)
	}
	if _, err := c.bot.Request(webhook); err != nil {
		return fmt.Errorf("set telegram webhook: %w", err)
	}

	updates := c.bot.ListenForWebhook(opts.Path)
	server := &http.Server{
		Addr:              opts.ListenAddr,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
			return
		}
		serverErrCh <- nil
	}()

	for {
		select {
		case <-ctx.Done():
			_, _ = c.bot.Request(tgbotapi.DeleteWebhookConfig{DropPendingUpdates: false})
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
			return ctx.Err()
		case err := <-serverErrCh:
			if err != nil {
				return fmt.Errorf("webhook server: %w", err)
			}
			return nil
		case update := <-updates:
			if err := c.processUpdate(ctx, handler, update); err != nil {
				chatID := "unknown"
				if update.Message != nil {
					chatID = strconv.FormatInt(update.Message.Chat.ID, 10)
				}
				if chatID != "unknown" {
					_ = c.SendMessage(ctx, chatID, "Error: "+err.Error())
				}
			}
		}
	}
}

func (c *Connector) processUpdate(ctx context.Context, handler gateway.Handler, update tgbotapi.Update) error {
	if update.Message == nil {
		return nil
	}

	chatID := update.Message.Chat.ID
	if len(c.allowedChatIDs) > 0 {
		if _, ok := c.allowedChatIDs[chatID]; !ok {
			userID := int64(0)
			if update.Message.From != nil {
				userID = int64(update.Message.From.ID)
			}
			fmt.Fprintf(os.Stderr,
				"telegram: rejected message from unauthorized chat_id=%d user_id=%d\n",
				chatID, userID,
			)
			_ = c.SendMessage(ctx, strconv.FormatInt(chatID, 10),
				"Sorry, you're not authorized to use this bot.")
			return nil
		}
	}

	isGroup := update.Message.Chat.IsGroup() || update.Message.Chat.IsSuperGroup()
	if isGroup && !c.messageAddressesBot(update.Message) {
		return nil
	}

	text := strings.TrimSpace(update.Message.Text)
	if isGroup {
		text = c.stripBotMention(text)
		text = strings.TrimSpace(text)
	}
	if text == "" {
		text = strings.TrimSpace(update.Message.Caption)
	}

	attachments, attachmentResults, failed, succeeded := c.processAttachments(ctx, update.Message)
	if text == "" && len(attachments) == 0 && len(failed) == 0 {
		return nil
	}

	if len(attachments)+len(failed) > 0 {
		fmt.Fprintf(os.Stderr,
			"telegram attachments summary: count=%d accepted=%d failed=%d bytes=%d\n",
			len(attachmentResults), len(succeeded), len(failed), sumAttachmentBytes(succeeded),
		)
	}
	var userName, userUsername string
	if update.Message.From != nil {
		userName = strings.TrimSpace(update.Message.From.FirstName + " " + update.Message.From.LastName)
		userUsername = update.Message.From.UserName
	}
	msg := gateway.IncomingMessage{
		Channel:              "telegram",
		ChatID:               strconv.FormatInt(update.Message.Chat.ID, 10),
		UserID:               strconv.FormatInt(int64(update.Message.From.ID), 10),
		UserName:             userName,
		UserUsername:         userUsername,
		ChatType:             update.Message.Chat.Type,
		Text:                 text,
		MessageID:            strconv.Itoa(update.Message.MessageID),
		Attachments:          attachments,
		AttachmentResults:    attachmentResults,
		AttachmentsFailed:    failed,
		AttachmentsSucceeded: succeeded,
	}
	if update.Message.ReplyToMessage != nil {
		msg.ReplyToText = update.Message.ReplyToMessage.Text
		// Also check Caption for media messages (photos with captions)
		if msg.ReplyToText == "" {
			msg.ReplyToText = update.Message.ReplyToMessage.Caption
		}
		if update.Message.ReplyToMessage.From != nil {
			msg.ReplyToUserName = strings.TrimSpace(
				update.Message.ReplyToMessage.From.FirstName + " " + update.Message.ReplyToMessage.From.LastName,
			)
		}
	}
	stopTyping := c.startTypingLoop(ctx, update.Message.Chat.ID)
	defer stopTyping()
	return handler.HandleMessage(ctx, msg, c)
}

func (c *Connector) SendMessage(_ context.Context, chatID, text string) error {
	chat, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat id %q: %w", chatID, err)
	}

	// Try HTML-formatted message first
	htmlText := util.MarkdownToTelegramHTML(text)
	msg := tgbotapi.NewMessage(chat, htmlText)
	msg.ParseMode = "HTML"
	_, err = c.bot.Send(msg)
	if err == nil {
		return nil
	}

	// Fallback to plain text if Telegram rejects the HTML
	msg = tgbotapi.NewMessage(chat, text)
	_, err = c.bot.Send(msg)
	if err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}
	return nil
}

func (c *Connector) SendMedia(_ context.Context, chatID, filePath, caption, mediaType string) error {
	chat, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat id %q: %w", chatID, err)
	}
	if strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("file path is required")
	}
	if _, err := os.Stat(filePath); err != nil {
		return fmt.Errorf("stat file %s: %w", filePath, err)
	}

	resolvedType := telegramMediaType(filePath, mediaType)
	htmlCaption := util.MarkdownToTelegramHTML(caption)

	switch resolvedType {
	case "photo":
		msg := tgbotapi.NewPhoto(chat, tgbotapi.FilePath(filePath))
		msg.Caption = htmlCaption
		msg.ParseMode = "HTML"
		if _, err := c.bot.Send(msg); err == nil {
			return nil
		}

		msg = tgbotapi.NewPhoto(chat, tgbotapi.FilePath(filePath))
		msg.Caption = caption
		_, err := c.bot.Send(msg)
		if err != nil {
			return fmt.Errorf("send telegram photo: %w", err)
		}
		return nil

	case "document":
		msg := tgbotapi.NewDocument(chat, tgbotapi.FilePath(filePath))
		msg.Caption = htmlCaption
		msg.ParseMode = "HTML"
		if _, err := c.bot.Send(msg); err == nil {
			return nil
		}

		msg = tgbotapi.NewDocument(chat, tgbotapi.FilePath(filePath))
		msg.Caption = caption
		_, err := c.bot.Send(msg)
		if err != nil {
			return fmt.Errorf("send telegram document: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported telegram media type %q", resolvedType)
	}
}

func telegramMediaType(filePath, requested string) string {
	requested = strings.ToLower(strings.TrimSpace(requested))
	switch requested {
	case "photo", "document":
		return requested
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	mimeType := mime.TypeByExtension(ext)
	if strings.HasPrefix(mimeType, "image/") {
		return "photo"
	}
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return "photo"
	default:
		return "document"
	}
}

func (c *Connector) sendTyping(chatID int64) {
	action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	_, _ = c.bot.Send(action)
}

func (c *Connector) startTypingLoop(ctx context.Context, chatID int64) func() {
	c.sendTyping(chatID)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				c.sendTyping(chatID)
			}
		}
	}()
	return func() {
		close(done)
	}
}

// messageAddressesBot reports whether a group/supergroup message is directed
// at this bot. True if any of: (a) the message @-mentions the bot's username,
// (b) the message is a reply to one of the bot's messages, (c) the message is
// a bot_command targeting this bot (`/x` or `/x@botusername`).
func (c *Connector) messageAddressesBot(msg *tgbotapi.Message) bool {
	if msg == nil {
		return false
	}
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil {
		if msg.ReplyToMessage.From.ID == c.botUserID {
			return true
		}
	}
	if c.botUsername == "" {
		return false
	}
	mention := "@" + strings.ToLower(c.botUsername)
	for _, ent := range entitiesFromMessage(msg) {
		switch {
		case ent.IsMention():
			if strings.EqualFold(entityText(msg, ent), mention) {
				return true
			}
		case ent.IsCommand():
			cmd := entityText(msg, ent)
			// `/foo` or `/foo@botname` — Telegram delivers @-suffixed commands
			// only to the targeted bot, so trust those. `/foo` with no suffix
			// gets delivered to all bots in privacy-off groups; accept it if
			// the bot is a member.
			if at := strings.Index(cmd, "@"); at >= 0 {
				if strings.EqualFold(cmd[at:], mention) {
					return true
				}
			} else {
				return true
			}
		}
	}
	return false
}

// stripBotMention removes any `@botusername` substrings from text
// (case-insensitive) and collapses extra whitespace.
func (c *Connector) stripBotMention(text string) string {
	if c.botUsername == "" || text == "" {
		return text
	}
	target := "@" + strings.ToLower(c.botUsername)
	lower := strings.ToLower(text)
	var b strings.Builder
	i := 0
	for i < len(text) {
		idx := strings.Index(lower[i:], target)
		if idx < 0 {
			b.WriteString(text[i:])
			break
		}
		b.WriteString(text[i : i+idx])
		i += idx + len(target)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func entitiesFromMessage(msg *tgbotapi.Message) []tgbotapi.MessageEntity {
	if len(msg.Entities) > 0 {
		return msg.Entities
	}
	return msg.CaptionEntities
}

// entityText extracts the substring covered by a MessageEntity. Telegram
// entity offsets are UTF-16 code units; we convert via the UTF-16 view of the
// source text (Message.Text or Caption).
func entityText(msg *tgbotapi.Message, ent tgbotapi.MessageEntity) string {
	src := msg.Text
	if src == "" {
		src = msg.Caption
	}
	if src == "" {
		return ""
	}
	u16 := utf16.Encode([]rune(src))
	if ent.Offset < 0 || ent.Offset+ent.Length > len(u16) {
		return ""
	}
	return string(utf16.Decode(u16[ent.Offset : ent.Offset+ent.Length]))
}
