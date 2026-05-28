package handler

import (
	"net/http"
	"wx_web_api/internal/config"
	"wx_web_api/internal/model"

	"github.com/gin-gonic/gin"
)

type SettingsHandler struct{}

func NewSettingsHandler() *SettingsHandler {
	return &SettingsHandler{}
}

type ConfigData struct {
	ApiBaseUrl string   `json:"api_base_url"`
	Tokens     []string `json:"tokens"`
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
	ApiBaseUrl string   `json:"api_base_url"`
	Tokens     []string `json:"tokens"`
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
		cfg.Tokens = req.Tokens
	}
	if err := config.Save(cfg); err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.SimpleResponse{Code: 0, Msg: "success"})
}