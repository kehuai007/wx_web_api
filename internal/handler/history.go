package handler

import (
	"net/http"
	"strconv"
	"strings"
	"wx_web_api/internal/storage"

	"github.com/gin-gonic/gin"
)

// GetHistory serves GET /api/history. All filter values default to "all"
// (no constraint) when empty or unknown.
func (h *Handler) GetHistory(c *gin.Context) {
	q := storage.HistoryQuery{
		Range:  c.Query("range"),
		Kind:   c.Query("kind"),
		Status: c.Query("status"),
		Token:  c.Query("token"),
		Q:      c.Query("q"),
		Page:   atoiOr(c.Query("page"), 1),
		Size:   atoiOr(c.Query("size"), 50),
	}
	if q.Page < 1 {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "page must be >= 1"})
		return
	}
	if q.Size < 1 || q.Size > 200 {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "size must be 1..200"})
		return
	}
	page, err := h.storage.QueryHistory(q)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "query failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": page})
}

// DeleteHistory serves DELETE /api/history. Use ?id=1,2,3 for batch or
// ?all=1 to nuke everything.
func (h *Handler) DeleteHistory(c *gin.Context) {
	if c.Query("all") == "1" {
		n, err := h.storage.DeleteAll()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "delete failed: " + err.Error()})
			return
		}
		EventsHub.PublishLogDeleted(nil)
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"deleted": n}})
		return
	}
	raw := c.Query("id")
	if raw == "" {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "id or all required"})
		return
	}
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "invalid id: " + p})
			return
		}
		ids = append(ids, v)
	}
	n, err := h.storage.DeleteByIDs(ids)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "delete failed: " + err.Error()})
		return
	}
	EventsHub.PublishLogDeleted(ids)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"deleted": n}})
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
