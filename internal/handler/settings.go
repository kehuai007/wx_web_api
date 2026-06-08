package handler

import (
	"net/http"
	"strings"
	"time"
	"wx_web_api/internal/config"
	"wx_web_api/internal/model"

	"github.com/gin-gonic/gin"
)

type SettingsHandler struct{}

func NewSettingsHandler() *SettingsHandler {
	return &SettingsHandler{}
}

type ConfigData struct {
	ApiBaseUrl string         `json:"api_base_url"`
	Tokens     []config.Token `json:"tokens"`
}

func (h *SettingsHandler) GetConfig(c *gin.Context) {
	cfg := config.Get()
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": ConfigData{
			ApiBaseUrl: cfg.ApiBaseUrl,
			Tokens:     cfg.Tokens,
		},
	})
}

type UpdateConfigRequest struct {
	ApiBaseUrl string         `json:"api_base_url"`
	Tokens     []config.Token `json:"tokens"`
}

func (h *SettingsHandler) UpdateConfig(c *gin.Context) {
	var req UpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid request"})
		return
	}
	cfg := config.Get()

	if req.ApiBaseUrl != "" {
		cfg.ApiBaseUrl = req.ApiBaseUrl
	}
	if req.Tokens != nil {
		// Validate each token
		normalized := make([]config.Token, 0, len(req.Tokens))
		seen := make(map[string]bool, len(req.Tokens))
		for i, t := range req.Tokens {
			value := strings.TrimSpace(t.Value)
			if value == "" {
				c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid tokens: index " + itoa(i) + " has empty value"})
				return
			}
			if seen[value] {
				continue // silently drop duplicates per spec step 3
			}
			expires := strings.TrimSpace(t.ExpiresAt)
			if expires != "" {
				if _, err := time.Parse("2006-01-02", expires); err != nil {
					c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid tokens: index " + itoa(i) + " expires_at must be yyyy-MM-dd or empty"})
					return
				}
			}
			seen[value] = true
			normalized = append(normalized, config.Token{Value: value, ExpiresAt: expires})
		}
		cfg.Tokens = normalized
	}

	if err := config.Save(cfg); err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.SimpleResponse{Code: 0, Msg: "success"})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
