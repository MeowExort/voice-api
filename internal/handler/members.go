package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) getMembers(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT m.id, m.user_id, u.username, u.avatar_url, m.nickname, m.joined_at
		 FROM members m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.server_id = $1
		 ORDER BY m.joined_at`, serverID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch members"})
		return
	}
	defer rows.Close()

	var members []gin.H
	for rows.Next() {
		var id, uid, username string
		var avatarURL, nickname *string
		var joinedAt string
		if err := rows.Scan(&id, &uid, &username, &avatarURL, &nickname, &joinedAt); err != nil {
			continue
		}
		members = append(members, gin.H{
			"id": id, "user_id": uid, "username": username,
			"avatar_url": avatarURL, "nickname": nickname, "joined_at": joinedAt,
		})
	}
	if members == nil {
		members = []gin.H{}
	}

	c.JSON(http.StatusOK, members)
}
