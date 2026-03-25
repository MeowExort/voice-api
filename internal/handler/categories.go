package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/meowexort/voice-api/internal/model"
)

type createCategoryRequest struct {
	Name     string `json:"name" binding:"required,min=1,max=100"`
	Position *int   `json:"position"`
}

type updateCategoryRequest struct {
	Name     *string `json:"name" binding:"omitempty,min=1,max=100"`
	Position *int    `json:"position"`
}

func (h *Handler) createCategory(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageChannels) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req createCategoryRequest
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
		`INSERT INTO categories (server_id, name, position) VALUES ($1, $2, $3) RETURNING id`,
		serverID, req.Name, pos,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create category"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name, "position": pos})
}

func (h *Handler) getCategories(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, name, position FROM categories WHERE server_id = $1 ORDER BY position`, serverID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch categories"})
		return
	}
	defer rows.Close()

	var categories []gin.H
	for rows.Next() {
		var id, name string
		var position int
		if err := rows.Scan(&id, &name, &position); err != nil {
			continue
		}
		categories = append(categories, gin.H{"id": id, "name": name, "position": position})
	}
	if categories == nil {
		categories = []gin.H{}
	}

	c.JSON(http.StatusOK, categories)
}

func (h *Handler) updateCategory(c *gin.Context) {
	serverID := c.Param("serverId")
	categoryID := c.Param("categoryId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageChannels) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req updateCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil {
		h.db.Exec(context.Background(), `UPDATE categories SET name = $1 WHERE id = $2 AND server_id = $3`, *req.Name, categoryID, serverID)
	}
	if req.Position != nil {
		h.db.Exec(context.Background(), `UPDATE categories SET position = $1 WHERE id = $2 AND server_id = $3`, *req.Position, categoryID, serverID)
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (h *Handler) deleteCategory(c *gin.Context) {
	serverID := c.Param("serverId")
	categoryID := c.Param("categoryId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageChannels) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	h.db.Exec(context.Background(), `DELETE FROM categories WHERE id = $1 AND server_id = $2`, categoryID, serverID)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
