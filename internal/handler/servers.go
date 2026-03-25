package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/meowexort/voice-api/internal/model"
)

type createServerRequest struct {
	Name string `json:"name" binding:"required,min=1,max=100"`
}

type updateServerRequest struct {
	Name    *string `json:"name" binding:"omitempty,min=1,max=100"`
	IconURL *string `json:"icon_url"`
}

func (h *Handler) createServer(c *gin.Context) {
	var req createServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetString("user_id")

	tx, err := h.db.Begin(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to begin transaction"})
		return
	}
	defer tx.Rollback(context.Background())

	var serverID string
	err = tx.QueryRow(context.Background(),
		`INSERT INTO servers (name, owner_id) VALUES ($1, $2) RETURNING id`, req.Name, userID,
	).Scan(&serverID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create server"})
		return
	}

	// создаём роль @everyone с базовыми правами
	defaultPerms := int64(model.PermViewChannels | model.PermSendMessages | model.PermConnect | model.PermSpeak | model.PermCreateInvite)
	var everyoneRoleID string
	err = tx.QueryRow(context.Background(),
		`INSERT INTO roles (server_id, name, permissions, position) VALUES ($1, '@everyone', $2, 0) RETURNING id`,
		serverID, defaultPerms,
	).Scan(&everyoneRoleID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create default role"})
		return
	}

	// добавляем владельца как участника
	var memberID string
	err = tx.QueryRow(context.Background(),
		`INSERT INTO members (server_id, user_id) VALUES ($1, $2) RETURNING id`, serverID, userID,
	).Scan(&memberID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add owner as member"})
		return
	}

	_, err = tx.Exec(context.Background(),
		`INSERT INTO member_roles (member_id, role_id) VALUES ($1, $2)`, memberID, everyoneRoleID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign default role"})
		return
	}

	// создаём категорию и каналы по умолчанию
	var catID string
	err = tx.QueryRow(context.Background(),
		`INSERT INTO categories (server_id, name, position) VALUES ($1, 'Основное', 0) RETURNING id`, serverID,
	).Scan(&catID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create default category"})
		return
	}

	_, err = tx.Exec(context.Background(),
		`INSERT INTO channels (server_id, category_id, name, type, position) VALUES ($1, $2, 'общий', 'text', 0), ($1, $2, 'Голосовой', 'voice', 1)`,
		serverID, catID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create default channels"})
		return
	}

	if err := tx.Commit(context.Background()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to commit"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": serverID, "name": req.Name, "owner_id": userID})
}

func (h *Handler) getServers(c *gin.Context) {
	userID := c.GetString("user_id")

	rows, err := h.db.Query(context.Background(),
		`SELECT s.id, s.name, s.icon_url, s.owner_id, s.created_at
		 FROM servers s
		 JOIN members m ON m.server_id = s.id
		 WHERE m.user_id = $1
		 ORDER BY s.created_at`, userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch servers"})
		return
	}
	defer rows.Close()

	var servers []gin.H
	for rows.Next() {
		var id, name, ownerID string
		var iconURL *string
		var createdAt string
		if err := rows.Scan(&id, &name, &iconURL, &ownerID, &createdAt); err != nil {
			continue
		}
		servers = append(servers, gin.H{
			"id": id, "name": name, "icon_url": iconURL,
			"owner_id": ownerID, "created_at": createdAt,
		})
	}
	if servers == nil {
		servers = []gin.H{}
	}

	c.JSON(http.StatusOK, servers)
}

func (h *Handler) getServer(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	var name, ownerID string
	var iconURL *string
	err := h.db.QueryRow(context.Background(),
		`SELECT name, icon_url, owner_id FROM servers WHERE id = $1`, serverID,
	).Scan(&name, &iconURL, &ownerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": serverID, "name": name, "icon_url": iconURL, "owner_id": ownerID})
}

func (h *Handler) updateServer(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageServer) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req updateServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil {
		h.db.Exec(context.Background(), `UPDATE servers SET name = $1 WHERE id = $2`, *req.Name, serverID)
	}
	if req.IconURL != nil {
		h.db.Exec(context.Background(), `UPDATE servers SET icon_url = $1 WHERE id = $2`, *req.IconURL, serverID)
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (h *Handler) deleteServer(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	var ownerID string
	err := h.db.QueryRow(context.Background(), `SELECT owner_id FROM servers WHERE id = $1`, serverID).Scan(&ownerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}
	if ownerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only owner can delete server"})
		return
	}

	h.db.Exec(context.Background(), `DELETE FROM servers WHERE id = $1`, serverID)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (h *Handler) isMember(userID, serverID string) bool {
	var exists bool
	h.db.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM members WHERE user_id = $1 AND server_id = $2)`, userID, serverID,
	).Scan(&exists)
	return exists
}

func (h *Handler) isOwner(userID, serverID string) bool {
	var ownerID string
	err := h.db.QueryRow(context.Background(), `SELECT owner_id FROM servers WHERE id = $1`, serverID).Scan(&ownerID)
	return err == nil && ownerID == userID
}

func (h *Handler) hasServerPermission(userID, serverID string, perm model.Permission) bool {
	if h.isOwner(userID, serverID) {
		return true
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT r.permissions FROM roles r
		 JOIN member_roles mr ON mr.role_id = r.id
		 JOIN members m ON m.id = mr.member_id
		 WHERE m.user_id = $1 AND m.server_id = $2`, userID, serverID,
	)
	if err != nil {
		return false
	}
	defer rows.Close()

	var combined int64
	for rows.Next() {
		var p int64
		rows.Scan(&p)
		combined |= p
	}

	return model.HasPermission(model.Permission(combined), perm)
}
