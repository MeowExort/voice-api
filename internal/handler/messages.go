package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/meowexort/voice-api/internal/model"
)

var mentionRegex = regexp.MustCompile(`<@([0-9a-f-]{36})>`)

type sendMessageRequest struct {
	Content    string   `json:"content" binding:"required,min=1,max=4000"`
	StickerID  *string  `json:"sticker_id"`
	Mentions   []string `json:"mentions"`
}

type editMessageRequest struct {
	Content string `json:"content" binding:"required,min=1,max=4000"`
}

func (h *Handler) getMessages(c *gin.Context) {
	serverID := c.Param("serverId")
	channelID := c.Param("channelId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	before := c.Query("before") // message ID for pagination

	var rows interface{ Close() }
	var err error

	if before != "" {
		r, e := h.db.Query(context.Background(),
			`SELECT m.id, m.content, m.author_id, u.username, m.edited_at, m.created_at,
			        COALESCE(json_agg(json_build_object('id', a.id, 'filename', a.filename, 'url', a.url, 'content_type', a.content_type, 'size_bytes', a.size_bytes)) FILTER (WHERE a.id IS NOT NULL), '[]')
			 FROM messages m
			 JOIN users u ON u.id = m.author_id
			 LEFT JOIN attachments a ON a.message_id = m.id
			 WHERE m.channel_id = $1 AND m.created_at < (SELECT created_at FROM messages WHERE id = $2)
			 GROUP BY m.id, u.username
			 ORDER BY m.created_at DESC LIMIT $3`,
			channelID, before, limit,
		)
		rows = r
		err = e
	} else {
		r, e := h.db.Query(context.Background(),
			`SELECT m.id, m.content, m.author_id, u.username, m.edited_at, m.created_at,
			        COALESCE(json_agg(json_build_object('id', a.id, 'filename', a.filename, 'url', a.url, 'content_type', a.content_type, 'size_bytes', a.size_bytes)) FILTER (WHERE a.id IS NOT NULL), '[]')
			 FROM messages m
			 JOIN users u ON u.id = m.author_id
			 LEFT JOIN attachments a ON a.message_id = m.id
			 WHERE m.channel_id = $1
			 GROUP BY m.id, u.username
			 ORDER BY m.created_at DESC LIMIT $2`,
			channelID, limit,
		)
		rows = r
		err = e
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch messages"})
		return
	}

	type scannable interface {
		Next() bool
		Scan(dest ...interface{}) error
		Close()
	}
	sr := rows.(scannable)
	defer sr.Close()

	var messages []gin.H
	for sr.Next() {
		var id, content, authorID, username string
		var editedAt *time.Time
		var createdAt time.Time
		var attachmentsJSON string
		if err := sr.Scan(&id, &content, &authorID, &username, &editedAt, &createdAt, &attachmentsJSON); err != nil {
			continue
		}
		messages = append(messages, gin.H{
			"id": id, "content": content, "author_id": authorID,
			"author_username": username, "edited_at": editedAt,
			"created_at": createdAt, "attachments": json.RawMessage(attachmentsJSON),
		})
	}
	if messages == nil {
		messages = []gin.H{}
	}

	// Reverse to chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	c.JSON(http.StatusOK, messages)
}

func (h *Handler) sendMessage(c *gin.Context) {
	serverID := c.Param("serverId")
	channelID := c.Param("channelId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}
	if !h.hasServerPermission(userID, serverID, model.PermSendMessages) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req sendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var id string
	var createdAt time.Time
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO messages (channel_id, author_id, content) VALUES ($1, $2, $3) RETURNING id, created_at`,
		channelID, userID, req.Content,
	).Scan(&id, &createdAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send message"})
		return
	}

	// Extract mentions from content and explicit mentions list
	mentionedIDs := make(map[string]bool)
	for _, match := range mentionRegex.FindAllStringSubmatch(req.Content, -1) {
		mentionedIDs[match[1]] = true
	}
	for _, uid := range req.Mentions {
		mentionedIDs[uid] = true
	}

	// Insert mentions and update read_states
	for uid := range mentionedIDs {
		h.db.Exec(context.Background(),
			`INSERT INTO message_mentions (message_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, uid)
		h.db.Exec(context.Background(),
			`INSERT INTO read_states (user_id, channel_id, mention_count) VALUES ($1, $2, 1)
			 ON CONFLICT (user_id, channel_id) DO UPDATE SET mention_count = read_states.mention_count + 1, updated_at = NOW()`,
			uid, channelID)
	}

	// Get author username
	var username string
	h.db.QueryRow(context.Background(), `SELECT username FROM users WHERE id = $1`, userID).Scan(&username)

	msgData := gin.H{
		"id": id, "channel_id": channelID, "content": req.Content,
		"author_id": userID, "author_username": username,
		"created_at": createdAt, "attachments": []interface{}{},
	}
	eventData, _ := json.Marshal(msgData)
	h.hub.PublishToChannel(channelID, WSEvent{Type: "message_create", Data: eventData})

	c.JSON(http.StatusCreated, msgData)
}

func (h *Handler) editMessage(c *gin.Context) {
	serverID := c.Param("serverId")
	channelID := c.Param("channelId")
	messageID := c.Param("messageId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	// Check author or manage messages permission
	var authorID string
	err := h.db.QueryRow(context.Background(),
		`SELECT author_id FROM messages WHERE id = $1 AND channel_id = $2`, messageID, channelID,
	).Scan(&authorID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "message not found"})
		return
	}
	if authorID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "can only edit own messages"})
		return
	}

	var req editMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err = h.db.Exec(context.Background(),
		`UPDATE messages SET content = $1, edited_at = NOW() WHERE id = $2`, req.Content, messageID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to edit message"})
		return
	}

	eventData, _ := json.Marshal(gin.H{
		"id": messageID, "channel_id": channelID, "content": req.Content, "edited_at": time.Now(),
	})
	h.hub.PublishToChannel(channelID, WSEvent{Type: "message_update", Data: eventData})

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (h *Handler) deleteMessage(c *gin.Context) {
	serverID := c.Param("serverId")
	channelID := c.Param("channelId")
	messageID := c.Param("messageId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	var authorID string
	err := h.db.QueryRow(context.Background(),
		`SELECT author_id FROM messages WHERE id = $1 AND channel_id = $2`, messageID, channelID,
	).Scan(&authorID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "message not found"})
		return
	}

	if authorID != userID && !h.hasServerPermission(userID, serverID, model.PermManageMessages) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	h.db.Exec(context.Background(), `DELETE FROM messages WHERE id = $1`, messageID)

	eventData, _ := json.Marshal(gin.H{"id": messageID, "channel_id": channelID})
	h.hub.PublishToChannel(channelID, WSEvent{Type: "message_delete", Data: eventData})

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
