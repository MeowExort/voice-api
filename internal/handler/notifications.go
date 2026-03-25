package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ackMessages marks a channel as read for the current user
func (h *Handler) ackMessages(c *gin.Context) {
	serverID := c.Param("serverId")
	channelID := c.Param("channelId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	var req struct {
		MessageID string `json:"message_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := h.db.Exec(context.Background(),
		`INSERT INTO read_states (user_id, channel_id, last_message_id, mention_count, updated_at)
		 VALUES ($1, $2, $3, 0, NOW())
		 ON CONFLICT (user_id, channel_id) DO UPDATE SET last_message_id = $3, mention_count = 0, updated_at = NOW()`,
		userID, channelID, req.MessageID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to ack"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// getUnreadCounts returns unread info for all channels in a server
func (h *Handler) getUnreadCounts(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT ch.id,
		        COALESCE(rs.mention_count, 0) AS mention_count,
		        CASE
		          WHEN rs.last_message_id IS NULL THEN (SELECT COUNT(*) FROM messages WHERE channel_id = ch.id)
		          ELSE (SELECT COUNT(*) FROM messages WHERE channel_id = ch.id AND created_at > (SELECT created_at FROM messages WHERE id = rs.last_message_id))
		        END AS unread_count
		 FROM channels ch
		 LEFT JOIN read_states rs ON rs.channel_id = ch.id AND rs.user_id = $1
		 WHERE ch.server_id = $2 AND ch.type = 'text'`,
		userID, serverID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch unread counts"})
		return
	}
	defer rows.Close()

	var result []gin.H
	for rows.Next() {
		var channelID string
		var mentionCount, unreadCount int
		if err := rows.Scan(&channelID, &mentionCount, &unreadCount); err != nil {
			continue
		}
		result = append(result, gin.H{
			"channel_id":    channelID,
			"mention_count": mentionCount,
			"unread_count":  unreadCount,
		})
	}
	if result == nil {
		result = []gin.H{}
	}

	c.JSON(http.StatusOK, result)
}
