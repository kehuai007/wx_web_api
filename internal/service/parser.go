package service

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// NewParserService returns a ParserService using the global config's API base URL.
// Used by handlers in production.
func NewParserService() *ParserService {
	return NewParserServiceWithBaseURL(config.Get().ApiBaseUrl)
}

// NewParserServiceWithBaseURL returns a ParserService with a custom API base URL.
// Used by tests; not exported as a setter to keep the field encapsulated.
func NewParserServiceWithBaseURL(baseURL string) *ParserService {
	return &ParserService{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Parse resolves a share URL to WxParseData. Tries feed/profile first;
// on any failure, falls back to parse_sph. Both responses are converted
// to the unified WxParseData format.
func (s *ParserService) Parse(shareURL string) (*model.WxParseData, error) {
	if body, err := s.fetchFeedProfile(shareURL); err == nil {
		if data, convErr := s.convertObjectShape(body); convErr == nil {
			return data, nil
		} else {
			log.Printf("parse: feed/profile convert failed, falling back to parse_sph: %v", convErr)
		}
	} else {
		log.Printf("parse: feed/profile failed, falling back to parse_sph: %v", err)
	}
	if body, err := s.fetchParseSph(shareURL); err == nil {
		if data, convErr := s.convertFeedInfoShape(body); convErr == nil {
			return data, nil
		} else {
			log.Printf("parse: parse_sph convert failed: %v", convErr)
			return nil, convErr
		}
	} else {
		log.Printf("parse: parse_sph also failed: %v", err)
		return nil, err
	}
}

// ParseFinderFeedByObjectID 通过 objectID 和 objectNonceID 获取视频信息
func (s *ParserService) ParseFinderFeedByObjectID(objectID, objectNonceID string) (*model.WxParseData, error) {
	apiURL := s.baseURL + "/api/channels/feed/profile?oid=" + url.QueryEscape(objectID) + "&nid=" + url.QueryEscape(objectNonceID)

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

// fetchFeedProfile GETs /api/channels/feed/profile?url=<shareURL> and returns the raw body.
// Errors are tagged with "feed" prefix to distinguish from parse_sph in logs.
func (s *ParserService) fetchFeedProfile(shareURL string) ([]byte, error) {
	apiURL := s.baseURL + "/api/channels/feed/profile?url=" + url.QueryEscape(shareURL)
	resp, err := s.client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("feed 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("feed 请求失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("feed 读取响应失败: %w", err)
	}
	return body, nil
}

// fetchParseSph GETs /api/channels/parse_sph?url=<shareURL> and returns the raw body.
// Errors are tagged with "sph" prefix.
func (s *ParserService) fetchParseSph(shareURL string) ([]byte, error) {
	apiURL := s.baseURL + "/api/channels/parse_sph?url=" + url.QueryEscape(shareURL)
	resp, err := s.client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("sph 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("sph 请求失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sph 读取响应失败: %w", err)
	}
	return body, nil
}

// convertObjectShape unmarshals the feed/profile response (object/objectDesc/media
// shape) and maps it to WxParseData. Returns error on any failure.
func (s *ParserService) convertObjectShape(body []byte) (*model.WxParseData, error) {
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
		return nil, fmt.Errorf("feed 解析响应失败: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("feed API错误: code=%d, msg=%s", result.Code, result.Msg)
	}
	if result.Data.ErrCode != 0 {
		return nil, fmt.Errorf("feed 获取feed失败: errCode=%d, errMsg=%s", result.Data.ErrCode, result.Data.ErrMsg)
	}
	mediaList := result.Data.Data.Object.ObjectDesc.Media
	if len(mediaList) == 0 {
		return nil, fmt.Errorf("feed 未找到媒体文件")
	}
	media := mediaList[0]
	data := &model.WxParseData{
		Author:    result.Data.Data.Object.Contact.Nickname,
		Title:     result.Data.Data.Object.ObjectDesc.Description,
		CoverURL:  media.CoverUrl,
		VideoURL:  media.URL + media.URLToken,
		DecodeKey: media.DecodeKey,
		MediaType: media.MediaType,
	}
	if data.Author == "" || data.Title == "" || data.CoverURL == "" || data.VideoURL == "" {
		return nil, fmt.Errorf("feed 必填字段缺失: %+v", data)
	}
	return data, nil
}

// convertFeedInfoShape unmarshals the parse_sph response (authorInfo/feedInfo
// shape) and maps it to WxParseData. Returns error on any failure.
func (s *ParserService) convertFeedInfoShape(body []byte) (*model.WxParseData, error) {
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ErrCode int    `json:"errCode"`
			ErrMsg  string `json:"errMsg"`
			Data    struct {
				AuthorInfo struct {
					Nickname string `json:"nickname"`
				} `json:"authorInfo"`
				FeedInfo struct {
					Description    string `json:"description"`
					MediaType      int    `json:"mediaType"`
					CoverUrl       string `json:"coverUrl"`
					OriginVideoUrl string `json:"originVideoUrl"`
				} `json:"feedInfo"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("sph 解析响应失败: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("sph API错误: code=%d, msg=%s", result.Code, result.Msg)
	}
	if result.Data.ErrCode != 0 {
		return nil, fmt.Errorf("sph 获取feed失败: errCode=%d, errMsg=%s", result.Data.ErrCode, result.Data.ErrMsg)
	}
	if result.Data.Data.FeedInfo.OriginVideoUrl == "" {
		return nil, fmt.Errorf("sph feedInfo.originVideoUrl 为空")
	}
	data := &model.WxParseData{
		Author:    result.Data.Data.AuthorInfo.Nickname,
		Title:     result.Data.Data.FeedInfo.Description,
		CoverURL:  result.Data.Data.FeedInfo.CoverUrl,
		VideoURL:  result.Data.Data.FeedInfo.OriginVideoUrl,
		DecodeKey: "", // parse_sph 不返回；空串 = 该视频无加密
		MediaType: result.Data.Data.FeedInfo.MediaType,
	}
	if data.Author == "" || data.Title == "" || data.CoverURL == "" {
		return nil, fmt.Errorf("sph 必填字段缺失: %+v", data)
	}
	return data, nil
}