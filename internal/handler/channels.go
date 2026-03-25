package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/meowexort/voice-api/internal/model"
)

type createChannelRequest struct {
	Name       string  `json:"name" binding:"required,min=1,max=100"`
	Type       string  `json:"type" binding:"required,oneof=text voice"`
	CategoryID *string `json:"category_id"`
	Position   *int    `json:"position"`
}

type updateChannelRequest struct {
	Name       *string `json:"name" binding:"omitempty,min=1,max=100"`
	CategoryID *string `json:"category_id"`
	Position   *int    `json:"position"`
}

func (h *Handler) createChannel(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageChannels) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req createChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	pos := 0
	if req.Position != nil {
		pos = *req.Position
	}

	var id string
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO channels (server_id, category_id, name, type, position) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		serverID, req.CategoryID, req.Name, req.Type, pos,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create channel"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name, "type": req.Type, "position": pos})
}

func (h *Handler) getChannels(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, category_id, name, type, position FROM channels WHERE server_id = $1 ORDER BY position`, serverID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch channels"})
		return
	}
	defer rows.Close()

	var channels []gin.H
	for rows.Next() {
		var id, name, chType string
		var categoryID *string
		var position int
		if err := rows.Scan(&id, &categoryID, &name, &chType, &position); err != nil {
			continue
		}
		channels = append(channels, gin.H{
			"id": id, "category_id": categoryID, "name": name,
			"type": chType, "position": position,
		})
	}
	if channels == nil {
		channels = []gin.H{}
	}

	c.JSON(http.StatusOK, channels)
}

func (h *Handler) updateChannel(c *gin.Context) {
	serverID := c.Param("serverId")
	channelID := c.Param("channelId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageChannels) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req updateChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil {
		h.db.Exec(context.Background(), `UPDATE channels SET name = $1 WHERE id = $2 AND server_id = $3`, *req.Name, channelID, serverID)
	}
	if req.CategoryID != nil {
		h.db.Exec(context.Background(), `UPDATE channels SET category_id = $1 WHERE id = $2 AND server_id = $3`, *req.CategoryID, channelID, serverID)
	}
	if req.Position != nil {
		h.db.Exec(context.Background(), `UPDATE channels SET position = $1 WHERE id = $2 AND server_id = $3`, *req.Position, channelID, serverID)
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (h *Handler) deleteChannel(c *gin.Context) {
	serverID := c.Param("serverId")
	channelID := c.Param("channelId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageChannels) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	h.db.Exec(context.Background(), `DELETE FROM channels WHERE id = $1 AND server_id = $2`, channelID, serverID)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
