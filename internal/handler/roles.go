package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/meowexort/voice-api/internal/model"
)

type createRoleRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=64"`
	Color       string `json:"color"`
	Permissions int64  `json:"permissions"`
}

type updateRoleRequest struct {
	Name        *string `json:"name" binding:"omitempty,min=1,max=64"`
	Color       *string `json:"color"`
	Permissions *int64  `json:"permissions"`
	Position    *int    `json:"position"`
}

type assignRoleRequest struct {
	UserID string `json:"user_id" binding:"required"`
}

func (h *Handler) createRole(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageRoles) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req createRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var maxPos int
	h.db.QueryRow(context.Background(),
		`SELECT COALESCE(MAX(position), 0) FROM roles WHERE server_id = $1`, serverID,
	).Scan(&maxPos)

	var id string
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO roles (server_id, name, color, permissions, position) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		serverID, req.Name, req.Color, req.Permissions, maxPos+1,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create role"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name, "permissions": req.Permissions})
}

func (h *Handler) getRoles(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, name, color, permissions, position FROM roles WHERE server_id = $1 ORDER BY position`, serverID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch roles"})
		return
	}
	defer rows.Close()

	var roles []gin.H
	for rows.Next() {
		var id, name string
		var color *string
		var permissions int64
		var position int
		if err := rows.Scan(&id, &name, &color, &permissions, &position); err != nil {
			continue
		}
		roles = append(roles, gin.H{
			"id": id, "name": name, "color": color,
			"permissions": permissions, "position": position,
		})
	}
	if roles == nil {
		roles = []gin.H{}
	}

	c.JSON(http.StatusOK, roles)
}

func (h *Handler) updateRole(c *gin.Context) {
	serverID := c.Param("serverId")
	roleID := c.Param("roleId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageRoles) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req updateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil {
		h.db.Exec(context.Background(), `UPDATE roles SET name = $1 WHERE id = $2 AND server_id = $3`, *req.Name, roleID, serverID)
	}
	if req.Color != nil {
		h.db.Exec(context.Background(), `UPDATE roles SET color = $1 WHERE id = $2 AND server_id = $3`, *req.Color, roleID, serverID)
	}
	if req.Permissions != nil {
		h.db.Exec(context.Background(), `UPDATE roles SET permissions = $1 WHERE id = $2 AND server_id = $3`, *req.Permissions, roleID, serverID)
	}
	if req.Position != nil {
		h.db.Exec(context.Background(), `UPDATE roles SET position = $1 WHERE id = $2 AND server_id = $3`, *req.Position, roleID, serverID)
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (h *Handler) deleteRole(c *gin.Context) {
	serverID := c.Param("serverId")
	roleID := c.Param("roleId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageRoles) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	// нельзя удалить @everyone
	var name string
	h.db.QueryRow(context.Background(), `SELECT name FROM roles WHERE id = $1`, roleID).Scan(&name)
	if name == "@everyone" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete @everyone role"})
		return
	}

	h.db.Exec(context.Background(), `DELETE FROM roles WHERE id = $1 AND server_id = $2`, roleID, serverID)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (h *Handler) assignRole(c *gin.Context) {
	serverID := c.Param("serverId")
	roleID := c.Param("roleId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageRoles) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req assignRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var memberID string
	err := h.db.QueryRow(context.Background(),
		`SELECT id FROM members WHERE user_id = $1 AND server_id = $2`, req.UserID, serverID,
	).Scan(&memberID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "member not found"})
		return
	}

	_, err = h.db.Exec(context.Background(),
		`INSERT INTO member_roles (member_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, memberID, roleID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign role"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "assigned"})
}

func (h *Handler) revokeRole(c *gin.Context) {
	serverID := c.Param("serverId")
	roleID := c.Param("roleId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageRoles) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req assignRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var memberID string
	err := h.db.QueryRow(context.Background(),
		`SELECT id FROM members WHERE user_id = $1 AND server_id = $2`, req.UserID, serverID,
	).Scan(&memberID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "member not found"})
		return
	}

	h.db.Exec(context.Background(), `DELETE FROM member_roles WHERE member_id = $1 AND role_id = $2`, memberID, roleID)
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}
