package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
	"wx_web_api/internal/config"
	"wx_web_api/internal/model"
)

type ParserService struct {
	baseURL string
	client  *http.Client
}

func NewParserService() *ParserService {
	return &ParserService{
		baseURL: config.Get().ApiBaseUrl,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *ParserService) Parse(shareURL string) (*model.WxParseData, error) {
	apiURL := s.baseURL + "/api/channels/shared_feed/profile?url=" + url.QueryEscape(shareURL)

	resp, err := s.client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ErrCode int    `json:"errCode"`
			ErrMsg  string `json:"errMsg"`
			Data    struct {
				Object struct {
					ObjectDesc struct {
						Description string `json:"description"`
						Media       []struct {
							URL       string `json:"url"`
							MediaType int    `json:"mediaType"`
							DecodeKey string `json:"decodeKey"`
							URLToken  string `json:"urlToken"`
							CoverUrl  string `json:"coverUrl"`
						} `json:"media"`
					} `json:"objectDesc"`
					Contact struct {
						Nickname string `json:"nickname"`
					} `json:"contact"`
				} `json:"object"`
			} `json:"data"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API错误: code=%d, msg=%s", result.Code, result.Msg)
	}

	if result.Data.ErrCode != 0 {
		return nil, fmt.Errorf("获取feed失败: errCode=%d, errMsg=%s", result.Data.ErrCode, result.Data.ErrMsg)
	}

	mediaList := result.Data.Data.Object.ObjectDesc.Media
	if len(mediaList) == 0 {
		return nil, fmt.Errorf("未找到媒体文件")
	}

	media := mediaList[0]
	videoURL := media.URL + media.URLToken

	return &model.WxParseData{
		Author:    result.Data.Data.Object.Contact.Nickname,
		Title:     result.Data.Data.Object.ObjectDesc.Description,
		CoverURL:  media.CoverUrl,
		VideoURL:  videoURL,
		DecodeKey: media.DecodeKey,
		MediaType: media.MediaType,
	}, nil
}